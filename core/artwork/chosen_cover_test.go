package artwork

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"time"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core/agents"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/tests"
	"github.com/navidrome/navidrome/utils/cache"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// pngOf is a small, real PNG of one colour, so each colour hashes differently.
func pngOf(c color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for x := range 4 {
		for y := range 4 {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	Expect(png.Encode(&buf, img)).To(Succeed())
	return buf.Bytes()
}

var _ = Describe("Chosen album covers", func() {
	var (
		ctx         context.Context
		ds          *tests.MockDataStore
		corrections *tests.MockCorrectionRepo
		trace       *ChainTrace
		resolve     func(id string) resolution
	)

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.DataFolder = conf.NewDir(GinkgoT().TempDir())
		conf.Server.CoverArtPriority = "embedded"
		repoRoot, err := os.Getwd()
		Expect(err).ToNot(HaveOccurred())
		libRepo := &tests.MockLibraryRepo{}
		libRepo.SetData(model.Libraries{{ID: 0, Path: testFileLibPath(repoRoot)}})
		corrections = &tests.MockCorrectionRepo{}
		ds = &tests.MockDataStore{MockedFolder: &fakeFolderRepo{}, MockedLibrary: libRepo, MockedCorrection: corrections}
		ds.Album(ctx).(*tests.MockAlbumRepo).SetData(model.Albums{
			{ID: "al1", Name: "Record", FolderIDs: []string{"cd2", "cd1"}, EmbedArtPath: "tests/fixtures/artist/an-album/test.mp3"},
		})
		trace = &ChainTrace{}
		ctx = withTrace(context.Background(), trace)
		resolve = func(id string) resolution {
			res, err := newResolver(ds, agents.GetAgents(&tests.MockDataStore{}, nil), tests.NewMockFFmpeg(""), nil).
				resolve(ctx, model.ArtworkQueueItem{ItemKind: "al", ItemID: id})
			Expect(err).ToNot(HaveOccurred())
			return res
		}
	})

	choose := func(folderID string, data []byte) StoredCover {
		GinkgoHelper()
		stored, err := StoreChosenCover(data)
		Expect(err).ToNot(HaveOccurred())
		Expect(corrections.Add(&model.Correction{Kind: model.CorrectionAlbumCover, MatchKey: folderID,
			ImageHash: stored.Hash, ImageMime: stored.Mime})).To(Succeed())
		return stored
	}

	It("stores a chosen image once, by its content", func() {
		red := pngOf(color.RGBA{R: 255, A: 255})
		first, err := StoreChosenCover(red)
		Expect(err).ToNot(HaveOccurred())
		Expect(first.Mime).To(Equal("image/png"))
		again, err := StoreChosenCover(red)
		Expect(err).ToNot(HaveOccurred())
		Expect(again).To(Equal(first))

		_, err = StoreChosenCover([]byte("<html>not an image</html>"))
		Expect(err).To(HaveOccurred())
	})

	It("makes the chosen image the album's cover, over every source in CoverArtPriority", func() {
		red := pngOf(color.RGBA{R: 255, A: 255})
		choose("cd2", red)

		res := resolve("al1")
		Expect(res.reader).ToNot(BeNil())
		Expect(readAll(res.reader)).To(Equal(string(red)))
		Expect(res.source).To(Equal(ChosenSource))
		Expect(res.sourcePath).ToNot(BeEmpty())
		Expect(trace.Steps()).To(HaveLen(1))
		Expect(trace.Steps()[0].Candidate).To(Equal(ChosenSource))
		Expect(trace.Steps()[0].Outcome).To(Equal(OutcomeHit))
	})

	It("ignores a choice being undone, going back to the default cover order", func() {
		choose("cd1", pngOf(color.RGBA{G: 255, A: 255}))
		corrections.All[0].Undoing = true

		res := resolve("al1")
		Expect(res.source).To(Equal("embedded"))
	})

	It("falls back to the default cover order when the stored image is gone", func() {
		stored := choose("cd1", pngOf(color.RGBA{B: 255, A: 255}))
		Expect(os.Remove(ChosenCoverStore().path(stored.Hash, stored.Mime))).To(Succeed())

		res := resolve("al1")
		Expect(res.source).To(Equal("embedded"))
		Expect(trace.Steps()[0]).To(Equal(TraceStep{Candidate: ChosenSource, Outcome: OutcomeMiss}))
	})

	It("leaves albums in other folders alone", func() {
		choose("elsewhere", pngOf(color.RGBA{R: 255, A: 255}))
		res := resolve("al1")
		Expect(res.source).To(Equal("embedded"))
	})
})

