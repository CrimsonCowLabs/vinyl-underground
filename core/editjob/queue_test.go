package editjob

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/server/events"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const fakeKind model.EditKind = "fake"

var errRefused = errors.New("refused: the File changed on disk")

// fakeKindRunner is a step kind that writes nothing: it records what ran, can hold a step until
// it is let go, and can refuse or fail a File.
type fakeKindRunner struct {
	mu      sync.Mutex
	ran     []string
	jobs    []string
	running atomic.Int32
	most    atomic.Int32
	// hold, when set, holds every step until a value is sent on it.
	hold chan struct{}
	// started gets the path of each step as it starts, when set.
	started chan string
	fail    map[string]error
	refuse  map[string]error
}

func (f *fakeKindRunner) Check(_ context.Context, step *model.EditJobStep) error {
	return f.refuse[step.MediaFileID]
}

func (f *fakeKindRunner) Run(_ context.Context, jobID string, step *model.EditJobStep) (Outcome, error) {
	n := f.running.Add(1)
	defer f.running.Add(-1)
	for {
		m := f.most.Load()
		if n <= m || f.most.CompareAndSwap(m, n) {
			break
		}
	}
	if f.started != nil {
		f.started <- step.Path
	}
	if f.hold != nil {
		<-f.hold
	}
	f.mu.Lock()
	f.ran = append(f.ran, step.Path)
	f.jobs = append(f.jobs, jobID)
	f.mu.Unlock()
	if err := f.fail[step.Path]; err != nil {
		return Outcome{}, err
	}
	return Outcome{Changed: step.Path != "same.mp3", EntryID: "entry-" + step.Path}, nil
}

func (f *fakeKindRunner) Code(err error) string {
	if errors.Is(err, errRefused) {
		return "changedOnDisk"
	}
	return "internal"
}

func (f *fakeKindRunner) Ran() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.ran...)
}

// spyBroker keeps the edit job events sent.
type spyBroker struct {
	http.Handler
	mu   sync.Mutex
	sent []events.EditJob
}

func (b *spyBroker) SendMessage(_ context.Context, e events.Event) {
	if j, ok := e.(*events.EditJob); ok {
		b.mu.Lock()
		b.sent = append(b.sent, *j)
		b.mu.Unlock()
	}
}
func (b *spyBroker) SendBroadcastMessage(ctx context.Context, e events.Event) { b.SendMessage(ctx, e) }
func (b *spyBroker) Sent() []events.EditJob {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]events.EditJob(nil), b.sent...)
}

func fileSteps(paths ...string) []model.EditJobStep {
	var res []model.EditJobStep
	for _, p := range paths {
		res = append(res, model.EditJobStep{Kind: fakeKind, MediaFileID: "mf-" + p, Path: p, Args: `{}`})
	}
	return res
}

