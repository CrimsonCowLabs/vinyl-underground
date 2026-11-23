package scanner_test

import (
	"context"
	"path/filepath"
	"testing/fstest"
	"time"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/corrections"
	"github.com/navidrome/navidrome/core/metrics"
	"github.com/navidrome/navidrome/core/playlists"
	"github.com/navidrome/navidrome/core/refresh"
	"github.com/navidrome/navidrome/core/storage/storagetest"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/persistence"
	"github.com/navidrome/navidrome/scanner"
	"github.com/navidrome/navidrome/server/events"
	"github.com/navidrome/navidrome/server/filter"
	"github.com/navidrome/navidrome/tests"
	"github.com/navidrome/navidrome/utils/slice"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func mfID(mf model.MediaFile) string { return mf.ID }

var _ = Describe("Scanning with a track numbers Correction", Ordered, func() {
	var ctx context.Context
	var ds model.DataStore
	var s model.Scanner
	var fsys storagetest.FakeFS
	var svc corrections.Corrections
	var findByPath func(string) (*model.MediaFile, error)

	const user = "123"
	// Tagged out of order: One and Two swapped, and Three on the first disc, though in CD2
	const one, two, three = "Band/Record/CD1/01 - One.mp3", "Band/Record/CD1/02 - Two.mp3", "Band/Record/CD2/01 - Three.mp3"

	BeforeAll(func() {
		tests.SkipOnWindows("SQLite file lock blocks TempDir cleanup (#TBD-path-sep-scanner)")
		ctx = request.WithUser(GinkgoT().Context(), model.User{ID: user, UserName: "admin", IsAdmin: true})
		conf.Server.DbPath = filepath.Join(GinkgoT().TempDir(), "test-track-numbers-scan.db?_journal_mode=WAL")
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
		svc = corrections.NewWithOptions(ds, refresh.New(ds, s, noInfo{}), corrections.Options{Wait: time.Minute})

		record := template(_t{"albumartist": "Band", "artist": "Band", "album": "Record", "year": 1998, "disc": 1})
		fsys = createFS(fstest.MapFS{
			one:   record(track(2, "One")),
			two:   record(track(1, "Two")),
			three: record(track(3, "Three")),
		})
		_, err := s.ScanAll(ctx, true)
		Expect(err).ToNot(HaveOccurred())
		findByPath = createFindByPath(ctx, ds)
	})

	get := func(path string) *model.MediaFile {
		GinkgoHelper()
		mf, err := findByPath(path)
		Expect(err).ToNot(HaveOccurred())
		return mf
	}
	trackIDs := func() map[string]string {
		GinkgoHelper()
		mfs, err := ds.MediaFile(ctx).GetAll()
		Expect(err).ToNot(HaveOccurred())
		return slice.ToMap(mfs, func(mf model.MediaFile) (string, string) { return mf.Path, mf.ID })
	}
	// The album's Tracks as the album page, Subsonic's getAlbum and Jellyfin's album view list them
	albumOrder := func() []string {
		GinkgoHelper()
		mfs, err := ds.MediaFile(ctx).GetAll(filter.SongsByAlbum(get(one).AlbumID))
		Expect(err).ToNot(HaveOccurred())
		return slice.Map(mfs, func(mf model.MediaFile) string { return mf.Title })
	}
	correct := func() *corrections.Applied {
		GinkgoHelper()
		res, err := svc.CorrectTrackNumbers(ctx, get(one).AlbumID, []corrections.TrackNumber{
			{ID: get(one).ID, DiscNumber: 1, TrackNumber: 1},
			{ID: get(two).ID, DiscNumber: 1, TrackNumber: 2},
			{ID: get(three).ID, DiscNumber: 2, TrackNumber: 1},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(res.Done).To(BeTrue())
		return res
	}

	It("plays the album in the corrected order, which survives rescans", func() {
		Expect(albumOrder()).To(Equal([]string{"Two", "One", "Three"}))
		albumID := get(one).AlbumID
		res := correct()

		expect := func() {
			GinkgoHelper()
			Expect(albumOrder()).To(Equal([]string{"One", "Two", "Three"}))
			Expect(get(three).DiscNumber).To(Equal(2))
			Expect(get(three).TrackNumber).To(Equal(1))
			Expect(get(one).AlbumID).To(Equal(albumID))
			Expect(get(one).CorrectionIDs).To(Equal([]string{res.Correction.ID}))
		}
		expect()
		_, err := s.ScanAll(ctx, false)
		Expect(err).ToNot(HaveOccurred())
		expect()
		_, err = s.ScanAll(ctx, true)
		Expect(err).ToNot(HaveOccurred())
		expect()

		all, err := svc.GetAll(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(all).To(HaveLen(1))
		Expect(all[0].TrackCount).To(Equal(3))
		Expect(all[0].To).To(Equal("1-1 One, 1-2 Two, 2-1 Three"))
	})

	It("keeps the Tracks' IDs, so playlists, the play queue and play counts are unaffected, and undo restores the tagged order", func() {
		before := trackIDs()
		ids := []string{get(three).ID, get(one).ID, get(two).ID}
		pls := model.Playlist{Name: "Mix", OwnerID: user}
		pls.AddMediaFilesByID(ids)
		Expect(ds.Playlist(ctx).Put(&pls)).To(Succeed())
		Expect(ds.PlayQueue(ctx).Store(&model.PlayQueue{UserID: user, Current: 1,
			Items: model.MediaFiles{{ID: ids[0]}, {ID: ids[1]}, {ID: ids[2]}}})).To(Succeed())
		Expect(ds.MediaFile(ctx).IncPlayCount(ids[1], time.Now())).To(Succeed())

		res := correct()
		Expect(trackIDs()).To(Equal(before))
		saved, err := ds.Playlist(ctx).GetWithTracks(pls.ID, false, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(slice.Map(saved.MediaFiles(), mfID)).To(Equal(ids))
		queue, err := ds.PlayQueue(ctx).RetrieveWithMediaFiles(user)
		Expect(err).ToNot(HaveOccurred())
		Expect(slice.Map(queue.Items, mfID)).To(Equal(ids))
		Expect(queue.Current).To(Equal(1))
		played, err := ds.MediaFile(ctx).Get(ids[1])
		Expect(err).ToNot(HaveOccurred())
		Expect(played.PlayCount).To(Equal(int64(1)))

		undone, err := svc.Undo(ctx, res.Correction.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(undone.Done).To(BeTrue())
		Expect(albumOrder()).To(Equal([]string{"Two", "One", "Three"}))
		Expect(get(three).DiscNumber).To(Equal(1))
		Expect(trackIDs()).To(Equal(before))
		_, err = ds.Correction(ctx).Get(res.Correction.ID)
		Expect(err).To(MatchError(model.ErrNotFound))
	})

	It("applies together with an album Correction, either undone first", func() {
		nums := correct()
		album, err := svc.CorrectAlbum(ctx, get(one).AlbumID, corrections.AlbumFix{Name: "The Record", AlbumArtist: "Band"})
		Expect(err).ToNot(HaveOccurred())
		Expect(album.Done).To(BeTrue())
		Expect(get(one).Album).To(Equal("The Record"))
		Expect(albumOrder()).To(Equal([]string{"One", "Two", "Three"}))

		_, err = svc.Undo(ctx, album.Correction.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(get(one).Album).To(Equal("Record"))
		Expect(albumOrder()).To(Equal([]string{"One", "Two", "Three"}))
		_, err = svc.Undo(ctx, nums.Correction.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(albumOrder()).To(Equal([]string{"Two", "One", "Three"}))
	})

	It("is orphaned once its Files are gone, and the moved Files read as tagged", func() {
		res := correct()
		fsys = createFS(fstest.MapFS{
			"Band/Moved/CD1/01 - One.mp3":   fsys.MapFS[one],
			"Band/Moved/CD1/02 - Two.mp3":   fsys.MapFS[two],
			"Band/Moved/CD2/01 - Three.mp3": fsys.MapFS[three],
		})
		_, err := s.ScanAll(ctx, true)
		Expect(err).ToNot(HaveOccurred())

		Expect(get("Band/Moved/CD1/01 - One.mp3").TrackNumber).To(Equal(2))
		all, err := svc.GetAll(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(all).To(HaveLen(1))
		Expect(all[0].ID).To(Equal(res.Correction.ID))
		Expect(all[0].TrackCount).To(BeZero())
	})
})
