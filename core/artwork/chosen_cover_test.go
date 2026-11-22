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
