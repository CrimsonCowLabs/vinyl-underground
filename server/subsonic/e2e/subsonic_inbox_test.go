package e2e

import (
	"net/http"
	"os"
	"testing/fstest"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/metrics"
	"github.com/navidrome/navidrome/core/playlists"
	"github.com/navidrome/navidrome/core/storage/storagetest"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/scanner"
	"github.com/navidrome/navidrome/server/events"
	"github.com/navidrome/navidrome/server/subsonic/responses"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The Inbox is where new music lands before it joins the Library. The app
// scans it as a second library that only the admin can see (issue #27).
var _ = Describe("Inbox", Ordered, func() {
	const inboxPath = "fake-inbox:///inbox"

	var admin model.User    // admin, reloaded so it carries the Inbox library
	var listener model.User // a Listener created after the Inbox exists
	var inboxSongID string

	inboxLibraries := func() model.Libraries {
		libs, err := ds.Library(ctx).GetAll(model.QueryOptions{Filters: squirrel.Eq{"name": core.InboxLibraryName}})
		Expect(err).ToNot(HaveOccurred())
		return libs
	}

	folderNames := func(user model.User) []string {
		resp := doReqWithUser(user, "getMusicFolders")
		Expect(resp.Status).To(Equal(responses.StatusOK))
		var names []string
		for _, f := range resp.MusicFolders.Folders {
			names = append(names, f.Name)
		}
		return names
	}

	reload := func(u model.User) model.User {
		loaded, err := ds.User(ctx).FindByUsername(u.UserName)
		Expect(err).ToNot(HaveOccurred())
		return *loaded
	}

	BeforeAll(func() {
		setupTestDB()

		dropped := template(_t{
			"albumartist": "Inbox Artist",
			"artist":      "Inbox Artist",
			"album":       "Fresh Album",
			"year":        2026,
			"genre":       "Indie",
		})
		inboxFS := storagetest.FakeFS{}
		inboxFS.SetFiles(fstest.MapFS{
			"Inbox Artist/Fresh Album/01 - Fresh Drop.mp3": dropped(track(1, "Fresh Drop")),
		})
		storagetest.Register("fake-inbox", &inboxFS)

		Expect(ensureInbox(inboxPath)).To(BeTrue())

		s := scanner.New(ctx, ds, events.NoopBroker(),
			playlists.NewPlaylists(ds, artwork.NewUploader(ds)), metrics.NewNoopInstance())
		_, err := s.ScanAll(ctx, false)
		Expect(err).ToNot(HaveOccurred())

		admin = reload(adminUser)

		// As in a real install (the multi-library migration), new Listeners get the Library.
		musicLib := lib
		musicLib.DefaultNewUsers = true
		Expect(ds.Library(ctx).Put(&musicLib)).To(Succeed())

		listener = model.User{ID: "inbox-listener", UserName: "inboxlistener", Name: "Inbox Listener", NewPassword: "password"}
		Expect(ds.User(ctx).Put(&listener)).To(Succeed())
		listener = reload(listener)

		songs, err := ds.MediaFile(ctx).GetAll(model.QueryOptions{Filters: squirrel.Eq{"media_file.title": "Fresh Drop"}})
		Expect(err).ToNot(HaveOccurred())
		Expect(songs).To(HaveLen(1))
		inboxSongID = songs[0].ID
	})

	Describe("bootstrap", func() {
		It("creates one Inbox library for the configured path", func() {
			libs := inboxLibraries()
			Expect(libs).To(HaveLen(1))
			Expect(libs[0].Path).To(Equal(inboxPath))
			Expect(libs[0].DefaultNewUsers).To(BeFalse())
		})

		It("is idempotent", func() {
			Expect(ensureInbox(inboxPath)).To(BeFalse())
			Expect(inboxLibraries()).To(HaveLen(1))
		})

		It("scans the Inbox into Tracks in the Inbox library", func() {
			mf, err := ds.MediaFile(ctx).Get(inboxSongID)
			Expect(err).ToNot(HaveOccurred())
			Expect(mf.LibraryID).To(Equal(inboxLibraries()[0].ID))
		})
	})

	Describe("access", func() {
		It("gives the admin the Inbox alongside the Library", func() {
			Expect(folderNames(admin)).To(ConsistOf("Music Library", core.InboxLibraryName))
		})

		It("does not give a new Listener the Inbox", func() {
			Expect(folderNames(listener)).To(ConsistOf("Music Library"))
		})

		It("does not give an existing Listener the Inbox", func() {
			Expect(folderNames(reload(regularUser))).ToNot(ContainElement(core.InboxLibraryName))
		})

		It("lets the admin find the Inbox Track, but not a Listener", func() {
			resp := doReqWithUser(admin, "search3", "query", "Fresh Drop")
			Expect(resp.SearchResult3.Song).To(HaveLen(1))

			resp = doReqWithUser(listener, "search3", "query", "Fresh Drop")
			Expect(resp.SearchResult3.Song).To(BeEmpty())
			Expect(resp.SearchResult3.Album).To(BeEmpty())
			Expect(resp.SearchResult3.Artist).To(BeEmpty())
		})

		It("keeps the Inbox Track out of a Listener's browsing", func() {
			albums := doReqWithUser(listener, "getAlbumList2", "type", "alphabeticalByName", "size", "500")
			for _, a := range albums.AlbumList2.Album {
				Expect(a.Name).ToNot(Equal("Fresh Album"))
			}

			random := doReqWithUser(listener, "getRandomSongs", "size", "500")
			for _, s := range random.RandomSongs.Songs {
				Expect(s.Id).ToNot(Equal(inboxSongID))
			}

			song := doReqWithUser(listener, "getSong", "id", inboxSongID)
			Expect(song.Status).To(Equal(responses.StatusFailed))
		})
	})

	Describe("playing Inbox Tracks", func() {
		It("lets the admin stream an Inbox Track", func() {
			w := doRawReqWithUser(admin, "stream", "id", inboxSongID)
			Expect(w.Code).To(Equal(http.StatusOK))
		})

		It("lets the admin favorite an Inbox Track", func() {
			Expect(doReqWithUser(admin, "star", "id", inboxSongID).Status).To(Equal(responses.StatusOK))

			starred := doReqWithUser(admin, "getStarred2")
			var ids []string
			for _, s := range starred.Starred2.Song {
				ids = append(ids, s.Id)
			}
			Expect(ids).To(ContainElement(inboxSongID))
		})

		It("lets the admin add an Inbox Track to a Playlist, hidden from Listeners if shared", func() {
			libSongs, err := ds.MediaFile(ctx).GetAll(model.QueryOptions{
				Filters: squirrel.Eq{"media_file.library_id": lib.ID}, Max: 1, Sort: "title",
			})
			Expect(err).ToNot(HaveOccurred())
			libSongID := libSongs[0].ID

			resp := doReqWithUser(admin, "createPlaylist", "name", "New Arrivals",
				"songId", libSongID, "songId", inboxSongID)
			Expect(resp.Status).To(Equal(responses.StatusOK))
			Expect(resp.Playlist.Entry).To(HaveLen(2))
			playlistID := resp.Playlist.Id

			Expect(doReqWithUser(admin, "updatePlaylist", "playlistId", playlistID, "public", "true").Status).
				To(Equal(responses.StatusOK))

			shared := doReqWithUser(listener, "getPlaylist", "id", playlistID)
			Expect(shared.Playlist).ToNot(BeNil())
			Expect(shared.Playlist.Entry).To(HaveLen(1))
			Expect(shared.Playlist.Entry[0].Id).To(Equal(libSongID))
		})
	})
})

// ensureInbox runs the startup step and reports whether it created or moved the Inbox library.
func ensureInbox(path string) bool {
	changed, err := core.EnsureInboxLibrary(ctx, ds, path)
	Expect(err).ToNot(HaveOccurred())
	return changed
}

var _ = Describe("Inbox library bootstrap", func() {
	BeforeEach(func() {
		setupTestDB()
	})

	countLibraries := func() int64 {
		n, err := ds.Library(ctx).CountAll()
		Expect(err).ToNot(HaveOccurred())
		return n
	}

	getLibrary := func(id int) *model.Library {
		l, err := ds.Library(ctx).Get(id)
		Expect(err).ToNot(HaveOccurred())
		return l
	}

	// The only library besides the Library itself.
	inboxLibrary := func() model.Library {
		libs, err := ds.Library(ctx).GetAll(model.QueryOptions{Filters: squirrel.NotEq{"id": lib.ID}})
		Expect(err).ToNot(HaveOccurred())
		Expect(libs).To(HaveLen(1))
		return libs[0]
	}

	It("does nothing when no Inbox path is set", func() {
		before := countLibraries()
		Expect(ensureInbox("")).To(BeFalse())
		Expect(countLibraries()).To(Equal(before))
	})

	It("does nothing when the Inbox folder is missing", func() {
		before := countLibraries()
		Expect(ensureInbox(GinkgoT().TempDir() + "/missing")).To(BeFalse())
		Expect(countLibraries()).To(Equal(before))
	})

	It("stores a clean path", func() {
		dir := GinkgoT().TempDir()
		Expect(ensureInbox(dir + "/")).To(BeTrue())
		Expect(inboxLibrary().Path).To(Equal(dir))
		Expect(ensureInbox(dir)).To(BeFalse())
	})

	It("moves the Inbox library when its path changes", func() {
		oldPath, newPath := GinkgoT().TempDir(), GinkgoT().TempDir()
		Expect(ensureInbox(oldPath)).To(BeTrue())
		id := inboxLibrary().ID

		Expect(ensureInbox(newPath)).To(BeTrue())
		Expect(countLibraries()).To(Equal(int64(2)))
		Expect(getLibrary(id).Path).To(Equal(newPath))
	})

	It("still finds the Inbox after an admin renames it", func() {
		dir := GinkgoT().TempDir()
		Expect(ensureInbox(dir)).To(BeTrue())
		inbox := inboxLibrary()
		inbox.Name = "New Arrivals"
		Expect(ds.Library(ctx).Put(&inbox)).To(Succeed())

		Expect(ensureInbox(dir)).To(BeFalse())
		Expect(countLibraries()).To(Equal(int64(2)))
	})

	It("turns 'default for new users' back off on an existing Inbox", func() {
		dir := GinkgoT().TempDir()
		Expect(ensureInbox(dir)).To(BeTrue())
		inbox := inboxLibrary()
		inbox.DefaultNewUsers = true
		Expect(ds.Library(ctx).Put(&inbox)).To(Succeed())

		ensureInbox(dir)
		Expect(getLibrary(inbox.ID).DefaultNewUsers).To(BeFalse())
	})

	It("does not take over another library that happens to be called Inbox", func() {
		other := model.Library{Name: core.InboxLibraryName, Path: GinkgoT().TempDir()}
		Expect(ds.Library(ctx).Put(&other)).To(Succeed())

		_, err := core.EnsureInboxLibrary(ctx, ds, GinkgoT().TempDir())
		Expect(err).To(HaveOccurred())
		Expect(getLibrary(other.ID).Path).To(Equal(other.Path))
	})

	It("refuses an Inbox inside, or containing, another library", func() {
		parent := GinkgoT().TempDir()
		music := model.Library{Name: "Local Music", Path: parent + "/music"}
		Expect(os.MkdirAll(music.Path+"/inbox", 0o755)).To(Succeed())
		Expect(ds.Library(ctx).Put(&music)).To(Succeed())
		before := countLibraries()

		Expect(ensureInbox(music.Path + "/inbox")).To(BeFalse())
		Expect(ensureInbox(music.Path)).To(BeFalse())
		Expect(ensureInbox(parent)).To(BeFalse())
		Expect(countLibraries()).To(Equal(before))
	})
})
