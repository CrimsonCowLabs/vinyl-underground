package artwork

import (
	"context"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core/agents"
	"github.com/navidrome/navidrome/core/musicbrainz"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/tests"
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

var _ = Describe("A MusicBrainz release picked before #170", func() {
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

	It("is no longer shown: the album, its disc art and its Tracks keep the usual order (#170)", func() {
		// A release picked before #170 is written into the Files by the Correction migration, its
		// cover as the album's cover.jpg; until then nothing applies it.
		pick(picked)
		Expect(resolve("al", "al1").source).To(Equal("folder"))
		Expect(resolveDisc().source).To(Equal("folder"))
		Expect(resolve("mf", "mf1").source).To(Equal("embedded"))
	})
})
