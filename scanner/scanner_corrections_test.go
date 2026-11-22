package scanner_test

import (
	"context"
	"path/filepath"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/metrics"
	"github.com/navidrome/navidrome/core/playlists"
	"github.com/navidrome/navidrome/core/storage/storagetest"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/metadata"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/persistence"
	"github.com/navidrome/navidrome/scanner"
	"github.com/navidrome/navidrome/server/events"
	"github.com/navidrome/navidrome/tests"
	"github.com/navidrome/navidrome/utils/slice"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"testing/fstest"
)

var _ = Describe("Scanning with Corrections", Ordered, func() {
	var ctx context.Context
	var ds model.DataStore
	var s model.Scanner
	var fsys storagetest.FakeFS
	var findByPath func(string) (*model.MediaFile, error)
	var live func(...map[string]any) *fstest.MapFile
	var rename *model.Correction

	const user = "123"
	oldArtistID := metadata.ArtistID("Greatful Dead")
	newArtistID := metadata.ArtistID("Grateful Dead")
	targets := []model.ScanTarget{{LibraryID: 1, FolderPath: "Greatful Dead"}}

	BeforeAll(func() {
		tests.SkipOnWindows("SQLite file lock blocks TempDir cleanup (#TBD-path-sep-scanner)")
		ctx = request.WithUser(GinkgoT().Context(), model.User{ID: user, UserName: "admin", IsAdmin: true})
		conf.Server.DbPath = filepath.Join(GinkgoT().TempDir(), "test-corrections-scan.db?_journal_mode=WAL")
		log.Warn("Using DB at " + conf.Server.DbPath)
		db.Db().SetMaxOpenConns(1)
	})

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.MusicFolder = "fake:///music"
		conf.Server.DevExternalScanner = false
		db.Init(ctx)
		DeferCleanup(func() { Expect(tests.ClearDB()).To(Succeed()) })

		ds = persistence.New(db.Db())
		Expect(ds.User(ctx).Put(&model.User{ID: user, UserName: "admin", IsAdmin: true, NewPassword: "pw"})).To(Succeed())
		s = scanner.New(ctx, ds, events.NoopBroker(),
			playlists.NewPlaylists(ds, artwork.NewUploader(ds)), metrics.NewNoopInstance())
		Expect(ds.Library(ctx).Put(&model.Library{ID: 1, Name: "Fake Library", Path: "fake:///music"})).To(Succeed())

		live = template(_t{"albumartist": "Greatful Dead", "artist": "Greatful Dead", "album": "Live", "year": 1971})
		fsys = createFS(fstest.MapFS{
			"Greatful Dead/Live/01 - Ripple.mp3":       live(track(1, "Ripple")),
			"Greatful Dead/Live/02 - Casey Jones.mp3":  live(track(2, "Casey Jones"), _t{"composer": "Greatful Dead"}),
			"Other/Mix/01 - Truckin.mp3":               template(_t{"artist": "greatful  DEAD", "album": "Mix"})(track(1, "Truckin")),
			"Other/Mix/02 - Not the Grateful Dead.mp3": template(_t{"artist": "Someone", "album": "Mix"})(track(2, "Other")),
		})
		_, err := s.ScanAll(ctx, true)
		Expect(err).ToNot(HaveOccurred())
		findByPath = createFindByPath(ctx, ds)
		rename = &model.Correction{Kind: model.CorrectionArtistRename, MatchKey: model.ArtistNameKey("Greatful Dead"),
			From: "Greatful Dead", To: "Grateful Dead"}
	})

	scanFolders := func(t ...model.ScanTarget) {
		GinkgoHelper()
		_, err := s.ScanFolders(ctx, true, t)
		Expect(err).ToNot(HaveOccurred())
	}
	trackIDs := func() map[string]string {
		GinkgoHelper()
		mfs, err := ds.MediaFile(ctx).GetAll()
		Expect(err).ToNot(HaveOccurred())
		return slice.ToMap(mfs, func(mf model.MediaFile) (string, string) { return mf.Path, mf.ID })
	}
	albumOf := func(path string) *model.Album {
		GinkgoHelper()
		mf, err := findByPath(path)
		Expect(err).ToNot(HaveOccurred())
		al, err := ds.Album(ctx).Get(mf.AlbumID)
		Expect(err).ToNot(HaveOccurred())
		return al
	}

	It("renames the Track artist and album artist everywhere, but not composers", func() {
		Expect(ds.Correction(ctx).Add(rename)).To(Succeed())
		scanFolders(model.ScanTarget{LibraryID: 1, FolderPath: "Greatful Dead"}, model.ScanTarget{LibraryID: 1, FolderPath: "Other"})

		mf, err := findByPath("Greatful Dead/Live/02 - Casey Jones.mp3")
		Expect(err).ToNot(HaveOccurred())
		Expect(mf.Artist).To(Equal("Grateful Dead"))
		Expect(mf.AlbumArtist).To(Equal("Grateful Dead"))
		Expect(mf.ArtistID).To(Equal(newArtistID))
		Expect(mf.Participants.First(model.RoleComposer).Name).To(Equal("Greatful Dead"))
		Expect(mf.CorrectionIDs).To(Equal([]string{rename.ID}))
		Expect(albumOf("Greatful Dead/Live/01 - Ripple.mp3").AlbumArtist).To(Equal("Grateful Dead"))

		mix, err := findByPath("Other/Mix/01 - Truckin.mp3")
		Expect(err).ToNot(HaveOccurred())
		Expect(mix.Artist).To(Equal("Grateful Dead"))

		ar, err := ds.Artist(ctx).Get(newArtistID)
		Expect(err).ToNot(HaveOccurred())
		Expect(ar.Name).To(Equal("Grateful Dead"))
	})

	It("keeps the rename across later scans, and applies it to Files added later", func() {
		Expect(ds.Correction(ctx).Add(rename)).To(Succeed())
		scanFolders(targets...)
		fsys.Add("Greatful Dead/Live/03 - Sugar Magnolia.mp3", live(track(3, "Sugar Magnolia")))

		_, err := s.ScanAll(ctx, false)
		Expect(err).ToNot(HaveOccurred())
		_, err = s.ScanAll(ctx, true)
		Expect(err).ToNot(HaveOccurred())

		for _, p := range []string{"Greatful Dead/Live/01 - Ripple.mp3", "Greatful Dead/Live/03 - Sugar Magnolia.mp3"} {
			mf, err := findByPath(p)
			Expect(err).ToNot(HaveOccurred())
			Expect(mf.Artist).To(Equal("Grateful Dead"), p)
		}
	})

	It("keeps Track IDs, so playlists, the play queue and play counts survive", func() {
		before := trackIDs()
		ripple, _ := findByPath("Greatful Dead/Live/01 - Ripple.mp3")
		casey, _ := findByPath("Greatful Dead/Live/02 - Casey Jones.mp3")
		Expect(ds.MediaFile(ctx).IncPlayCount(ripple.ID, time.Now())).To(Succeed())
		Expect(ds.MediaFile(ctx).SetStar(true, casey.ID)).To(Succeed())
		pls := &model.Playlist{Name: "Dead", OwnerID: user}
		Expect(ds.Playlist(ctx).Put(pls)).To(Succeed())
		_, err := ds.Playlist(ctx).Tracks(pls.ID, false).Add([]string{ripple.ID, casey.ID})
		Expect(err).ToNot(HaveOccurred())
		Expect(ds.PlayQueue(ctx).Store(&model.PlayQueue{UserID: user, Current: 1,
			Items: model.MediaFiles{{ID: casey.ID}, {ID: ripple.ID}}})).To(Succeed())

		Expect(ds.Correction(ctx).Add(rename)).To(Succeed())
		scanFolders(targets...)

		Expect(trackIDs()).To(Equal(before))
		got, err := ds.MediaFile(ctx).Get(ripple.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.PlayCount).To(Equal(int64(1)))
		Expect(got.Artist).To(Equal("Grateful Dead"))
		got, err = ds.MediaFile(ctx).Get(casey.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Starred).To(BeTrue())
		withTracks, err := ds.Playlist(ctx).GetWithTracks(pls.ID, false, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(slice.Map(withTracks.Tracks, func(t model.PlaylistTrack) string { return t.MediaFileID })).To(Equal([]string{ripple.ID, casey.ID}))
		pq, err := ds.PlayQueue(ctx).RetrieveWithMediaFiles(user)
		Expect(err).ToNot(HaveOccurred())
		Expect(slice.Map(pq.Items, func(mf model.MediaFile) string { return mf.ID })).To(Equal([]string{casey.ID, ripple.ID}))
	})

	It("moves the Artist's and album's stars, ratings and play counts to their new IDs, and back on undo", func() {
		oldAlbum := albumOf("Greatful Dead/Live/01 - Ripple.mp3")
		Expect(ds.Artist(ctx).SetStar(true, oldArtistID)).To(Succeed())
		Expect(ds.Artist(ctx).SetRating(4, oldArtistID)).To(Succeed())
		Expect(ds.Artist(ctx).IncPlayCount(oldArtistID, time.Now())).To(Succeed())
		Expect(ds.Album(ctx).SetStar(true, oldAlbum.ID)).To(Succeed())
		Expect(ds.Album(ctx).SetRating(5, oldAlbum.ID)).To(Succeed())
		Expect(ds.Album(ctx).IncPlayCount(oldAlbum.ID, time.Now())).To(Succeed())

		By("renaming")
		Expect(ds.Correction(ctx).Add(rename)).To(Succeed())
		scanFolders(targets...)

		ar, err := ds.Artist(ctx).Get(newArtistID)
		Expect(err).ToNot(HaveOccurred())
		Expect(ar.Starred).To(BeTrue())
		Expect(ar.Rating).To(Equal(4))
		Expect(ar.PlayCount).To(Equal(int64(1)))
		newAlbum := albumOf("Greatful Dead/Live/01 - Ripple.mp3")
		Expect(newAlbum.ID).ToNot(Equal(oldAlbum.ID))
		Expect(newAlbum.Starred).To(BeTrue())
		Expect(newAlbum.Rating).To(Equal(5))
		Expect(newAlbum.PlayCount).To(Equal(int64(1)))

		By("undoing")
		Expect(ds.Correction(ctx).SetUndoing(rename.ID)).To(Succeed())
		scanFolders(targets...)
		Expect(ds.Correction(ctx).Delete(rename.ID)).To(Succeed())

		ar, err = ds.Artist(ctx).Get(oldArtistID)
		Expect(err).ToNot(HaveOccurred())
		Expect(ar.Name).To(Equal("Greatful Dead"))
		Expect(ar.Starred).To(BeTrue())
		Expect(ar.Rating).To(Equal(4))
		album := albumOf("Greatful Dead/Live/01 - Ripple.mp3")
		Expect(album.ID).To(Equal(oldAlbum.ID))
		Expect(album.Starred).To(BeTrue())
		mf, err := findByPath("Greatful Dead/Live/01 - Ripple.mp3")
		Expect(err).ToNot(HaveOccurred())
		Expect(mf.Artist).To(Equal("Greatful Dead"))
		Expect(mf.CorrectionIDs).To(BeEmpty())
	})

	It("records which Tracks each Correction applies to", func() {
		Expect(ds.Correction(ctx).Add(rename)).To(Succeed())
		scanFolders(targets...)

		all, err := ds.Correction(ctx).GetAll()
		Expect(err).ToNot(HaveOccurred())
		Expect(all[0].TrackCount).To(Equal(2)) // the Mix folder wasn't rescanned
		Expect(ds.MediaFile(ctx).CountAll(model.QueryOptions{Filters: squirrel.Eq{"artist": "Grateful Dead"}})).To(Equal(int64(2)))
	})
})
