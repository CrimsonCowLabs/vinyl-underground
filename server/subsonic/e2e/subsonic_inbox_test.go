package e2e

import (
	"net/http"
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

		Expect(core.EnsureInboxLibrary(ctx, ds, inboxPath)).To(Succeed())

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
			Expect(core.EnsureInboxLibrary(ctx, ds, inboxPath)).To(Succeed())
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

var _ = Describe("Inbox when not configured", func() {
	BeforeEach(func() {
		setupTestDB()
	})

	countLibraries := func() int64 {
		n, err := ds.Library(ctx).CountAll()
		Expect(err).ToNot(HaveOccurred())
		return n
	}

	It("does nothing when no Inbox path is set", func() {
		before := countLibraries()
		Expect(core.EnsureInboxLibrary(ctx, ds, "")).To(Succeed())
		Expect(countLibraries()).To(Equal(before))
	})

	It("does nothing when the Inbox folder is missing", func() {
		before := countLibraries()
		Expect(core.EnsureInboxLibrary(ctx, ds, GinkgoT().TempDir()+"/missing")).To(Succeed())
		Expect(countLibraries()).To(Equal(before))
	})

	It("moves the Inbox library when its path changes", func() {
		oldPath, newPath := GinkgoT().TempDir(), GinkgoT().TempDir()
		Expect(core.EnsureInboxLibrary(ctx, ds, oldPath)).To(Succeed())
		Expect(core.EnsureInboxLibrary(ctx, ds, newPath)).To(Succeed())

		libs, err := ds.Library(ctx).GetAll(model.QueryOptions{Filters: squirrel.Eq{"name": core.InboxLibraryName}})
		Expect(err).ToNot(HaveOccurred())
		Expect(libs).To(HaveLen(1))
		Expect(libs[0].Path).To(Equal(newPath))
	})
})
