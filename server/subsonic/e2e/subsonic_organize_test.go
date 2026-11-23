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
	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/metrics"
	"github.com/navidrome/navidrome/core/organize"
	"github.com/navidrome/navidrome/core/playlists"
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
	"go.senan.xyz/taglib"
)

// Organize (#167) moves a Track's File, and the Track stays the same Track: a Track's row is found
// by its File's path, so a move that didn't point the row at the new path would have the next scan
// make a new Track, and lose everything attached to the old one. These specs run the real
// database, a real File and the real scanner.
var _ = Describe("Organize keeps each Track's identity", func() {
	const (
		oldPath = "iTunes_Library/Some Folder/song.mp3"
		// test.mp3's tags (album artist "Album Artist", original date 1996, track 2), with its album
		// renamed: retag (and so Organize) takes "Album" for no album.
		newPath = "Album Artist/Brothers in Arms (1996)/02 Song.mp3"
	)

	var (
		nas        model.Library
		nasDir     string
		s          inboxScanner
		admin      model.User
		trackID    string
		albumID    string
		playlistID string
	)

	scan := func(full bool) {
		Eventually(scanner.IsScanning).Should(BeFalse())
		var err error
		Eventually(func() bool {
			_, err = s.ScanFolders(ctx, full, []model.ScanTarget{{LibraryID: nas.ID}})
			return errors.Is(err, scanner.ErrAlreadyScanning)
		}).Should(BeFalse())
		Expect(err).ToNot(HaveOccurred())
	}
	tracks := func() model.MediaFiles {
		mfs, err := ds.MediaFile(ctx).GetAll(model.QueryOptions{Filters: squirrel.Eq{"media_file.library_id": nas.ID}})
		Expect(err).ToNot(HaveOccurred())
		return mfs
	}
	exists := func(rel string) bool {
		_, err := os.Stat(filepath.Join(nasDir, filepath.FromSlash(rel)))
		return err == nil
	}

	BeforeEach(func() {
		setupTestDB()

		nasDir = GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(nasDir, "iTunes_Library", "Some Folder"), 0o755)).To(Succeed())
		data, err := os.ReadFile(filepath.Join("tests", "fixtures", "test.mp3"))
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(nasDir, filepath.FromSlash(oldPath)), data, 0o644)).To(Succeed())
		Expect(taglib.WriteTags(filepath.Join(nasDir, filepath.FromSlash(oldPath)), map[string][]string{"ALBUM": {"Brothers in Arms"}}, 0)).To(Succeed())
		cover, err := os.ReadFile(filepath.Join("tests", "fixtures", "artist", "an-album", "cover.jpg"))
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(nasDir, "iTunes_Library", "Some Folder", "cover.jpg"), cover, 0o644)).To(Succeed())
		nas = model.Library{Name: "NAS", Path: nasDir}
		Expect(ds.Library(ctx).Put(&nas)).To(Succeed())
		Expect(ds.User(ctx).SetUserLibraries(adminUser.ID, []int{lib.ID, nas.ID})).To(Succeed())
		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: nas.ID, Name: nas.Name, Writable: true}}})
		DeferCleanup(core.SetLibraryEditing, core.LibraryEditing{})

		s = inboxScanner{scanner.New(ctx, ds, events.NoopBroker(),
			playlists.NewPlaylists(ds, artwork.NewUploader(ds)), metrics.NewNoopInstance())}
		scan(false)
		Expect(tracks()).To(HaveLen(1))
		trackID, albumID = tracks()[0].ID, tracks()[0].AlbumID

		loaded, err := ds.User(ctx).FindByUsername(adminUser.UserName)
		Expect(err).ToNot(HaveOccurred())
		admin = *loaded

		Expect(doReq("star", "id", trackID).Status).To(Equal(responses.StatusOK))
		Expect(doReq("star", "albumId", albumID).Status).To(Equal(responses.StatusOK))
		Expect(doReq("setRating", "id", trackID, "rating", "4").Status).To(Equal(responses.StatusOK))
		Expect(doReq("scrobble", "id", trackID, "submission", "true").Status).To(Equal(responses.StatusOK))
		Expect(doReq("createBookmark", "id", trackID, "position", "12000").Status).To(Equal(responses.StatusOK))
		resp := doReq("createPlaylist", "name", "Organized", "songId", trackID)
		Expect(resp.Status).To(Equal(responses.StatusOK))
		playlistID = resp.Playlist.Id
	})

	call := func(method, url string, body any) *httptest.ResponseRecorder {
		editor := tagedit.New(ds, s)
		router := nativeapi.New(ds, core.NewShare(ds), playlists.NewPlaylists(ds, artwork.NewUploader(ds)), nil, tests.NewMockLibraryService(), tests.NewMockUserService(),
			nil, nil, nil, nil, nil, nil, nil, editor)
		// The jobs run on the real database's queue, with Organize's job kind, as at startup.
		queue := editjob.New(ds, nil, editjob.Kinds(ds, editor))
		for kind, k := range organize.JobKinds(ds, editor) {
			queue.Handle(kind, k)
		}
		router.SetEditQueue(queue)
		qctx, stop := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() { queue.Run(qctx); close(done) }()
		defer func() { stop(); <-done }()
		b, err := json.Marshal(body)
		Expect(err).ToNot(HaveOccurred())
		req := httptest.NewRequest(method, url, bytes.NewBuffer(b))
		token, err := auth.CreateToken(&admin)
		Expect(err).ToNot(HaveOccurred())
		req.Header.Set(consts.UIAuthorizationHeader, "Bearer "+token)
		w := httptest.NewRecorder()
		server.JWTVerifier(router).ServeHTTP(w, req)
		return w
	}

	// attached checks that everything a Listener attached to the Track is still attached to it,
	// with its File at path.
	attached := func(path string) {
		GinkgoHelper()
		all := tracks()
		Expect(all).To(HaveLen(1), "no second Track for the moved File")
		mf := all[0]
		Expect(mf.ID).To(Equal(trackID))
		Expect(mf.Path).To(Equal(path))
		Expect(mf.Missing).To(BeFalse())

		song := doReq("getSong", "id", trackID)
		Expect(song.Status).To(Equal(responses.StatusOK))
		Expect(song.Song.Starred).ToNot(BeNil())
		Expect(song.Song.UserRating).To(Equal(int32(4)))
		Expect(song.Song.PlayCount).To(Equal(int64(1)))

		pl := doReq("getPlaylist", "id", playlistID)
		Expect(pl.Playlist.Entry).To(HaveLen(1))
		Expect(pl.Playlist.Entry[0].Id).To(Equal(trackID))

		bm := doReq("getBookmarks")
		Expect(bm.Bookmarks.Bookmark).To(HaveLen(1))
		Expect(bm.Bookmarks.Bookmark[0].Entry.Id).To(Equal(trackID))

		album, err := ds.Album(ctx).Get(mf.AlbumID)
		Expect(err).ToNot(HaveOccurred())
		Expect(album.Starred).To(BeTrue())
	}

	It("plans the album, moves it as one job, and after a rescan, and after the job's undo, the Track is the same", func() {
		w := call(http.MethodPost, "/organize/plan", map[string]any{"albumId": albumID})
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var plan struct {
			Token  string         `json:"token"`
			Tracks int            `json:"tracks"`
			Files  int            `json:"files"`
			Moves  []organize.Row `json:"moves"`
		}
		Expect(json.Unmarshal(w.Body.Bytes(), &plan)).To(Succeed())
		Expect(plan.Tracks).To(Equal(1), w.Body.String())
		Expect(plan.Files).To(Equal(1))
		Expect(plan.Moves).To(ContainElements(
			And(HaveField("Path", oldPath), HaveField("NewPath", newPath), HaveField("TrackID", trackID)),
			And(HaveField("Path", "iTunes_Library/Some Folder/cover.jpg"), HaveField("NewPath", "Album Artist/Brothers in Arms (1996)/cover.jpg")),
		))
		Expect(exists(oldPath)).To(BeTrue(), "a plan moves nothing")

		w = call(http.MethodPost, "/organize/run", map[string]any{"albumId": albumID, "token": plan.Token})
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var job model.EditJob
		Expect(json.Unmarshal(w.Body.Bytes(), &job)).To(Succeed())
		Expect(job.Kind).To(Equal(model.EditOrganize))
		Expect(job.Skipped).To(BeZero())

		Expect(exists(newPath)).To(BeTrue())
		Expect(exists("Album Artist/Brothers in Arms (1996)/cover.jpg")).To(BeTrue())
		Expect(exists("iTunes_Library")).To(BeFalse(), "the emptied folders are removed")
		attached(newPath)

		// The next scan, a full one, reads the File at its new place into the same row.
		scan(true)
		attached(newPath)

		// The whole job's undo puts every File back where it was.
		w = call(http.MethodPost, "/editJob/"+job.ID+"/undo", nil)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		Expect(exists(oldPath)).To(BeTrue())
		Expect(exists("iTunes_Library/Some Folder/cover.jpg")).To(BeTrue())
		Expect(exists("Album Artist")).To(BeFalse())
		attached(oldPath)
		scan(true)
		attached(oldPath)
	})

	It("refuses a run of a plan that isn't the one reviewed", func() {
		w := call(http.MethodPost, "/organize/run", map[string]any{"albumId": albumID, "token": "stale"})
		Expect(w.Code).To(Equal(http.StatusConflict), w.Body.String())
		Expect(w.Body.String()).To(ContainSubstring("planChanged"))
		Expect(exists(oldPath)).To(BeTrue())
	})
})