var _ = Describe("Queue", func() {
	var (
		ctx     context.Context
		ds      *tests.MockDataStore
		jobs    *tests.MockEditJobRepo
		journal *tests.MockEditJournalRepo
		broker  *spyBroker
		kind    *fakeKindRunner
		q       *Queue
		run     func()
	)

	BeforeEach(func() {
		ctx = request.WithUser(context.Background(), model.User{ID: "admin-1", IsAdmin: true})
		jobs = &tests.MockEditJobRepo{}
		journal = &tests.MockEditJournalRepo{}
		ds = &tests.MockDataStore{MockedEditJob: jobs, MockedEditJournal: journal}
		broker = &spyBroker{}
		kind = &fakeKindRunner{}
		q = New(ds, broker, map[model.EditKind]StepKind{fakeKind: kind})
		run = func() {
			runCtx, cancel := context.WithCancel(ctx)
			done := make(chan struct{})
			go func() { q.Run(runCtx); close(done) }()
			DeferCleanup(func() {
				cancel()
				if kind.hold != nil {
					close(kind.hold)
				}
				Eventually(done).Should(BeClosed())
			})
		}
	})

	wait := func(job *model.EditJob) *model.EditJob {
		GinkgoHelper()
		got, err := q.Wait(ctx, job.ID, 5*time.Second)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.State).To(Equal(model.EditJobDone))
		return got
	}

	Describe("Enqueue", func() {
		It("keeps a job of one step per File, queued, for the admin", func() {
			job, err := q.Enqueue(ctx, fakeKind, fileSteps("a.mp3", "b.mp3"))
			Expect(err).ToNot(HaveOccurred())
			got, err := q.Job(ctx, job.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.State).To(Equal(model.EditJobQueued))
			Expect(got.UserID).To(Equal("admin-1"))
			Expect(got.Kind).To(Equal(fakeKind))
			Expect(got.Total).To(Equal(2))
			Expect(got.Steps).To(HaveLen(2))
			Expect(got.Steps[0].Path).To(Equal("a.mp3"))
			Expect(got.Steps[0].Position).To(Equal(1))
			Expect(got.Steps[1].Path).To(Equal("b.mp3"))
			Expect(got.Steps[1].State).To(Equal(model.EditStepQueued))
		})

		It("queues nothing when a step's kind refuses it", func() {
			kind.refuse = map[string]error{"mf-b.mp3": errRefused}
			_, err := q.Enqueue(ctx, fakeKind, fileSteps("a.mp3", "b.mp3"))
			Expect(err).To(MatchError(errRefused))
			Expect(jobs.GetAll(0)).To(BeEmpty())
		})

		It("refuses a step of a kind it can't run", func() {
			steps := fileSteps("a.mp3")
			steps[0].Kind = "nonsense"
			Expect(q.Enqueue(ctx, fakeKind, steps)).Error().To(HaveOccurred())
			Expect(jobs.GetAll(0)).To(BeEmpty())
		})

		It("refuses a job that writes a File twice", func() {
			steps := append(fileSteps("a.mp3", "b.mp3"), fileSteps("a.mp3")...)
			Expect(q.Enqueue(ctx, fakeKind, steps)).Error().To(MatchError(ErrSameFileTwice))
			Expect(jobs.GetAll(0)).To(BeEmpty())
		})

		It("refuses a job with no steps", func() {
			Expect(q.Enqueue(ctx, fakeKind, nil)).Error().To(HaveOccurred())
		})
	})

	Describe("Run", func() {
		It("writes a job's Files in order, in the job, and ends it done", func() {
			job, err := q.Enqueue(ctx, fakeKind, fileSteps("a.mp3", "b.mp3", "same.mp3"))
			Expect(err).ToNot(HaveOccurred())
			run()
			got := wait(job)
			Expect(kind.Ran()).To(Equal([]string{"a.mp3", "b.mp3", "same.mp3"}))
			Expect(kind.jobs).To(HaveEach(job.ID))
			Expect(got.Done).To(Equal(3))
			Expect(got.Skipped).To(BeZero())
			Expect(got.Current).To(BeEmpty())
			Expect(got.Steps[0].State).To(Equal(model.EditStepWritten))
			Expect(got.Steps[0].EntryID).To(Equal("entry-a.mp3"))
			Expect(got.Steps[2].State).To(Equal(model.EditStepUnchanged))
		})

		It("runs one job at a time, and one File at a time, oldest job first", func() {
			kind.hold = make(chan struct{})
			first, err := q.Enqueue(ctx, fakeKind, fileSteps("1a.mp3", "1b.mp3"))
			Expect(err).ToNot(HaveOccurred())
			second, err := q.Enqueue(ctx, fakeKind, fileSteps("2a.mp3", "2b.mp3"))
			Expect(err).ToNot(HaveOccurred())
			run()
			for range 4 {
				// Give a second writer the chance to start, were there one.
				Consistently(kind.running.Load, 50*time.Millisecond).Should(BeNumerically("<=", 1))
				kind.hold <- struct{}{}
			}
			wait(first)
			wait(second)
			Expect(kind.most.Load()).To(BeEquivalentTo(1))
			Expect(kind.Ran()).To(Equal([]string{"1a.mp3", "1b.mp3", "2a.mp3", "2b.mp3"}))
		})

		It("shows its progress live: Files done, of how many, and the File being written", func() {
			kind.hold = make(chan struct{})
			kind.started = make(chan string, 1)
			job, err := q.Enqueue(ctx, fakeKind, fileSteps("a.mp3", "b.mp3"))
			Expect(err).ToNot(HaveOccurred())
			run()
			Eventually(kind.started).Should(Receive(Equal("a.mp3")))
			kind.hold <- struct{}{}
			Eventually(kind.started).Should(Receive(Equal("b.mp3")))

			now, err := q.Job(ctx, job.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(now.State).To(Equal(model.EditJobRunning))
			Expect(now.Done).To(Equal(1))
			Expect(now.Total).To(Equal(2))
			Expect(now.Current).To(Equal("b.mp3"))
			Expect(broker.Sent()).To(ContainElement(SatisfyAll(
				HaveField("ID", job.ID), HaveField("State", model.EditJobRunning),
				HaveField("Done", 1), HaveField("Total", 2))))

			kind.hold <- struct{}{}
			wait(job)
			Eventually(broker.Sent).Should(ContainElement(SatisfyAll(
				HaveField("ID", job.ID), HaveField("State", model.EditJobDone), HaveField("Done", 2))))
		})

		It("skips a File it can't write, says why, and carries on with the rest", func() {
			kind.fail = map[string]error{"b.mp3": errRefused}
			job, err := q.Enqueue(ctx, fakeKind, fileSteps("a.mp3", "b.mp3", "c.mp3"))
			Expect(err).ToNot(HaveOccurred())
			run()
			got := wait(job)
			Expect(kind.Ran()).To(Equal([]string{"a.mp3", "b.mp3", "c.mp3"}))
			Expect(got.Done).To(Equal(3))
			Expect(got.Skipped).To(Equal(1))
			Expect(got.Steps[1].State).To(Equal(model.EditStepSkipped))
			Expect(got.Steps[1].Code).To(Equal("changedOnDisk"))
			Expect(got.Steps[1].Error).To(Equal(errRefused.Error()))
			Expect(got.Steps[2].State).To(Equal(model.EditStepWritten))

			list, err := q.Jobs(ctx, 10)
			Expect(err).ToNot(HaveOccurred())
			Expect(list).To(HaveLen(1))
			Expect(list[0].Skipped).To(Equal(1))
			// The list shows the skipped Files only.
			Expect(list[0].Steps).To(HaveLen(1))
			Expect(list[0].Steps[0].Path).To(Equal("b.mp3"))
		})
	})

	It("waits a while, rather than retrying at once, when it can't record a job's progress", func() {
		job, err := q.Enqueue(ctx, fakeKind, fileSteps("a.mp3"))
		Expect(err).ToNot(HaveOccurred())
		var tries atomic.Int32
		jobs.FailPut = func(j *model.EditJob) error {
			if j.State == model.EditJobDone {
				tries.Add(1)
				return errors.New("disk full")
			}
			return nil
		}
		run()
		Eventually(tries.Load).Should(BeEquivalentTo(1))
		Consistently(tries.Load, 200*time.Millisecond).Should(BeEquivalentTo(1))
		got, err := q.Job(ctx, job.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.State).To(Equal(model.EditJobRunning))
		Expect(got.Steps[0].State).To(Equal(model.EditStepWritten))
	})

	Describe("Wait", func() {
		It("gives up after a while, with the job still running", func() {
			kind.hold = make(chan struct{})
			job, err := q.Enqueue(ctx, fakeKind, fileSteps("a.mp3"))
			Expect(err).ToNot(HaveOccurred())
			run()
			got, err := q.Wait(ctx, job.ID, 100*time.Millisecond)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.State).ToNot(Equal(model.EditJobDone))
			kind.hold <- struct{}{}
			wait(job)
		})

		It("returns ErrNotFound for a job that isn't there", func() {
			Expect(q.Wait(ctx, "nope", time.Millisecond)).Error().To(MatchError(model.ErrNotFound))
		})
	})

	Describe("after a restart", func() {
		// interrupted leaves a job as a stop in the middle of its second File would: the first
		// written, the second running, the third queued.
		interrupted := func() *model.EditJob {
			GinkgoHelper()
			job := &model.EditJob{Kind: fakeKind, State: model.EditJobRunning, UserID: "admin-1", Total: 3, Done: 1, Current: "b.mp3"}
			Expect(jobs.Put(job)).To(Succeed())
			for i, st := range []model.EditStepState{model.EditStepWritten, model.EditStepRunning, model.EditStepQueued} {
				s := fileSteps(string(rune('a'+i)) + ".mp3")[0]
				s.JobID, s.Position, s.State = job.ID, i+1, st
				Expect(jobs.PutStep(&s)).To(Succeed())
			}
			return job
		}
		journaled := func(job *model.EditJob, state model.EditState) *model.EditEntry {
			GinkgoHelper()
			e := &model.EditEntry{JobID: job.ID, Kind: model.EditTags, State: state, MediaFileID: "mf-b.mp3",
				Path: "b.mp3", NewPath: "b.mp3", Error: "interrupted by a restart"}
			Expect(journal.Put(e)).To(Succeed())
			return e
		}

		It("takes a File the restart finished as written, then carries on with the rest", func() {
			job := interrupted()
			entry := journaled(job, model.EditWritten)
			// A job queued since waits for the interrupted one.
			queued, err := q.Enqueue(ctx, fakeKind, fileSteps("later.mp3"))
			Expect(err).ToNot(HaveOccurred())
			run()
			got := wait(job)
			wait(queued)
			Expect(kind.Ran()).To(Equal([]string{"c.mp3", "later.mp3"}))
			Expect(got.Steps[1].State).To(Equal(model.EditStepWritten))
			Expect(got.Steps[1].EntryID).To(Equal(entry.ID))
			Expect(got.Done).To(Equal(3))
			Expect(got.Skipped).To(BeZero())
		})

		It("skips a File the restart rolled back, as interrupted, then carries on", func() {
			job := interrupted()
			journaled(job, model.EditFailed)
			run()
			got := wait(job)
			Expect(kind.Ran()).To(Equal([]string{"c.mp3"}))
			Expect(got.Steps[1].State).To(Equal(model.EditStepSkipped))
			Expect(got.Steps[1].Code).To(Equal("interrupted"))
			Expect(got.Skipped).To(Equal(1))
			Expect(got.Done).To(Equal(3))
		})

		It("skips a File the restart couldn't settle yet: the next start settles it", func() {
			job := interrupted()
			journaled(job, model.EditPending)
			run()
			got := wait(job)
			Expect(kind.Ran()).To(Equal([]string{"c.mp3"}))
			Expect(got.Steps[1].State).To(Equal(model.EditStepSkipped))
			Expect(got.Steps[1].Code).To(Equal("interrupted"))
		})

		It("writes a File again that the stop came before anything was written to", func() {
			job := interrupted()
			run()
			got := wait(job)
			Expect(kind.Ran()).To(Equal([]string{"b.mp3", "c.mp3"}))
			Expect(got.Steps[1].State).To(Equal(model.EditStepWritten))
			Expect(got.Skipped).To(BeZero())
		})
	})
})
