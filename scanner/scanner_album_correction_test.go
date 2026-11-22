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
	"github.com/navidrome/navidrome/model/metadata"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/persistence"
	"github.com/navidrome/navidrome/scanner"
	"github.com/navidrome/navidrome/server/events"
	"github.com/navidrome/navidrome/tests"
	"github.com/navidrome/navidrome/utils/slice"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type noInfo struct{}

func (noInfo) RefreshInfo(context.Context, model.Kind, string) error { return nil }

var _ = Describe("Scanning with an album Correction", Ordered, func() {
	var ctx context.Context
	var ds model.DataStore
	var s model.Scanner
	var fsys storagetest.FakeFS
	var svc corrections.Corrections
	var findByPath func(string) (*model.MediaFile, error)

	const user = "123"
	const cd1, cd2 = "Band/Recrod/CD1/01 - One.mp3", "Band/Recrod/CD2/01 - Two.mp3"
	const other = "Band/Other/01 - Else.mp3"
	fix := corrections.AlbumFix{Name: "Record", AlbumArtist: "The Band", Year: 1999}

	BeforeAll(func() {
		tests.SkipOnWindows("SQLite file lock blocks TempDir cleanup (#TBD-path-sep-scanner)")
		ctx = request.WithUser(GinkgoT().Context(), model.User{ID: user, UserName: "admin", IsAdmin: true})
		conf.Server.DbPath = filepath.Join(GinkgoT().TempDir(), "test-album-correction-scan.db?_journal_mode=WAL")
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

		record := template(_t{"albumartist": "Band", "artist": "Band", "album": "Recrod", "year": 1998})
		fsys = createFS(fstest.MapFS{
			cd1:   record(track(1, "One"), _t{"disc": 1}),
			cd2:   record(track(1, "Two"), _t{"disc": 2}),
			other: template(_t{"albumartist": "Band", "artist": "Band", "album": "Other", "year": 1998})(track(1, "Else")),
		})
		_, err := s.ScanAll(ctx, true)
		Expect(err).ToNot(HaveOccurred())
		findByPath = createFindByPath(ctx, ds)
	})

	albumOf := func(path string) *model.Album {
		GinkgoHelper()
		mf, err := findByPath(path)
		Expect(err).ToNot(HaveOccurred())
		al, err := ds.Album(ctx).Get(mf.AlbumID)
		Expect(err).ToNot(HaveOccurred())
		return al
	}
	trackIDs := func() map[string]string {
		GinkgoHelper()
		mfs, err := ds.MediaFile(ctx).GetAll()
		Expect(err).ToNot(HaveOccurred())
		return slice.ToMap(mfs, func(mf model.MediaFile) (string, string) { return mf.Path, mf.ID })
	}
	correct := func(fix corrections.AlbumFix) *corrections.Applied {
		GinkgoHelper()
		res, err := svc.CorrectAlbum(ctx, albumOf(cd1).ID, fix)
		Expect(err).ToNot(HaveOccurred())
		Expect(res.Done).To(BeTrue())
		return res
	}
	expectCorrected := func() {
		GinkgoHelper()
		for _, p := range []string{cd1, cd2} {
			al := albumOf(p)
			Expect(al.Name).To(Equal("Record"), p)
			Expect(al.AlbumArtist).To(Equal("The Band"), p)
			Expect(al.AlbumArtistID).To(Equal(metadata.ArtistID("The Band")), p)
			Expect(al.MaxYear).To(Equal(1999), p)
		}
		Expect(albumOf(other).Name).To(Equal("Other"))
		Expect(albumOf(other).AlbumArtist).To(Equal("Band"))
	}

	It("corrects the album on every disc, in a rescan of its folders, and keeps it across later scans", func() {
		before := trackIDs()
		res := correct(fix)

		expectCorrected()
		Expect(res.AlbumID).To(Equal(albumOf(cd1).ID))
		Expect(albumOf(cd2).ID).To(Equal(res.AlbumID), "both discs are still one album")
		mf, err := findByPath(cd2)
		Expect(err).ToNot(HaveOccurred())
		Expect(mf.Album).To(Equal("Record"))
		Expect(mf.Artist).To(Equal("Band"), "the Track artist is no album value")
		Expect(mf.CorrectionIDs).To(Equal([]string{res.Correction.ID}))
		Expect(trackIDs()).To(Equal(before))

		_, err = s.ScanAll(ctx, false)
		Expect(err).ToNot(HaveOccurred())
		_, err = s.ScanAll(ctx, true)
		Expect(err).ToNot(HaveOccurred())
		expectCorrected()
		Expect(trackIDs()).To(Equal(before))

		all, err := svc.GetAll(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(all).To(HaveLen(1))
		Expect(all[0].TrackCount).To(Equal(2))
	})

	It("moves the album's stars, ratings and play counts to its new ID, and back on undo", func() {
		before := trackIDs()
		oldAlbum := albumOf(cd1)
		Expect(ds.Album(ctx).SetStar(true, oldAlbum.ID)).To(Succeed())
		Expect(ds.Album(ctx).SetRating(5, oldAlbum.ID)).To(Succeed())
		Expect(ds.Album(ctx).IncPlayCount(oldAlbum.ID, time.Now())).To(Succeed())
		one, err := findByPath(cd1)
		Expect(err).ToNot(HaveOccurred())
		Expect(ds.MediaFile(ctx).IncPlayCount(one.ID, time.Now())).To(Succeed())

		res := correct(fix)
		newAlbum := albumOf(cd1)
		Expect(newAlbum.ID).ToNot(Equal(oldAlbum.ID))
		Expect(newAlbum.Starred).To(BeTrue())
		Expect(newAlbum.Rating).To(Equal(5))
		Expect(newAlbum.PlayCount).To(Equal(int64(1)))
		one, err = ds.MediaFile(ctx).Get(one.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(one.PlayCount).To(Equal(int64(1)), "the Track keeps its ID, and so its play count")

		undone, err := svc.Undo(ctx, res.Correction.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(undone.Done).To(BeTrue())
		Expect(undone.AlbumID).To(Equal(oldAlbum.ID))
		album := albumOf(cd1)
		Expect(album.ID).To(Equal(oldAlbum.ID))
		Expect(album.Name).To(Equal("Recrod"))
		Expect(album.Starred).To(BeTrue())
		Expect(album.Rating).To(Equal(5))
		Expect(album.PlayCount).To(Equal(int64(1)))
		Expect(trackIDs()).To(Equal(before))
		_, err = ds.Correction(ctx).Get(res.Correction.ID)
		Expect(err).To(MatchError(model.ErrNotFound))
	})

	It("wins over an Artist rename for the album's album artist", func() {
		ar, err := ds.Artist(ctx).Get(metadata.ArtistID("Band"))
		Expect(err).ToNot(HaveOccurred())
		_, err = svc.RenameArtist(ctx, ar.ID, "Bandd")
		Expect(err).ToNot(HaveOccurred())
		correct(fix)

		_, err = s.ScanAll(ctx, true)
		Expect(err).ToNot(HaveOccurred())
		expectCorrected := albumOf(cd1)
		Expect(expectCorrected.AlbumArtist).To(Equal("The Band"))
		mf, err := findByPath(cd1)
		Expect(err).ToNot(HaveOccurred())
		Expect(mf.Artist).To(Equal("Bandd"), "the rename still applies to the Track artist")
		Expect(albumOf(other).AlbumArtist).To(Equal("Bandd"), "and to the other albums")
	})

	It("is orphaned once the album's folder moves, and the moved album is as tagged", func() {
		res := correct(fix)
		fsys = createFS(fstest.MapFS{
			"Band/Moved/CD1/01 - One.mp3": fsys.MapFS[cd1],
			"Band/Moved/CD2/01 - Two.mp3": fsys.MapFS[cd2],
			other:                         fsys.MapFS[other],
		})
		_, err := s.ScanAll(ctx, true)
		Expect(err).ToNot(HaveOccurred())

		Expect(albumOf("Band/Moved/CD1/01 - One.mp3").Name).To(Equal("Recrod"))
		all, err := svc.GetAll(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(all).To(HaveLen(1))
		Expect(all[0].ID).To(Equal(res.Correction.ID))
		Expect(all[0].TrackCount).To(BeZero())
	})
})
