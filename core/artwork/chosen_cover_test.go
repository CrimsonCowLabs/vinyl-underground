package artwork

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core/agents"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/tests"
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

var _ = Describe("The chosen-cover store", func() {
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

	// A chosen cover is the album's cover.jpg now (#164), and the choices kept before are written
	// as cover.jpg by the Correction migration (#165): one not yet migrated isn't applied on read.
	It("doesn't apply a chosen cover Correction kept before #164", func() {
		choose("cd2", pngOf(color.RGBA{R: 255, A: 255}))
		res := resolve("al1")
		Expect(res.source).To(Equal("embedded"))
	})
})

var _ = Describe("Chosen album covers kept before #164, on disc and Track art", func() {
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

	It("shows no chosen cover Correction kept before #164 as disc or Track art", func() {
		choose(red)
		Expect(resolveTrack("mf1").source).To(Equal("embedded"))
		Expect(resolveDisc().source).To(Equal("folder"))
	})
})
