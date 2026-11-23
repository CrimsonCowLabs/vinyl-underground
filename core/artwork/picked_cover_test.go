package artwork

import (
	"context"
	"image/color"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core/agents"
	"github.com/navidrome/navidrome/core/musicbrainz"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/tests"
	"github.com/navidrome/navidrome/utils/cache"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fixedFront answers every front cover lookup with its cover, or its error.
type fixedFront struct {
	cover musicbrainz.Cover
	err   error
	asked []string
}

func (f *fixedFront) FrontCover(_ context.Context, releaseID, releaseGroupID string) (musicbrainz.Cover, error) {
	f.asked = append(f.asked, releaseID+"|"+releaseGroupID)
	return f.cover, f.err
}

var _ = Describe("FetchPickedCover", func() {
	var archive *fixedFront
	var image []byte

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.DataFolder = conf.NewDir(GinkgoT().TempDir())
		conf.Server.EnableExternalServices = true
		image = pngOf(color.RGBA{G: 200, A: 255})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/front.png":
				_, _ = w.Write(image)
			case "/page.html":
				_, _ = w.Write([]byte("<html>no image</html>"))
			default:
				http.NotFound(w, r)
			}
		}))
		DeferCleanup(srv.Close)
		archive = &fixedFront{cover: musicbrainz.Cover{URL: srv.URL + "/front.png"}}
		prev := coverArtArchive
		coverArtArchive = func() frontCoverSource { return archive }
		DeferCleanup(func() { coverArtArchive = prev })
	})

	It("keeps the release's (else its release group's) front cover in the chosen-cover store", func() {
		stored, err := FetchPickedCover(context.Background(), "rel-1", "rg-1")
		Expect(err).ToNot(HaveOccurred())
		Expect(archive.asked).To(Equal([]string{"rel-1|rg-1"}))
		Expect(stored.Mime).To(Equal("image/png"))
		data, err := os.ReadFile(ChosenCoverStore().path(stored.Hash, stored.Mime))
		Expect(err).ToNot(HaveOccurred())
		Expect(data).To(Equal(image))
	})

	It("says so when the archive has no front cover", func() {
		archive.err = musicbrainz.ErrNoCover
		_, err := FetchPickedCover(context.Background(), "rel-1", "rg-1")
		Expect(err).To(MatchError(musicbrainz.ErrNoCover))
	})

	It("asks nothing when external services are off", func() {
		conf.Server.EnableExternalServices = false
		_, err := FetchPickedCover(context.Background(), "rel-1", "rg-1")
		Expect(err).To(MatchError(ErrExternalServicesOff))
		Expect(archive.asked).To(BeEmpty())
	})

	It("refuses what is no image", func() {
		archive.cover.URL = archive.cover.URL[:len(archive.cover.URL)-len("front.png")] + "page.html"
		_, err := FetchPickedCover(context.Background(), "rel-1", "rg-1")
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("A picked MusicBrainz release's cover", func() {
	const embedPath = "tests/fixtures/artist/an-album/test.mp3"
	var (
		ctx         context.Context
		ds          *tests.MockDataStore
		corrections *tests.MockCorrectionRepo
		r           *resolver
		trace       *ChainTrace
		picked      []byte
	)

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.DataFolder = conf.NewDir(GinkgoT().TempDir())
		// The default order: the (wrong) folder image and embedded art come before the archive
		conf.Server.CoverArtPriority = "cover.*, embedded, coverartarchive"
		conf.Server.DiscArtPriority = "cover.*, embedded"
		conf.Server.EnableMediaFileCoverArt = true
		trace = &ChainTrace{}
		ctx = withTrace(context.Background(), trace)
		repoRoot, err := os.Getwd()
		Expect(err).ToNot(HaveOccurred())
		libRepo := &tests.MockLibraryRepo{}
		libRepo.SetData(model.Libraries{{ID: 0, Path: testFileLibPath(repoRoot)}})
		corrections = &tests.MockCorrectionRepo{}
		folders := &fakeFolderRepo{result: []model.Folder{{ID: "cd1", Path: "tests/fixtures/artist/an-album", ImageFiles: []string{"cover.jpg"}}}}
		ds = &tests.MockDataStore{MockedFolder: folders, MockedLibrary: libRepo, MockedCorrection: corrections}
		ds.Album(ctx).(*tests.MockAlbumRepo).SetData(model.Albums{
			{ID: "al1", Name: "Record", FolderIDs: []string{"cd1"}, EmbedArtPath: embedPath,
				MbzAlbumID: "rel-1", MbzReleaseGroupID: "rg-1"},
		})
		ds.MediaFile(ctx).(*tests.MockMediaFileRepo).SetData(model.MediaFiles{
			{ID: "mf1", AlbumID: "al1", DiscNumber: 1, FolderID: "cd1", Path: embedPath, HasCoverArt: true},
			{ID: "mf2", AlbumID: "al1", DiscNumber: 1, FolderID: "cd1", Path: embedPath},
		})
		r = newResolver(ds, agents.GetAgents(&tests.MockDataStore{}, nil), tests.NewMockFFmpeg(""), nil)
		picked = pngOf(color.RGBA{B: 255, A: 255})
	})

	pick := func(data []byte) StoredCover {
		GinkgoHelper()
		stored, err := StoreChosenCover(data)
		Expect(err).ToNot(HaveOccurred())
		Expect(corrections.Add(&model.Correction{Kind: model.CorrectionAlbum, MatchKey: "cd1",
			MbzAlbumID: "rel-1", MbzReleaseGroupID: "rg-1", ImageHash: stored.Hash, ImageMime: stored.Mime})).To(Succeed())
		return stored
	}
	resolve := func(kind, id string) resolution {
		GinkgoHelper()
		res, err := r.resolve(ctx, model.ArtworkQueueItem{ItemKind: kind, ItemID: id})
		Expect(err).ToNot(HaveOccurred())
		return res
	}
	resolveDisc := func() resolution {
		GinkgoHelper()
		res, err := r.resolveDisc(ctx, model.DiscArtworkID("al1", 1))
		Expect(err).ToNot(HaveOccurred())
		return res
	}

	It("is the album's cover, its disc art and a Track's own art, over the folder image and embedded art", func() {
		Expect(resolve("al", "al1").source).To(Equal("folder"))
		Expect(resolveDisc().source).To(Equal("folder"))
		Expect(resolve("mf", "mf1").source).To(Equal("embedded"))
		pick(picked)

		trace = &ChainTrace{}
		ctx = withTrace(context.Background(), trace)
		res := resolve("al", "al1")
		Expect(res.source).To(Equal(PickSource))
		Expect(readAll(res.reader)).To(Equal(string(picked)))
		Expect(trace.Steps()[0]).To(Equal(TraceStep{Candidate: PickSource, Outcome: OutcomeHit, Detail: res.sourcePath}))

		res = resolveDisc()
		Expect(res.source).To(Equal(PickSource))
		Expect(readAll(res.reader)).To(Equal(string(picked)))
		res = resolve("mf", "mf1")
		Expect(res.source).To(Equal(PickSource))
		Expect(readAll(res.reader)).To(Equal(string(picked)))
	})

	It("gives way to a chosen cover", func() {
		pick(picked)
		red := pngOf(color.RGBA{R: 255, A: 255})
		chosen, err := StoreChosenCover(red)
		Expect(err).ToNot(HaveOccurred())
		Expect(corrections.Add(&model.Correction{Kind: model.CorrectionAlbumCover, MatchKey: "cd1",
			ImageHash: chosen.Hash, ImageMime: chosen.Mime})).To(Succeed())

		Expect(resolve("al", "al1").source).To(Equal(ChosenSource))
		Expect(resolveDisc().source).To(Equal(ChosenSource))
		Expect(resolve("mf", "mf1").source).To(Equal(ChosenSource))
	})

	It("is passed over once blocked, falling back to the usual order", func() {
		stored := pick(picked)
		Expect(corrections.Add(&model.Correction{Kind: model.CorrectionAlbumCoverBlock, MatchKey: "cd1",
			ImageHash: stored.Hash})).To(Succeed())

		Expect(resolve("al", "al1").source).To(Equal("folder"))
		Expect(resolveDisc().source).To(Equal("folder"))
		Expect(resolve("mf", "mf1").source).To(Equal("embedded"))
	})

	It("falls back to the usual order when the pick brought no cover, or is undone", func() {
		Expect(corrections.Add(&model.Correction{Kind: model.CorrectionAlbum, MatchKey: "cd1",
			MbzAlbumID: "rel-1", MbzReleaseGroupID: "rg-1"})).To(Succeed())
		Expect(resolve("al", "al1").source).To(Equal("folder"))

		corrections.All = nil
		pick(picked)
		corrections.All[0].Undoing = true
		Expect(resolve("al", "al1").source).To(Equal("folder"))
		Expect(resolveDisc().source).To(Equal("folder"))
	})

	It("reaches a Track without art of its own through its disc art, past a warm cache, until undone", func() {
		conf.Server.CacheFolder = conf.NewDir(GinkgoT().TempDir())
		imgCache := cache.NewFileCache("PickServingTest", "100MB", "images", 0,
			func(ctx context.Context, arg cache.Item) (io.Reader, error) {
				return arg.(artworkReader).Reader(ctx)
			})
		Eventually(func() bool { return imgCache.Available(ctx) }, 10*time.Second).Should(BeTrue())
		svc := NewArtwork(ds, imgCache, NewImageStore(GinkgoT().TempDir()), tests.NewMockFFmpeg(""))
		get := func() []byte {
			GinkgoHelper()
			img, err := svc.Get(ctx, model.MustParseArtworkID("mf-mf2"), 0, false)
			Expect(err).ToNot(HaveOccurred())
			defer img.Close()
			data, err := io.ReadAll(img)
			Expect(err).ToNot(HaveOccurred())
			return data
		}

		Expect(get()).ToNot(Equal(picked))
		pick(picked)
		Expect(get()).To(Equal(picked))
		corrections.All = nil
		Expect(get()).ToNot(Equal(picked))
	})
})
