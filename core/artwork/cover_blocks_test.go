package artwork

import (
	"bytes"
	"context"
	"image/color"
	"io"
	"os"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core/agents"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Blocked album covers", func() {
	const embedPath = "tests/fixtures/artist/an-album/test.mp3"
	var (
		ctx         context.Context
		ds          *tests.MockDataStore
		corrections *tests.MockCorrectionRepo
		trace       *ChainTrace
		r           *resolver
	)

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.DataFolder = conf.NewDir(GinkgoT().TempDir())
		conf.Server.CoverArtPriority = "cover.*, embedded"
		conf.Server.DiscArtPriority = "cover.*, embedded"
		conf.Server.EnableMediaFileCoverArt = true
		repoRoot, err := os.Getwd()
		Expect(err).ToNot(HaveOccurred())
		libRepo := &tests.MockLibraryRepo{}
		libRepo.SetData(model.Libraries{{ID: 0, Path: testFileLibPath(repoRoot)}})
		corrections = &tests.MockCorrectionRepo{}
		folders := &fakeFolderRepo{result: []model.Folder{{ID: "cd1", Path: "tests/fixtures/artist/an-album", ImageFiles: []string{"cover.jpg"}}}}
		ds = &tests.MockDataStore{MockedFolder: folders, MockedLibrary: libRepo, MockedCorrection: corrections}
		ds.Album(ctx).(*tests.MockAlbumRepo).SetData(model.Albums{
			{ID: "al1", Name: "Record", FolderIDs: []string{"cd2", "cd1"}, EmbedArtPath: embedPath},
		})
		ds.MediaFile(ctx).(*tests.MockMediaFileRepo).SetData(model.MediaFiles{
			{ID: "mf1", AlbumID: "al1", DiscNumber: 1, FolderID: "cd1", Path: embedPath, HasCoverArt: true},
		})
		trace = &ChainTrace{}
		ctx = withTrace(context.Background(), trace)
		r = newResolver(ds, agents.GetAgents(&tests.MockDataStore{}, nil), tests.NewMockFFmpeg(""), nil)
	})

	resolveAlbum := func() resolution {
		GinkgoHelper()
		res, err := r.resolve(ctx, model.ArtworkQueueItem{ItemKind: "al", ItemID: "al1"})
		Expect(err).ToNot(HaveOccurred())
		return res
	}
	// hashOf is the content hash of what a resolution yields, as the processor stores it
	hashOf := func(res resolution) string {
		GinkgoHelper()
		Expect(res.reader).ToNot(BeNil())
		data, err := io.ReadAll(res.reader)
		Expect(err).ToNot(HaveOccurred())
		res.reader.Close()
		hash, err := hashImage(bytes.NewReader(data))
		Expect(err).ToNot(HaveOccurred())
		return hash
	}
	block := func(folderID, hash string) {
		GinkgoHelper()
		Expect(corrections.Add(&model.Correction{Kind: model.CorrectionAlbumCoverBlock, MatchKey: folderID,
			ImageHash: hash})).To(Succeed())
	}
	sources := func() (folder, embedded string) {
		GinkgoHelper()
		folder = hashOf(resolveAlbum())
		conf.Server.CoverArtPriority = "embedded"
		embedded = hashOf(resolveAlbum())
		conf.Server.CoverArtPriority = "cover.*, embedded"
		trace = &ChainTrace{}
		ctx = withTrace(context.Background(), trace)
		return folder, embedded
	}

	It("falls back to the next source in the cover order, past a blocked image", func() {
		folderHash, embeddedHash := sources()
		Expect(folderHash).ToNot(Equal(embeddedHash))
		block("cd1", folderHash)

		res := resolveAlbum()
		Expect(res.source).To(Equal("embedded"))
		Expect(hashOf(res)).To(Equal(embeddedHash))
		Expect(trace.Steps()[0].Candidate).To(Equal("cover.*"))
		Expect(trace.Steps()[0].Outcome).To(Equal(OutcomeBlocked))
	})

	It("settles absent when every source is blocked, with no fault", func() {
		folderHash, embeddedHash := sources()
		block("cd1", folderHash)
		block("cd2", embeddedHash)

		res := resolveAlbum()
		Expect(res.reader).To(BeNil())
		Expect(res.localError).To(BeFalse())
		Expect(res.extErr).ToNot(HaveOccurred())
	})

	It("skips a blocked chosen cover too", func() {
		stored, err := StoreChosenCover(pngOf(color.RGBA{R: 255, A: 255}))
		Expect(err).ToNot(HaveOccurred())
		Expect(corrections.Add(&model.Correction{Kind: model.CorrectionAlbumCover, MatchKey: "cd1",
			ImageHash: stored.Hash, ImageMime: stored.Mime})).To(Succeed())
		block("cd1", stored.Hash)

		Expect(resolveAlbum().source).To(Equal("folder"))
	})

	It("ignores blocks of other folders, and blocks being undone", func() {
		folderHash, _ := sources()
		block("elsewhere", folderHash)
		block("cd2", folderHash)
		corrections.All[1].Undoing = true

		Expect(resolveAlbum().source).To(Equal("folder"))
	})

	It("skips blocked images in the album's disc art, falling back to the album cover", func() {
		folderHash, embeddedHash := sources()
		block("cd1", folderHash)
		res, err := r.resolveDisc(ctx, model.DiscArtworkID("al1", 1))
		Expect(err).ToNot(HaveOccurred())
		Expect(res.source).To(Equal("embedded"))

		block("cd1", embeddedHash)
		res, err = r.resolveDisc(ctx, model.DiscArtworkID("al1", 1))
		Expect(err).ToNot(HaveOccurred())
		Expect(res.reader).To(BeNil())
	})

	It("skips a blocked image in the album's tracks' own art", func() {
		_, embeddedHash := sources()
		res, err := r.resolve(ctx, model.ArtworkQueueItem{ItemKind: "mf", ItemID: "mf1"})
		Expect(err).ToNot(HaveOccurred())
		Expect(hashOf(res)).To(Equal(embeddedHash))

		block("cd1", embeddedHash)
		res, err = r.resolve(ctx, model.ArtworkQueueItem{ItemKind: "mf", ItemID: "mf1"})
		Expect(err).ToNot(HaveOccurred())
		Expect(res.reader).To(BeNil())
		Expect(res.localError).To(BeFalse())
	})
})
