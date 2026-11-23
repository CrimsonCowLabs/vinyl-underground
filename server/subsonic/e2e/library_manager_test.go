package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"

	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/metrics"
	"github.com/navidrome/navidrome/core/playlists"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/scanner"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/server/events"
	"github.com/navidrome/navidrome/server/nativeapi"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.senan.xyz/taglib"
)

// The Library manager (#166) end to end: the real scanner finds the Files with no album artist
// tag, the listing filters them, a bulk edit of all matching writes them as one job, and the
// whole job's undo puts them back. Real database, real Files, real scanner.
var _ = Describe("Library manager bulk edit", func() {
	var (
		nas    model.Library
		nasDir string
		s      inboxScanner
		admin  model.User
	)
	files := []string{"Artist/Album/01 One.mp3", "Artist/Album/02 Two.mp3", "Other/03 Three.mp3"}

	scan := func() {
		GinkgoHelper()
		Eventually(scanner.IsScanning).Should(BeFalse())
		var err error
		Eventually(func() bool {
			_, err = s.ScanFolders(ctx, false, []model.ScanTarget{{LibraryID: nas.ID}})
			return errors.Is(err, scanner.ErrAlreadyScanning)
		}).Should(BeFalse())
		Expect(err).ToNot(HaveOccurred())
	}
	albumArtistOf := func(rel string) []string {
		GinkgoHelper()
		tags, err := taglib.ReadTags(filepath.Join(nasDir, filepath.FromSlash(rel)))
		Expect(err).ToNot(HaveOccurred())
		return tags["ALBUMARTIST"]
	}

	BeforeEach(func() {
		setupTestDB()
		nasDir = GinkgoT().TempDir()
		data, err := os.ReadFile(filepath.Join("tests", "fixtures", "test.mp3"))
		Expect(err).ToNot(HaveOccurred())
		for i, rel := range files {
			p := filepath.Join(nasDir, filepath.FromSlash(rel))
			Expect(os.MkdirAll(filepath.Dir(p), 0o755)).To(Succeed())
			Expect(os.WriteFile(p, data, 0o644)).To(Succeed())
			tags, err := taglib.ReadTags(p)
			Expect(err).ToNot(HaveOccurred())
			delete(tags, "ALBUMARTIST")
			if i == 1 {
				// Only the second File has an album artist tag.
				tags["ALBUMARTIST"] = []string{"The Artist"}
			}
			Expect(taglib.WriteTags(p, tags, taglib.Clear)).To(Succeed())
		}
		nas = model.Library{Name: "NAS", Path: nasDir}
		Expect(ds.Library(ctx).Put(&nas)).To(Succeed())
		Expect(ds.User(ctx).SetUserLibraries(adminUser.ID, []int{lib.ID, nas.ID})).To(Succeed())
		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: nas.ID, Name: nas.Name, Writable: true}}})
		DeferCleanup(core.SetLibraryEditing, core.LibraryEditing{})
		s = inboxScanner{scanner.New(ctx, ds, events.NoopBroker(),
			playlists.NewPlaylists(ds, artwork.NewUploader(ds)), metrics.NewNoopInstance())}
		scan()

		loaded, err := ds.User(ctx).FindByUsername(adminUser.UserName)
		Expect(err).ToNot(HaveOccurred())
		admin = *loaded
	})

	call := func(method, url string, body any) *httptest.ResponseRecorder {
		GinkgoHelper()
		editor := tagedit.New(ds, s)
		router := nativeapi.New(ds, core.NewShare(ds), playlists.NewPlaylists(ds, artwork.NewUploader(ds)), nil, tests.NewMockLibraryService(), tests.NewMockUserService(),
			nil, nil, nil, nil, nil, nil, nil, editor)
		queue := editjob.New(ds, nil, editjob.Kinds(ds, editor))
		router.SetEditQueue(queue)
		qctx, stop := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() { queue.Run(qctx); close(done) }()
		defer func() { stop(); <-done }()
		var b []byte
		if body != nil {
			var err error
			b, err = json.Marshal(body)
			Expect(err).ToNot(HaveOccurred())
		}
		req := httptest.NewRequest(method, url, bytes.NewBuffer(b))
		token, err := auth.CreateToken(&admin)
		Expect(err).ToNot(HaveOccurred())
		req.Header.Set(consts.UIAuthorizationHeader, "Bearer "+token)
		w := httptest.NewRecorder()
		server.JWTVerifier(router).ServeHTTP(w, req)
		return w
	}
	// listed returns the paths (and IDs) of the Tracks the listing shows for the query string.
	listed := func(query string) ([]string, map[string]string) {
		GinkgoHelper()
		w := call(http.MethodGet, "/libraryTrack?_start=0&_end=50&_sort=path&_order=ASC&missing=false&"+query, nil)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var mfs model.MediaFiles
		Expect(json.Unmarshal(w.Body.Bytes(), &mfs)).To(Succeed())
		var paths []string
		ids := map[string]string{}
		for _, mf := range mfs {
			if mf.LibraryID == nas.ID {
				paths = append(paths, mf.Path)
				ids[mf.Path] = mf.ID
			}
		}
		return paths, ids
	}

	It("finds the Files with no album artist tag, sets one in all matching as one job, and undoes the job", func() {
		paths, ids := listed("missing_tag=album_artist&folder=Artist")
		Expect(paths).To(Equal([]string{"Artist/Album/01 One.mp3"}))
		paths, _ = listed("missing_tag=album_artist")
		Expect(paths).To(Equal([]string{"Artist/Album/01 One.mp3", "Other/03 Three.mp3"}))

		edit := map[string]any{
			"filter": map[string]any{"missing_tag": []string{"album_artist"}},
			"field":  "albumartist", "values": []string{"The Artist"},
		}
		w := call(http.MethodPost, "/libraryManager/preview", edit)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		Expect(w.Body.String()).To(ContainSubstring(`"changes":2`))

		w = call(http.MethodPost, "/libraryManager/edit", edit)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var job model.EditJob
		Expect(json.Unmarshal(w.Body.Bytes(), &job)).To(Succeed())
		Expect(job.Total).To(Equal(2))
		Expect(job.Done).To(Equal(2))
		Expect(albumArtistOf("Artist/Album/01 One.mp3")).To(Equal([]string{"The Artist"}))
		Expect(albumArtistOf("Other/03 Three.mp3")).To(Equal([]string{"The Artist"}))

		// Each write re-scanned its File: none is missing an album artist now, and each Track
		// kept its ID.
		paths, after := listed("missing_tag=album_artist")
		Expect(paths).To(BeEmpty())
		_, after = listed("folder=Artist")
		Expect(after["Artist/Album/01 One.mp3"]).To(Equal(ids["Artist/Album/01 One.mp3"]))

		// One job of two writes in the Edit history.
		w = call(http.MethodGet, "/editHistory?_start=0&_end=25", nil)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var history []tagedit.HistoryEntry
		Expect(json.Unmarshal(w.Body.Bytes(), &history)).To(Succeed())
		Expect(history).To(HaveLen(2))
		Expect(slices.IndexFunc(history, func(e tagedit.HistoryEntry) bool { return e.JobID != job.ID || e.JobSize != 2 })).To(Equal(-1))

		w = call(http.MethodPost, "/editJob/"+job.ID+"/undo", nil)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		Expect(albumArtistOf("Artist/Album/01 One.mp3")).To(BeEmpty())
		Expect(albumArtistOf("Other/03 Three.mp3")).To(BeEmpty())
		Expect(albumArtistOf("Artist/Album/02 Two.mp3")).To(Equal([]string{"The Artist"}))
		paths, _ = listed("missing_tag=album_artist")
		Expect(paths).To(Equal([]string{"Artist/Album/01 One.mp3", "Other/03 Three.mp3"}))
	})
})