var _ = Describe("Chosen album covers on disc and Track art", func() {
	const embedPath = "tests/fixtures/artist/an-album/test.mp3"
	var (
		ctx         context.Context
		ds          *tests.MockDataStore
		corrections *tests.MockCorrectionRepo
		r           *resolver
		red         []byte
	)

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.DataFolder = conf.NewDir(GinkgoT().TempDir())
		conf.Server.CoverArtPriority = "cover.*, embedded"
		conf.Server.DiscArtPriority = "cover.*, embedded"
		conf.Server.EnableMediaFileCoverArt = true
		ctx = withTrace(context.Background(), &ChainTrace{})
		repoRoot, err := os.Getwd()
		Expect(err).ToNot(HaveOccurred())
		libRepo := &tests.MockLibraryRepo{}
		libRepo.SetData(model.Libraries{{ID: 0, Path: testFileLibPath(repoRoot)}})
		corrections = &tests.MockCorrectionRepo{}
		folders := &fakeFolderRepo{result: []model.Folder{{ID: "cd1", Path: "tests/fixtures/artist/an-album", ImageFiles: []string{"cover.jpg"}}}}
		ds = &tests.MockDataStore{MockedFolder: folders, MockedLibrary: libRepo, MockedCorrection: corrections}
		ds.Album(ctx).(*tests.MockAlbumRepo).SetData(model.Albums{
			{ID: "al1", Name: "Record", FolderIDs: []string{"cd1"}, EmbedArtPath: embedPath},
		})
		ds.MediaFile(ctx).(*tests.MockMediaFileRepo).SetData(model.MediaFiles{
			{ID: "mf1", AlbumID: "al1", DiscNumber: 1, FolderID: "cd1", Path: embedPath, HasCoverArt: true},
			{ID: "mf2", AlbumID: "al1", DiscNumber: 1, FolderID: "cd1", Path: embedPath},
		})
		r = newResolver(ds, agents.GetAgents(&tests.MockDataStore{}, nil), tests.NewMockFFmpeg(""), nil)
		red = pngOf(color.RGBA{R: 255, A: 255})
	})

	choose := func(data []byte) StoredCover {
		GinkgoHelper()
		stored, err := StoreChosenCover(data)
		Expect(err).ToNot(HaveOccurred())
		Expect(corrections.Add(&model.Correction{Kind: model.CorrectionAlbumCover, MatchKey: "cd1",
			ImageHash: stored.Hash, ImageMime: stored.Mime})).To(Succeed())
		return stored
	}
	resolveDisc := func() resolution {
		GinkgoHelper()
		res, err := r.resolveDisc(ctx, model.DiscArtworkID("al1", 1))
		Expect(err).ToNot(HaveOccurred())
		return res
	}
	resolveTrack := func(id string) resolution {
		GinkgoHelper()
		res, err := r.resolve(ctx, model.ArtworkQueueItem{ItemKind: "mf", ItemID: id})
		Expect(err).ToNot(HaveOccurred())
		return res
	}

	It("shows the chosen image as the album's disc art, over DiscArtPriority", func() {
		Expect(resolveDisc().source).To(Equal("folder"))
		choose(red)

		res := resolveDisc()
		Expect(res.source).To(Equal(ChosenSource))
		Expect(readAll(res.reader)).To(Equal(string(red)))
	})

	It("shows the chosen image as a Track's own art, over its embedded art, until undone", func() {
		Expect(resolveTrack("mf1").source).To(Equal("embedded"))
		choose(red)

		res := resolveTrack("mf1")
		Expect(res.source).To(Equal(ChosenSource))
		Expect(readAll(res.reader)).To(Equal(string(red)))

		corrections.All = nil
		Expect(resolveTrack("mf1").source).To(Equal("embedded"))
		Expect(resolveDisc().source).To(Equal("folder"))
	})

	It("passes over a chosen image that is blocked, in disc and track art alike", func() {
		stored := choose(red)
		Expect(corrections.Add(&model.Correction{Kind: model.CorrectionAlbumCoverBlock, MatchKey: "cd1",
			ImageHash: stored.Hash})).To(Succeed())

		Expect(resolveTrack("mf1").source).To(Equal("embedded"))
		Expect(resolveDisc().source).To(Equal("folder"))
	})

	Describe("serving", func() {
		var svc Artwork

		BeforeEach(func() {
			conf.Server.CacheFolder = conf.NewDir(GinkgoT().TempDir())
			imgCache := cache.NewFileCache("ChosenServingTest", "100MB", "images", 0,
				func(ctx context.Context, arg cache.Item) (io.Reader, error) {
					return arg.(artworkReader).Reader(ctx)
				})
			Eventually(func() bool { return imgCache.Available(ctx) }, 10*time.Second).Should(BeTrue())
			svc = NewArtwork(ds, imgCache, NewImageStore(GinkgoT().TempDir()), tests.NewMockFFmpeg(""))
		})

		get := func(id string, size int) []byte {
			GinkgoHelper()
			img, err := svc.Get(ctx, model.MustParseArtworkID(id), size, false)
			Expect(err).ToNot(HaveOccurred())
			defer img.Close()
			data, err := io.ReadAll(img)
			Expect(err).ToNot(HaveOccurred())
			return data
		}
		isRed := func(data []byte) bool {
			GinkgoHelper()
			img, _, err := image.Decode(bytes.NewReader(data))
			Expect(err).ToNot(HaveOccurred())
			red, green, blue, _ := img.At(0, 0).RGBA()
			return red > 0xe000 && green < 0x2000 && blue < 0x2000
		}

		It("serves a track with embedded art and no state row yet the chosen image", func() {
			choose(red)
			Expect(get("mf-mf1", 0)).To(Equal(red))
		})

		It("serves a track without embedded art the chosen image through its disc art, past a warm cache", func() {
			Expect(isRed(get("mf-mf2", 64))).To(BeFalse())
			choose(red)
			Expect(isRed(get("mf-mf2", 64))).To(BeTrue())

			corrections.All = nil
			Expect(isRed(get("mf-mf2", 64))).To(BeFalse())
		})
	})
})
