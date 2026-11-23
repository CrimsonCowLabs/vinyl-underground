package e2e

import (
	"bytes"
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
	"github.com/navidrome/navidrome/core/metrics"
	"github.com/navidrome/navidrome/core/playlists"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/db"
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

// Editing a Track's tags (#160) writes them into its File, and the Track stays the same Track:
// a Track's ID comes from its title, disc and track numbers (its persistent ID), yet after an edit
// of all three, and a rescan, everything a Listener attached to it is still attached. These specs
// run the real database, a real File and the real scanner.
var _ = Describe("Editing a Track's tags keeps its identity", func() {
	const relPath = "Artist/Album/02 Song.mp3"

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

	BeforeEach(func() {
		setupTestDB()

		nasDir = GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(nasDir, "Artist", "Album"), 0o755)).To(Succeed())
		data, err := os.ReadFile(filepath.Join("tests", "fixtures", "test.mp3"))
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(nasDir, filepath.FromSlash(relPath)), data, 0o644)).To(Succeed())
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
		Expect(tracks()[0].Title).To(Equal("Song"))

		loaded, err := ds.User(ctx).FindByUsername(adminUser.UserName)
		Expect(err).ToNot(HaveOccurred())
		admin = *loaded

		Expect(doReq("star", "id", trackID).Status).To(Equal(responses.StatusOK))
		Expect(doReq("star", "albumId", albumID).Status).To(Equal(responses.StatusOK))
		Expect(doReq("setRating", "id", trackID, "rating", "4").Status).To(Equal(responses.StatusOK))
		Expect(doReq("scrobble", "id", trackID, "submission", "true").Status).To(Equal(responses.StatusOK))
		Expect(doReq("createBookmark", "id", trackID, "position", "12000").Status).To(Equal(responses.StatusOK))
		resp := doReq("createPlaylist", "name", "Edited", "songId", trackID)
		Expect(resp.Status).To(Equal(responses.StatusOK))
		playlistID = resp.Playlist.Id
	})

	edit := func(values tagedit.Values) *httptest.ResponseRecorder {
		api := server.JWTVerifier(nativeapi.New(ds, core.NewShare(ds), playlists.NewPlaylists(ds, artwork.NewUploader(ds)), nil, tests.NewMockLibraryService(), tests.NewMockUserService(),
			nil, nil, nil, nil, nil, nil, nil, tagedit.New(ds, s)))
		body, err := json.Marshal(map[string]any{"values": values})
		Expect(err).ToNot(HaveOccurred())
		req := httptest.NewRequest(http.MethodPut, "/trackTags/"+trackID, bytes.NewBuffer(body))
		token, err := auth.CreateToken(&admin)
		Expect(err).ToNot(HaveOccurred())
		req.Header.Set(consts.UIAuthorizationHeader, "Bearer "+token)
		w := httptest.NewRecorder()
		api.ServeHTTP(w, req)
		return w
	}

	It("keeps its ID, favorite, rating, plays, bookmark and Playlist entry after an edit of its title, disc and track numbers, and a rescan", func() {
		before := tracks()[0]
		w := edit(tagedit.Values{"title": {"Song (Live)"}, "tracknumber": {"5"}, "discnumber": {"2"}})
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())

		// The File holds the new tags.
		tags, err := taglib.ReadTags(filepath.Join(nasDir, filepath.FromSlash(relPath)))
		Expect(err).ToNot(HaveOccurred())
		Expect(tags).To(HaveKeyWithValue("TITLE", []string{"Song (Live)"}))
		Expect(tags).To(HaveKeyWithValue("TRACKNUMBER", []string{"5/10"}))
		Expect(tags).To(HaveKeyWithValue("DISCNUMBER", []string{"2/2"}))

		// Re-read at once, into the same row, though its persistent ID changed; and again by a
		// full rescan, as the next scan would.
		for _, full := range []bool{false, true} {
			all := tracks()
			Expect(all).To(HaveLen(1))
			mf := all[0]
			Expect(mf.ID).To(Equal(trackID))
			Expect(mf.PID).ToNot(Equal(before.PID))
			Expect(mf.Title).To(Equal("Song (Live)"))
			Expect(mf.TrackNumber).To(Equal(5))
			Expect(mf.DiscNumber).To(Equal(2))
			Expect(mf.Missing).To(BeFalse())

			song := doReq("getSong", "id", trackID)
			Expect(song.Status).To(Equal(responses.StatusOK))
			Expect(song.Song.Title).To(Equal("Song (Live)"))
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

			if !full {
				scan(true)
			}
		}

		// The Edit journal kept the File's original tags.
		var original string
		Expect(ds.EditJournal(ctx).Pending()).To(BeEmpty())
		Expect(db.Db().QueryRow("select original_tags from edit_journal where media_file_id = ? and state = 'written'", trackID).Scan(&original)).To(Succeed())
		Expect(original).To(ContainSubstring(`"TITLE":["Song"]`))
		Expect(original).To(ContainSubstring(`"TRACKNUMBER":["2/10"]`))
	})
})
