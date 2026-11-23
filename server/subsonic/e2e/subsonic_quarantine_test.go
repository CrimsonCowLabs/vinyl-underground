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

	"github.com/Masterminds/squirrel"
	_ "github.com/navidrome/navidrome/adapters/gotaglib" // reads the real Files below
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/metrics"
	"github.com/navidrome/navidrome/core/playlists"
	"github.com/navidrome/navidrome/core/quarantine"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/scanner"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/server/events"
	"github.com/navidrome/navidrome/server/nativeapi"
	"github.com/navidrome/navidrome/server/subsonic/responses"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Quarantine from the app (#168): a Track sent to Quarantine leaves the Library, and restoring
// its File brings back the same Track, with its favorite, rating, plays and Playlist entry. Its
// row is kept, missing and quarantined, through scans that purge missing Tracks, and off the
// Missing files page. These specs run the real database, a real File and the real scanner.
var _ = Describe("Quarantine keeps a Track's identity", func() {
	const relPath = "Artist/Album/02 Song.mp3"

	var (
		nas        model.Library
		nasDir     string
		s          inboxScanner
		admin      model.User
		trackID    string
		playlistID string
	)

	scan := func(full bool) {
		GinkgoHelper()
		Eventually(scanner.IsScanning).Should(BeFalse())
		var err error
		Eventually(func() bool {
			_, err = s.ScanFolders(ctx, full, []model.ScanTarget{{LibraryID: nas.ID}})
			return errors.Is(err, scanner.ErrAlreadyScanning)
		}).Should(BeFalse())
		Expect(err).ToNot(HaveOccurred())
	}
	track := func() *model.MediaFile {
		GinkgoHelper()
		mf, err := ds.MediaFile(ctx).Get(trackID)
		Expect(err).ToNot(HaveOccurred())
		return mf
	}
	inLibrary := func() bool {
		GinkgoHelper()
		resp := doReq("search3", "query", "Song")
		Expect(resp.Status).To(Equal(responses.StatusOK))
		for _, song := range resp.SearchResult3.Song {
			if song.Id == trackID {
				return true
			}
		}
		return false
	}
	abs := func(rel string) string { return filepath.Join(nasDir, filepath.FromSlash(rel)) }

	BeforeEach(func() {
		setupTestDB()
		// Missing Tracks are purged at every scan: a quarantined one must survive it.
		conf.Server.Scanner.PurgeMissing = consts.PurgeMissingAlways

		nasDir = GinkgoT().TempDir()
		Expect(os.MkdirAll(abs("Artist/Album"), 0o755)).To(Succeed())
		data, err := os.ReadFile(filepath.Join("tests", "fixtures", "test.mp3"))
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(abs(relPath), data, 0o644)).To(Succeed())
		nas = model.Library{Name: "NAS", Path: nasDir}
		Expect(ds.Library(ctx).Put(&nas)).To(Succeed())
		Expect(ds.User(ctx).SetUserLibraries(adminUser.ID, []int{lib.ID, nas.ID})).To(Succeed())
		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: nas.ID, Name: nas.Name, Writable: true}}})
		DeferCleanup(core.SetLibraryEditing, core.LibraryEditing{})

		s = inboxScanner{scanner.New(ctx, ds, events.NoopBroker(),
			playlists.NewPlaylists(ds, artwork.NewUploader(ds)), metrics.NewNoopInstance())}
		scan(false)
		mfs, err := ds.MediaFile(ctx).GetAll(model.QueryOptions{Filters: squirrel.Eq{"media_file.library_id": nas.ID}})
		Expect(err).ToNot(HaveOccurred())
		Expect(mfs).To(HaveLen(1))
		trackID = mfs[0].ID

		loaded, err := ds.User(ctx).FindByUsername(adminUser.UserName)
		Expect(err).ToNot(HaveOccurred())
		admin = *loaded

		Expect(doReq("star", "id", trackID).Status).To(Equal(responses.StatusOK))
		Expect(doReq("setRating", "id", trackID, "rating", "4").Status).To(Equal(responses.StatusOK))
		Expect(doReq("scrobble", "id", trackID, "submission", "true").Status).To(Equal(responses.StatusOK))
		resp := doReq("createPlaylist", "name", "Kept", "songId", trackID)
		Expect(resp.Status).To(Equal(responses.StatusOK))
		playlistID = resp.Playlist.Id
	})

	call := func(method, url string, body any) *httptest.ResponseRecorder {
		GinkgoHelper()
		editor := tagedit.New(ds, s)
		router := nativeapi.New(ds, core.NewShare(ds), playlists.NewPlaylists(ds, artwork.NewUploader(ds)), nil, tests.NewMockLibraryService(), tests.NewMockUserService(),
			core.NewMaintenance(ds), nil, nil, nil, nil, nil, nil, editor)
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
	contents := func() quarantine.Summary {
		GinkgoHelper()
		w := call(http.MethodGet, "/quarantine", nil)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var c quarantine.Summary
		Expect(json.Unmarshal(w.Body.Bytes(), &c)).To(Succeed())
		return c
	}
	send := func() {
		GinkgoHelper()
		w := call(http.MethodPost, "/quarantine/send", map[string]any{"ids": []string{trackID}})
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		Expect(w.Body.String()).To(ContainSubstring(`"skipped":0`))
	}
	expectIntact := func() {
		GinkgoHelper()
		mf := track()
		Expect(mf.Missing).To(BeFalse())
		Expect(mf.Quarantined).To(BeFalse())
		Expect(mf.Path).To(Equal(relPath))
		Expect(inLibrary()).To(BeTrue())
		song := doReq("getSong", "id", trackID)
		Expect(song.Status).To(Equal(responses.StatusOK))
		Expect(song.Song.Starred).ToNot(BeNil())
		Expect(song.Song.UserRating).To(Equal(int32(4)))
		Expect(song.Song.PlayCount).To(Equal(int64(1)))
		pl := doReq("getPlaylist", "id", playlistID)
		Expect(pl.Playlist.Entry).To(HaveLen(1))
		Expect(pl.Playlist.Entry[0].Id).To(Equal(trackID))
	}

	It("sends a Track to Quarantine, out of the Library, and restores the same Track with its favorite, rating, plays and Playlist entry", func() {
		Expect(inLibrary()).To(BeTrue())
		send()

		Expect(abs("_Quarantine/" + relPath)).To(BeARegularFile())
		Expect(abs("_Quarantine/.ndignore")).To(BeARegularFile())
		Expect(abs(relPath)).ToNot(BeAnExistingFile())
		Expect(inLibrary()).To(BeFalse(), "the Track left the Library")

		// A full scan, purging missing Tracks, keeps the row, and leaves the File in Quarantine
		// alone; the Missing files page doesn't list it.
		scan(true)
		mf := track()
		Expect(mf.Missing).To(BeTrue())
		Expect(mf.Quarantined).To(BeTrue())
		Expect(inLibrary()).To(BeFalse())
		all, err := ds.MediaFile(ctx).GetAll(model.QueryOptions{Filters: squirrel.Eq{"media_file.library_id": nas.ID}})
		Expect(err).ToNot(HaveOccurred())
		Expect(all).To(HaveLen(1), "the File in Quarantine is no Track")
		w := call(http.MethodGet, "/missing?_start=0&_end=25", nil)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		Expect(w.Body.String()).ToNot(ContainSubstring(trackID))
		Expect(call(http.MethodDelete, "/missing", nil).Code).To(Equal(http.StatusOK))
		Expect(track().Quarantined).To(BeTrue(), "Remove all on the Missing files page leaves it")

		// The Quarantine page lists its File, where it came from and its Track.
		c := contents()
		Expect(c.Count).To(Equal(1))
		Expect(c.Files[0].Path).To(Equal("_Quarantine/" + relPath))
		Expect(c.Files[0].Original).To(Equal(relPath))
		Expect(c.Files[0].TrackID).To(Equal(trackID))

		w = call(http.MethodPost, "/quarantine/restore", map[string]any{"libraryId": nas.ID, "path": c.Files[0].Path})
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		Expect(abs(relPath)).To(BeARegularFile())
		Expect(abs("_Quarantine/" + relPath)).ToNot(BeAnExistingFile())
		expectIntact()
		scan(true)
		expectIntact()
		Expect(contents().Count).To(Equal(0))
	})

	It("restores the same Track with an undo of the whole job from the Edit history", func() {
		send()
		var jobID string
		jobs := call(http.MethodGet, "/editJob", nil)
		var list []model.EditJob
		Expect(json.Unmarshal(jobs.Body.Bytes(), &list)).To(Succeed())
		for _, j := range list {
			if j.Kind == model.EditQuarantine {
				jobID = j.ID
			}
		}
		Expect(jobID).ToNot(BeEmpty())
		w := call(http.MethodPost, "/editJob/"+jobID+"/undo", nil)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		Expect(w.Body.String()).To(ContainSubstring(`"skipped":0`))
		expectIntact()
	})

	It("empties Quarantine only as confirmed, deleting the File and its Track for good", func() {
		send()
		c := contents()
		w := call(http.MethodPost, "/quarantine/empty", map[string]any{"count": c.Count + 1, "size": c.Size})
		Expect(w.Code).To(Equal(http.StatusConflict), w.Body.String())
		Expect(w.Body.String()).To(ContainSubstring("quarantineChanged"))
		Expect(abs("_Quarantine/" + relPath)).To(BeARegularFile())

		w = call(http.MethodPost, "/quarantine/empty", map[string]any{"count": c.Count, "size": c.Size})
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		Expect(abs("_Quarantine/" + relPath)).ToNot(BeAnExistingFile())
		Expect(abs("_Quarantine/.ndignore")).To(BeARegularFile())
		_, err := ds.MediaFile(ctx).Get(trackID)
		Expect(err).To(MatchError(model.ErrNotFound))
		Expect(contents().Count).To(Equal(0))
	})
})
