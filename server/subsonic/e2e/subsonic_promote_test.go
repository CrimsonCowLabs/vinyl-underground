package e2e

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Masterminds/squirrel"
	_ "github.com/navidrome/navidrome/adapters/gotaglib" // reads the real Files below
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/metrics"
	"github.com/navidrome/navidrome/core/playlists"
	"github.com/navidrome/navidrome/core/rip"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/scanner"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/server/events"
	"github.com/navidrome/navidrome/server/nativeapi"
	"github.com/navidrome/navidrome/server/ripapi"
	"github.com/navidrome/navidrome/server/subsonic/responses"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.senan.xyz/taglib"
)

// Promote (#171) is Organize from the Inbox: a reviewed Inbox File moves to its scheme path at the
// Library's root, and the Track stays the same Track: everything a Listener attached to it in the
// Inbox is still attached after both libraries are rescanned, and every Listener can now see it.
// The Promote job is undone from the Edit history, which puts the File back in the Inbox, still
// the same Track. These specs run the real database, real Files and the real scanner.
var _ = Describe("Promote keeps a Track's identity", func() {
	// ripped.opus's tags (album artist "Rip Artist", album "Rip Album", title "Ripped Opus"), with a
	// track number and a year written in, which the scheme needs.
	const (
		inboxPath    = "Rip Album/01.opus"
		promotedPath = "Rip Artist/Rip Album (2024)/01 Ripped Opus.opus"
	)

	var (
		inbox, nasDir string
		inboxID       int
		nas           model.Library
		admin         model.User
		s             inboxScanner
		ripper        *rip.Ripper
		editor        *tagedit.Editor
		queue         *editjob.Queue
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

		// The music share as the app sees it: a library on disk, mounted read-write.
		nasDir = GinkgoT().TempDir()
		nas = model.Library{Name: "NAS", Path: nasDir}
		Expect(ds.Library(ctx).Put(&nas)).To(Succeed())
		Expect(ds.User(ctx).SetUserLibraries(regularUser.ID, []int{lib.ID, nas.ID})).To(Succeed())

		inbox = GinkgoT().TempDir()
		conf.Server.Inbox.Path = inbox
		Expect(os.Mkdir(filepath.Join(inbox, "Rip Album"), 0o755)).To(Succeed())
		data, err := os.ReadFile(filepath.Join("tests", "fixtures", "rips", "ripped.opus"))
		Expect(err).ToNot(HaveOccurred())
		full := filepath.Join(inbox, filepath.FromSlash(inboxPath))
		Expect(os.WriteFile(full, data, 0o644)).To(Succeed())
		Expect(taglib.WriteTags(full, map[string][]string{"TRACKNUMBER": {"1"}, "DATE": {"2024"}}, 0)).To(Succeed())
		Expect(ensureInbox(inbox)).To(BeTrue())
		inboxLib, err := core.FindInboxLibrary(ctx, ds)
		Expect(err).ToNot(HaveOccurred())
		inboxID = inboxLib.ID

		s = inboxScanner{scanner.New(ctx, ds, events.NoopBroker(),
			playlists.NewPlaylists(ds, artwork.NewUploader(ds)), metrics.NewNoopInstance())}
		scanAll()
		Expect(tracks()).To(HaveLen(1))
		trackID, albumID = tracks()[0].ID, tracks()[0].AlbumID
		Expect(tracks()[0].LibraryID).To(Equal(inboxID))

		job = &model.RipJob{UserID: adminUser.ID, URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ", Status: model.RipReady, Folder: "Rip Album"}
		Expect(ds.RipJob(ctx).Put(job)).To(Succeed())
		it := model.RipItem{JobID: job.ID, Position: 1, Status: model.RipReady, Path: inboxPath}
		Expect(ds.RipItem(ctx).Put(&it)).To(Succeed())
		// Another Rip that warned "already have" about the Track.
		other := &model.RipJob{UserID: adminUser.ID, URL: "https://www.youtube.com/watch?v=x", Status: model.RipFailed}
		Expect(ds.RipJob(ctx).Put(other)).To(Succeed())
		Expect(ds.RipItem(ctx).Put(&model.RipItem{JobID: other.ID, Position: 1, Status: model.RipFailed,
			AlreadyHave: model.RipMatches{{ID: trackID, Title: "Ripped Opus", Library: core.InboxLibraryName}}})).To(Succeed())

		loaded, err := ds.User(ctx).FindByUsername(adminUser.UserName)
		Expect(err).ToNot(HaveOccurred())
		admin = *loaded

		ripper = rip.New(ds, nil, s, rip.Options{ScanRetryDelay: 10 * time.Millisecond, DropSettle: 10 * time.Millisecond})
		queue, editor = startPromote(ripper, s, nas)

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

	api := func() http.Handler { return server.JWTVerifier(ripapi.New(ds, ripper)) }

	// attached checks the one Track, the same one, is in the library libID at path, with everything
	// attached to it in the Inbox still attached.
	attached := func(libID int, path string) {
		GinkgoHelper()
		all := tracks()
		Expect(all).To(HaveLen(1), "no second Track for the moved File")
		mf := all[0]
		Expect(mf.ID).To(Equal(trackID))
		Expect(mf.LibraryID).To(Equal(libID))
		Expect(mf.Path).To(Equal(path))
		Expect(mf.Missing).To(BeFalse())
		Expect(mf.Source).To(Equal("YouTube")) // the Ripped badge survives

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

		// Re-read by the scanner into its library, starred as the Inbox one was.
		album, err := ds.Album(ctx).Get(mf.AlbumID)
		Expect(err).ToNot(HaveOccurred())
		Expect(album.LibraryID).To(Equal(libID))
		Expect(album.Starred).To(BeTrue())
	}
	alreadyHaveLibrary := func() string {
		GinkgoHelper()
		var have string
		Expect(db.Db().QueryRow("select already_have from rip_item where already_have like ?", "%"+trackID+"%").Scan(&have)).To(Succeed())
		return have
	}

	It("shows the plan, promotes to the scheme path at the Library's root, keeps everything attached, and is undone back into the Inbox", func() {
		plan, editJob := promoteJob(api(), admin, job.ID)
		Expect(plan.Tracks).To(Equal(1))
		Expect(plan.Moves).To(ContainElement(And(HaveField("TrackID", trackID), HaveField("Path", inboxPath),
			HaveField("NewPath", promotedPath), HaveField("NewLibraryID", nas.ID))))
		Expect(editJob.Skipped).To(BeZero())

		Expect(filepath.Join(nasDir, filepath.FromSlash(promotedPath))).To(BeAnExistingFile())
		Expect(filepath.Join(inbox, "Rip Album")).ToNot(BeADirectory(), "the emptied Inbox folder is removed")
		attached(nas.ID, promotedPath)
		n, err := ds.MediaFile(ctx).CountAll(model.QueryOptions{Filters: squirrel.Eq{"media_file.library_id": inboxID}})
		Expect(err).ToNot(HaveOccurred())
		Expect(n).To(BeZero())

		// The Rip is done: its rows are gone; the other Rip's "already have" names the Library.
		_, err = ds.RipJob(ctx).Get(job.ID)
		Expect(errors.Is(err, model.ErrNotFound)).To(BeTrue())
		Expect(alreadyHaveLibrary()).To(ContainSubstring(`"libraryName":"NAS"`))

		// And so after a rescan of both libraries.
		scanAll()
		attached(nas.ID, promotedPath)

		// The Promote job is undone from the Edit history: the File goes back into the Inbox, the
		// same Track.
		router := nativeapi.New(ds, core.NewShare(ds), playlists.NewPlaylists(ds, artwork.NewUploader(ds)), nil, tests.NewMockLibraryService(), tests.NewMockUserService(),
			nil, nil, nil, nil, nil, nil, nil, editor)
		router.SetEditQueue(queue)
		w := adminCall(server.JWTVerifier(router), admin, http.MethodPost, "/editJob/"+editJob.ID+"/undo", nil)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		Expect(filepath.Join(inbox, filepath.FromSlash(inboxPath))).To(BeAnExistingFile())
		Expect(filepath.Join(nasDir, "Rip Artist")).ToNot(BeADirectory(), "the Library folders it made are removed")
		attached(inboxID, inboxPath)
		Expect(alreadyHaveLibrary()).To(ContainSubstring(`"libraryName":"` + core.InboxLibraryName + `"`))
		scanAll()
		attached(inboxID, inboxPath)
		// A Listener can't see it any more.
		Expect(doReqWithUser(regularUser, "getSong", "id", trackID).Status).To(Equal(responses.StatusFailed))

		// Back in the Inbox, the File gets a review card again.
		Eventually(func() []string {
			_, err := ripper.ReconcileDrops(ctx)
			Expect(err).ToNot(HaveOccurred())
			paths, err := ds.RipItem(ctx).Paths()
			Expect(err).ToNot(HaveOccurred())
			return paths
		}, 5*time.Second, 20*time.Millisecond).Should(ContainElement(inboxPath))
	})

	It("makes the promoted Track visible to every Listener", func() {
		promoteJob(api(), admin, job.ID)
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

	It("reuses a Library folder whose name differs only in letter case", func() {
		Expect(os.MkdirAll(filepath.Join(nasDir, "rip artist", "Older (1999)"), 0o755)).To(Succeed())
		data, err := os.ReadFile(filepath.Join("tests", "fixtures", "test.mp3"))
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(nasDir, "rip artist", "Older (1999)", "01 Old.mp3"), data, 0o644)).To(Succeed())
		scanAll()

		plan, _ := promoteJob(api(), admin, job.ID)
		Expect(plan.Moves).To(ContainElement(HaveField("NewPath", "rip artist/Rip Album (2024)/01 Ripped Opus.opus")))
		Expect(filepath.Join(nasDir, "rip artist", "Rip Album (2024)", "01 Ripped Opus.opus")).To(BeAnExistingFile())
		entries, err := os.ReadDir(nasDir)
		Expect(err).ToNot(HaveOccurred())
		Expect(entries).To(HaveLen(1), "no second folder differing only in letter case")
		attached(nas.ID, "rip artist/Rip Album (2024)/01 Ripped Opus.opus")
	})

	It("is off while the Library is read-only", func() {
		core.SetLibraryEditing(core.LibraryEditing{})
		w := adminCall(api(), admin, http.MethodPost, "/"+job.ID+"/promote/plan", nil)
		Expect(w.Code).To(Equal(http.StatusNotFound))
		Expect(filepath.Join(inbox, filepath.FromSlash(inboxPath))).To(BeAnExistingFile())
	})

	It("settles a Promote a stop interrupted, at the next start", func() {
		// The copy was placed in the Library (the Inbox is another filesystem), then the app stopped
		// before the Track followed.
		data, err := os.ReadFile(filepath.Join(inbox, filepath.FromSlash(inboxPath)))
		Expect(err).ToNot(HaveOccurred())
		Expect(os.MkdirAll(filepath.Join(nasDir, "Rip Artist", "Rip Album (2024)"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(nasDir, filepath.FromSlash(promotedPath)), data, 0o644)).To(Succeed())
		entry := &model.EditEntry{JobID: "interrupted", Kind: model.EditMove, State: model.EditPending,
			LibraryID: inboxID, NewLibraryID: nas.ID, MediaFileID: trackID, Path: inboxPath, NewPath: promotedPath,
			CopySum: fmt.Sprintf("%x", sha256.Sum256(data))}
		Expect(ds.EditJournal(ctx).Put(entry)).To(Succeed())

		editor.Settle(ctx)
		Expect(filepath.Join(inbox, filepath.FromSlash(inboxPath))).ToNot(BeAnExistingFile())
		scanAll()
		attached(nas.ID, promotedPath)
		got, err := ds.EditJournal(ctx).Get(entry.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.State).To(Equal(model.EditWritten))
	})
})
