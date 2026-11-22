package scanner_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"testing/fstest"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/corrections"
	"github.com/navidrome/navidrome/core/metrics"
	"github.com/navidrome/navidrome/core/playlists"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/persistence"
	"github.com/navidrome/navidrome/scanner"
	"github.com/navidrome/navidrome/server/events"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Scanning with a chosen album cover", Ordered, func() {
	var ctx context.Context
	var ds model.DataStore
	var s model.Scanner
	var svc corrections.Corrections

	const user = "123"
	folder := model.ScanTarget{LibraryID: 1, FolderPath: "Band"}

	BeforeAll(func() {
		tests.SkipOnWindows("SQLite file lock blocks TempDir cleanup (#TBD-path-sep-scanner)")
		ctx = request.WithUser(GinkgoT().Context(), model.User{ID: user, UserName: "admin", IsAdmin: true})
		conf.Server.DbPath = filepath.Join(GinkgoT().TempDir(), "test-album-cover-scan.db?_journal_mode=WAL")
		db.Db().SetMaxOpenConns(1)
	})

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.MusicFolder = "fake:///music"
		conf.Server.DevExternalScanner = false
		conf.Server.DataFolder = conf.NewDir(GinkgoT().TempDir())
		conf.Server.CoverArtPriority = "cover.*, embedded"
		db.Init(ctx)
		DeferCleanup(func() { Expect(tests.ClearDB()).To(Succeed()) })

		ds = persistence.New(db.Db())
		Expect(ds.User(ctx).Put(&model.User{ID: user, UserName: "admin", IsAdmin: true, NewPassword: "pw"})).To(Succeed())
		s = scanner.New(ctx, ds, events.NoopBroker(),
			playlists.NewPlaylists(ds, artwork.NewUploader(ds)), metrics.NewNoopInstance())
		Expect(ds.Library(ctx).Put(&model.Library{ID: 1, Name: "Fake Library", Path: "fake:///music"})).To(Succeed())
		svc = corrections.New(ds, nil)

		record := template(_t{"albumartist": "Band", "artist": "Band", "album": "Record", "year": 1999})
		createFS(fstest.MapFS{
			"Band/Record/01 - One.mp3": record(track(1, "One")),
			"Band/Record/02 - Two.mp3": record(track(2, "Two")),
		})
		_, err := s.ScanAll(ctx, true)
		Expect(err).ToNot(HaveOccurred())
	})

	theAlbum := func() model.Album {
		GinkgoHelper()
		als, err := ds.Album(ctx).GetAll()
		Expect(err).ToNot(HaveOccurred())
		Expect(als).To(HaveLen(1))
		return als[0]
	}
	coverSource := func() string {
		GinkgoHelper()
		src, err := artwork.NewTracingResolver(ds, nil, tests.NewMockFFmpeg(""), &artwork.ChainTrace{}, false).
			Resolve(ctx, model.KindAlbumArtwork, theAlbum().ID)
		Expect(err).ToNot(HaveOccurred())
		return src
	}
	choose := func() *corrections.Applied {
		GinkgoHelper()
		img := image.NewRGBA(image.Rect(0, 0, 2, 2))
		img.Set(0, 0, color.RGBA{R: 255, A: 255})
		var buf bytes.Buffer
		Expect(png.Encode(&buf, img)).To(Succeed())
		stored, err := artwork.StoreChosenCover(buf.Bytes())
		Expect(err).ToNot(HaveOccurred())
		res, err := svc.ChooseAlbumCover(ctx, theAlbum().ID,
			corrections.AlbumCover{StoredCover: stored, Source: "Cover Art Archive: Record"})
		Expect(err).ToNot(HaveOccurred())
		return res
	}

	It("keeps the chosen cover across rescans, even when the album's ID changes", func() {
		choose()
		Expect(coverSource()).To(Equal(artwork.ChosenSource))

		_, err := s.ScanAll(ctx, true)
		Expect(err).ToNot(HaveOccurred())
		Expect(coverSource()).To(Equal(artwork.ChosenSource))

		// An Artist rename gives the album a new ID; the choice follows its folder
		before := theAlbum().ID
		Expect(ds.Correction(ctx).Add(&model.Correction{Kind: model.CorrectionArtistRename,
			MatchKey: model.ArtistNameKey("Band"), From: "Band", To: "The Band"})).To(Succeed())
		_, err = s.ScanFolders(ctx, true, []model.ScanTarget{folder})
		Expect(err).ToNot(HaveOccurred())
		Expect(theAlbum().ID).ToNot(Equal(before))
		Expect(coverSource()).To(Equal(artwork.ChosenSource))
	})

	It("counts the album's Tracks, and goes back to the default cover order on undo", func() {
		res := choose()
		all, err := svc.GetAll(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(all).To(HaveLen(1))
		Expect(all[0].TrackCount).To(Equal(2))
		Expect(all[0].From).To(Equal("Band – Record"))

		_, err = svc.Undo(ctx, res.Correction.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(coverSource()).ToNot(Equal(artwork.ChosenSource))
	})
})
