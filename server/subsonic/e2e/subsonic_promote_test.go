package e2e

import (
	"bytes"
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
	"github.com/navidrome/navidrome/core/metrics"
	"github.com/navidrome/navidrome/core/playlists"
	"github.com/navidrome/navidrome/core/rip"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/scanner"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/server/events"
	"github.com/navidrome/navidrome/server/ripapi"
	"github.com/navidrome/navidrome/server/subsonic/responses"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Promote (#36) moves a reviewed Inbox File into the Library's read-write folder, and the
// Track stays the same Track: everything a Listener attached to it in the Inbox is still
// attached after both libraries are rescanned, and every Listener can now see it. These specs
// run the real database, real Files and the real scanner.
var _ = Describe("Promote keeps a Track's identity", func() {
	const promotedPath = "Vinyl Underground/Rip Artist/Rip Album/Ripped Opus.opus"

	var (
		inbox, nasDir string
		inboxID       int
		nas           model.Library
		admin         model.User
		s             inboxScanner
		job           *model.RipJob
		trackID       string
		albumID       string
		playlistID    string
	)

	scanAll := func() {
		Eventually(scanner.IsScanning).Should(BeFalse())
		var err error
		Eventually(func() bool {
			_, err = s.ScanFolders(ctx, false, []model.ScanTarget{{LibraryID: inboxID}, {LibraryID: nas.ID}})
			return errors.Is(err, scanner.ErrAlreadyScanning)
		}).Should(BeFalse())
		Expect(err).ToNot(HaveOccurred())
	}

	// tracks returns every Track (missing ones too) with the fixture's title, in any library.
	tracks := func() model.MediaFiles {
		mfs, err := ds.MediaFile(ctx).GetAll(model.QueryOptions{Filters: squirrel.Eq{"media_file.title": "Ripped Opus"}})
		Expect(err).ToNot(HaveOccurred())
		return mfs
	}

	BeforeEach(func() {
		setupTestDB()
		conf.Server.Ripping.Enabled = true
		conf.Server.EnableSharing = true

		// The NAS share as the app sees it: a library on disk with the read-write folder in it.
		nasDir = GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(nasDir, "Vinyl Underground"), 0o755)).To(Succeed())
		nas = model.Library{Name: "NAS", Path: nasDir}
		Expect(ds.Library(ctx).Put(&nas)).To(Succeed())
		Expect(ds.User(ctx).SetUserLibraries(regularUser.ID, []int{lib.ID, nas.ID})).To(Succeed())

		inbox = GinkgoT().TempDir()
		Expect(os.Mkdir(filepath.Join(inbox, "Rip Album"), 0o755)).To(Succeed())
		data, err := os.ReadFile(filepath.Join("tests", "fixtures", "rips", "ripped.opus"))
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(inbox, "Rip Album", "01.opus"), data, 0o644)).To(Succeed())
		Expect(ensureInbox(inbox)).To(BeTrue())
		inboxLib, err := core.FindInboxLibrary(ctx, ds)
		Expect(err).ToNot(HaveOccurred())
		inboxID = inboxLib.ID

		s = inboxScanner{scanner.New(ctx, ds, events.NoopBroker(),
			playlists.NewPlaylists(ds, artwork.NewUploader(ds)), metrics.NewNoopInstance())}
		scanAll()
		Expect(tracks()).To(HaveLen(1))
		trackID, albumID = tracks()[0].ID, tracks()[0].AlbumID

		job = &model.RipJob{UserID: adminUser.ID, URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ", Status: model.RipReady, Folder: "Rip Album"}
		Expect(ds.RipJob(ctx).Put(job)).To(Succeed())
		it := model.RipItem{JobID: job.ID, Position: 1, Status: model.RipReady, Path: "Rip Album/01.opus"}
		Expect(ds.RipItem(ctx).Put(&it)).To(Succeed())
		// Another Rip that warned "already have" about the Track.
		other := &model.RipJob{UserID: adminUser.ID, URL: "https://www.youtube.com/watch?v=x", Status: model.RipFailed}
		Expect(ds.RipJob(ctx).Put(other)).To(Succeed())
		Expect(ds.RipItem(ctx).Put(&model.RipItem{JobID: other.ID, Position: 1, Status: model.RipFailed,
			AlreadyHave: model.RipMatches{{ID: trackID, Title: "Ripped Opus", Library: core.InboxLibraryName}}})).To(Succeed())

		loaded, err := ds.User(ctx).FindByUsername(adminUser.UserName)
		Expect(err).ToNot(HaveOccurred())
		admin = *loaded

		// The admin uses the Inbox Track straight away.
		Expect(doReq("star", "id", trackID).Status).To(Equal(responses.StatusOK))
		Expect(doReq("star", "albumId", albumID).Status).To(Equal(responses.StatusOK))
		Expect(doReq("setRating", "id", trackID, "rating", "4").Status).To(Equal(responses.StatusOK))
		Expect(doReq("scrobble", "id", trackID, "submission", "true").Status).To(Equal(responses.StatusOK))
		Expect(doReq("createBookmark", "id", trackID, "position", "12000").Status).To(Equal(responses.StatusOK))
		Expect(doReq("savePlayQueue", "id", trackID, "current", trackID).Status).To(Equal(responses.StatusOK))
		resp := doReq("createPlaylist", "name", "Fresh", "songId", trackID)
		Expect(resp.Status).To(Equal(responses.StatusOK))
		playlistID = resp.Playlist.Id
		Expect(doReq("createShare", "id", trackID).Status).To(Equal(responses.StatusOK))
		_, err = db.Db().Exec("insert into station_track (user_id, media_file_id, artist, served_at) values (?, ?, 'Rip Artist', current_timestamp)",
			adminUser.ID, trackID)
		Expect(err).ToNot(HaveOccurred())

		// A Listener can't see it while it is in the Inbox.
		Expect(doReqWithUser(regularUser, "getSong", "id", trackID).Status).To(Equal(responses.StatusFailed))
	})

	promote := func() *httptest.ResponseRecorder {
		api := server.JWTVerifier(ripapi.New(ds, rip.New(ds, nil, s, rip.Options{PromoteRoot: filepath.Join(nasDir, "Vinyl Underground")})))
		req := httptest.NewRequest(http.MethodPost, "/"+job.ID+"/promote", bytes.NewBufferString(""))
		token, err := auth.CreateToken(&admin)
		Expect(err).ToNot(HaveOccurred())
		req.Header.Set(consts.UIAuthorizationHeader, "Bearer "+token)
		w := httptest.NewRecorder()
		api.ServeHTTP(w, req)
		return w
	}

	It("keeps favorites, ratings, plays, bookmarks, the play queue, Playlists, shares and Stations attached", func() {
		w := promote()
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		Expect(filepath.Join(nasDir, filepath.FromSlash(promotedPath))).To(BeAnExistingFile())
		Expect(filepath.Join(inbox, "Rip Album")).ToNot(BeADirectory())
		scanAll()

		// One Track, the same one, at its new place in the NAS library, with no ghost left.
		all := tracks()
		Expect(all).To(HaveLen(1))
		mf := all[0]
		Expect(mf.ID).To(Equal(trackID))
		Expect(mf.LibraryID).To(Equal(nas.ID))
		Expect(mf.Path).To(Equal(promotedPath))
		Expect(mf.Missing).To(BeFalse())
		Expect(mf.Source).To(Equal("YouTube")) // the Ripped badge survives
		n, err := ds.MediaFile(ctx).CountAll(model.QueryOptions{Filters: squirrel.Eq{"media_file.library_id": inboxID}})
		Expect(err).ToNot(HaveOccurred())
		Expect(n).To(BeZero())
		// Re-read by the scanner into its new library: a NAS album, starred like the Inbox one was.
		album, err := ds.Album(ctx).Get(mf.AlbumID)
		Expect(err).ToNot(HaveOccurred())
		Expect(album.LibraryID).To(Equal(nas.ID))
		Expect(album.Starred).To(BeTrue())

		song := doReq("getSong", "id", trackID)
		Expect(song.Status).To(Equal(responses.StatusOK))
		Expect(song.Song.Starred).ToNot(BeNil())
		Expect(song.Song.UserRating).To(Equal(int32(4)))
		Expect(song.Song.PlayCount).To(Equal(int64(1)))

		pl := doReq("getPlaylist", "id", playlistID)
		Expect(pl.Status).To(Equal(responses.StatusOK))
		Expect(pl.Playlist.Entry).To(HaveLen(1))
		Expect(pl.Playlist.Entry[0].Id).To(Equal(trackID))

		bm := doReq("getBookmarks")
		Expect(bm.Bookmarks.Bookmark).To(HaveLen(1))
		Expect(bm.Bookmarks.Bookmark[0].Entry.Id).To(Equal(trackID))

		pq := doReq("getPlayQueue")
		Expect(pq.PlayQueue.Entry).To(HaveLen(1))
		Expect(pq.PlayQueue.Entry[0].Id).To(Equal(trackID))

		sh := doReq("getShares")
		Expect(sh.Shares.Share).To(HaveLen(1))
		Expect(sh.Shares.Share[0].Entry).To(HaveLen(1))
		Expect(sh.Shares.Share[0].Entry[0].Id).To(Equal(trackID))

		var stationTracks int
		Expect(db.Db().QueryRow("select count(*) from station_track st join media_file mf on mf.id = st.media_file_id where mf.id = ?",
			trackID).Scan(&stationTracks)).To(Succeed())
		Expect(stationTracks).To(Equal(1))

		// The other Rip's "already have" now says where the Track is.
		var have string
		Expect(db.Db().QueryRow("select already_have from rip_item where already_have like ?", "%"+trackID+"%").Scan(&have)).To(Succeed())
		Expect(have).To(ContainSubstring(`"libraryName":"NAS"`))

		// The job is done: its rows are gone.
		_, err = ds.RipJob(ctx).Get(job.ID)
		Expect(errors.Is(err, model.ErrNotFound)).To(BeTrue())
	})

	It("makes the promoted Track visible to every Listener", func() {
		Expect(promote().Code).To(Equal(http.StatusOK))
		scanAll()

		got := doReqWithUser(regularUser, "getSong", "id", trackID)
		Expect(got.Status).To(Equal(responses.StatusOK))
		Expect(got.Song.Title).To(Equal("Ripped Opus"))
		found := doReqWithUser(regularUser, "search3", "query", "Ripped Opus")
		Expect(found.Status).To(Equal(responses.StatusOK))
		Expect(found.SearchResult3.Song).To(ContainElement(HaveField("Id", trackID)))
		// The Listener's own favorites start empty: the admin's star is the admin's.
		Expect(got.Song.Starred).To(BeNil())
	})
})
