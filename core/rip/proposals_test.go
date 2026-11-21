package rip

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/rip/propose"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeProposer proposes "<title> LP" as the album, and records what it was asked. While held,
// each proposal waits for a release, like a slow MusicBrainz.
type fakeProposer struct {
	mu     sync.Mutex
	inputs []propose.Input
	hold   chan struct{}
}

func (p *fakeProposer) Propose(ctx context.Context, in propose.Input) model.RipProposal {
	p.mu.Lock()
	p.inputs = append(p.inputs, in)
	hold := p.hold
	p.mu.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
		}
	}
	s := propose.StartFrom(in)
	return model.RipProposal{Status: model.RipProposalReady, Title: s.Title, Artist: s.Artist,
		Album: s.Title + " LP", AlbumArtist: s.AlbumArtist, Year: "1999", AlbumFrom: propose.AlbumFromMusicBrainz}
}

func (p *fakeProposer) asked() []propose.Input {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]propose.Input(nil), p.inputs...)
}

var _ = Describe("Tag proposals", func() {
	var (
		ctx      context.Context
		ds       *tests.MockDataStore
		inbox    string
		fetcher  *fakeFetcher
		proposer *fakeProposer
		ripper   *Ripper
	)

	BeforeEach(func() {
		ctx = request.WithUser(GinkgoT().Context(), model.User{ID: "admin", IsAdmin: true})
		inbox = GinkgoT().TempDir()
		libs := &tests.MockLibraryRepo{}
		libs.SetData(model.Libraries{{ID: 1, Name: "Music", Path: GinkgoT().TempDir()}})
		libs.PutFn = func(l *model.Library) error {
			if l.ID == 0 {
				l.ID = 2
			}
			libs.Data[l.ID] = *l
			return nil
		}
		ds = &tests.MockDataStore{MockedLibrary: libs}
		_, err := core.EnsureInboxLibrary(ctx, ds, inbox)
		Expect(err).ToNot(HaveOccurred())
		fetcher = newFakeFetcher()
		proposer = &fakeProposer{}
		ripper = New(ds, fetcher, &fakeScanner{inbox: inbox}, Options{
			Timeout: time.Minute, ScanRetryDelay: time.Millisecond, Proposer: proposer,
			Tagger: func(string, *Info) error { return nil },
		})
	})

	// start runs the Ripper. Rips are queued before it starts: the mock store's transactions
	// aren't atomic, so a worker already running could pick a job up before its items are saved.
	start := func() (stop func()) {
		runCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			ripper.Run(runCtx)
		}()
		return func() {
			cancel()
			Eventually(done).Should(BeClosed())
		}
	}

	item := func(jobID string) func() model.RipItem {
		return func() model.RipItem {
			items, err := ds.RipItem(ctx).ForJobs(jobID)
			Expect(err).ToNot(HaveOccurred())
			Expect(items).To(HaveLen(1))
			return items[0]
		}
	}
	jobStatus := func(id string) func() model.RipStatus {
		return func() model.RipStatus {
			j, err := ds.RipJob(ctx).Get(id)
			Expect(err).ToNot(HaveOccurred())
			return j.Status
		}
	}

	const topicURL = "https://music.youtube.com/watch?v=t1"

	topicInfo := func() *Info {
		return &Info{ID: "t1", Title: "Song", Artist: "Band, Guest", Source: "YouTube", SourceURL: topicURL,
			Origin: model.RipOrigin{Title: "Song", Uploader: "Band - Topic", Duration: 201,
				Artist: "Band, Guest", Artists: []string{"Band", "Guest"}, Track: "Song", Album: "Record", ReleaseYear: 1999}}
	}

	It("works out each Track's proposal in the background, after its File has landed, without holding up the Rip", func() {
		fetcher.infos[topicURL] = topicInfo()
		proposer.hold = make(chan struct{})
		job, err := ripper.Enqueue(ctx, topicURL, false)
		Expect(err).ToNot(HaveOccurred())
		defer start()()

		// The Rip is ready for review while MusicBrainz is still being asked.
		Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
		Expect(item(job.ID)().Proposal.Status).To(Equal(model.RipProposalPending))
		Expect(item(job.ID)().Path).ToNot(BeEmpty())

		close(proposer.hold)
		Eventually(func() model.RipProposalStatus { return item(job.ID)().Proposal.Status }).Should(Equal(model.RipProposalReady))
		p := item(job.ID)().Proposal
		Expect(p.Album).To(Equal("Song LP"))
		Expect(p.AlbumArtist).To(Equal("Band"))

		// It was asked from what yt-dlp said, YouTube Music's metadata included.
		Expect(proposer.asked()).To(ConsistOf(propose.Input{
			Artist: "Band - Topic", Title: "Song", Duration: 201 * time.Second,
			YouTubeMusic: &propose.YouTubeMusic{Artist: "Band, Guest", Artists: []string{"Band", "Guest"},
				Track: "Song", Album: "Record", Year: 1999},
		}))
	})

	It("asks from the video's title alone when it isn't a Topic upload", func() {
		const url = "https://www.youtube.com/watch?v=v1"
		fetcher.infos[url] = &Info{ID: "v1", Title: "Band - Song (Official Video)", Artist: "BandVEVO", Source: "YouTube",
			SourceURL: url, Origin: model.RipOrigin{Title: "Band - Song (Official Video)", Uploader: "BandVEVO"}}
		job, err := ripper.Enqueue(ctx, url, false)
		Expect(err).ToNot(HaveOccurred())
		defer start()()

		Eventually(item(job.ID)).Should(HaveField("Proposal.Status", model.RipProposalReady))
		Expect(proposer.asked()).To(ConsistOf(propose.Input{Artist: "BandVEVO", Title: "Band - Song (Official Video)"}))
		Expect(item(job.ID)().Proposal.Title).To(Equal("Song"))
	})

	It("never writes a proposal into the File", func() {
		fetcher.infos[topicURL] = topicInfo()
		job, err := ripper.Enqueue(ctx, topicURL, false)
		Expect(err).ToNot(HaveOccurred())
		defer start()()
		Eventually(func() model.RipProposalStatus { return item(job.ID)().Proposal.Status }).Should(Equal(model.RipProposalReady))

		data, err := os.ReadFile(filepath.Join(inbox, item(job.ID)().Path))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data)).To(Equal("audio"), "the fake download's File, untouched")
	})

	It("works out the proposals a stop left pending, on the next start", func() {
		job := &model.RipJob{URL: topicURL, Status: model.RipReady}
		Expect(ds.RipJob(ctx).Put(job)).To(Succeed())
		it := &model.RipItem{JobID: job.ID, Position: 1, URL: topicURL, Status: model.RipReady, Path: "a.opus",
			Origin: topicInfo().Origin}
		Expect(ds.RipItem(ctx).Put(it)).To(Succeed())
		Expect(ds.RipItem(ctx).SetProposal(it.ID, model.RipProposal{Status: model.RipProposalPending})).To(Succeed())

		defer start()()
		Eventually(func() model.RipProposalStatus { return item(job.ID)().Proposal.Status }).Should(Equal(model.RipProposalReady))
	})

	It("works the proposals out again on demand, keeping the old ones on show meanwhile", func() {
		fetcher.infos[topicURL] = topicInfo()
		job, err := ripper.Enqueue(ctx, topicURL, false)
		Expect(err).ToNot(HaveOccurred())
		defer start()()
		Eventually(func() model.RipProposalStatus { return item(job.ID)().Proposal.Status }).Should(Equal(model.RipProposalReady))
		Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))

		proposer.mu.Lock()
		proposer.hold = make(chan struct{})
		proposer.mu.Unlock()
		Expect(ripper.RefreshProposals(ctx, job.ID)).To(Succeed())
		Expect(item(job.ID)().Proposal.Status).To(Equal(model.RipProposalPending))
		Expect(item(job.ID)().Proposal.Album).To(Equal("Song LP"))

		close(proposer.hold)
		Eventually(func() model.RipProposalStatus { return item(job.ID)().Proposal.Status }).Should(Equal(model.RipProposalReady))
		Expect(proposer.asked()).To(HaveLen(2))
	})

	It("refuses to refresh a Rip that is still running", func() {
		ripper = New(ds, fetcher, &fakeScanner{inbox: inbox}, Options{Proposer: proposer})
		job := &model.RipJob{URL: topicURL, Status: model.RipDownloading}
		Expect(ds.RipJob(ctx).Put(job)).To(Succeed())
		Expect(ripper.RefreshProposals(ctx, job.ID)).To(MatchError(ErrNotReady))
	})
})
