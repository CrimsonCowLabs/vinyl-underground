package scanner_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
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
	"github.com/zeebo/xxh3"
)

// "This image is wrong for the album" stays app data once the overlay is gone (#165).
var _ = Describe("Scanning with an album cover marked wrong", Ordered, func() {
	var ctx context.Context
	var ds model.DataStore
	var s model.Scanner
	var svc corrections.Corrections

	const user = "123"

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
		svc = corrections.New(ds)

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
	It("keeps a cover marked wrong blocked across rescans, until undone", func() {
		var buf bytes.Buffer
		Expect(png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2)))).To(Succeed())
		wrong := buf.Bytes()
		record := template(_t{"albumartist": "Band", "artist": "Band", "album": "Record", "year": 1999})
		createFS(fstest.MapFS{
			"Band/Record/01 - One.mp3": record(track(1, "One")),
			"Band/Record/02 - Two.mp3": record(track(2, "Two")),
			"Band/Record/cover.png":    {Data: wrong},
		})
		_, err := s.ScanAll(ctx, true)
		Expect(err).ToNot(HaveOccurred())
		Expect(coverSource()).To(Equal("folder"))

		// The album shows the folder image, as the artwork worker stored it
		hash := fmt.Sprintf("%016x", xxh3.Hash(wrong))
		Expect(ds.Artwork(ctx).PutImage(&model.Artwork{Hash: hash, Mime: "image/png"})).To(Succeed())
		Expect(ds.Artwork(ctx).PutItemArtwork(&model.ItemArtwork{ItemKind: model.KindAlbumArtwork.Prefix(),
			ItemID: theAlbum().ID, ImageType: model.ImageTypePrimary, Hash: hash, Source: "folder", SourcePath: "/music/Band/Record/cover.png"})).To(Succeed())
		res, err := svc.BlockAlbumCover(ctx, theAlbum().ID, hash)
		Expect(err).ToNot(HaveOccurred())
		Expect(res.Correction.To).To(Equal("cover.png"))
		Expect(coverSource()).ToNot(Equal("folder"))

		_, err = s.ScanAll(ctx, true)
		Expect(err).ToNot(HaveOccurred())
		Expect(coverSource()).ToNot(Equal("folder"))
		all, err := svc.GetAll(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(all).To(HaveLen(1))
		Expect(all[0].Kind).To(Equal(model.CorrectionAlbumCoverBlock))
		Expect(all[0].TrackCount).To(Equal(2))

		_, err = svc.Undo(ctx, res.Correction.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(coverSource()).To(Equal("folder"))
	})
})
