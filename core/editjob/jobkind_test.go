package editjob

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const renameJob model.EditKind = "fakeRename"

// fakeJobKind records what ran, with the steps that had ended.
type fakeJobKind struct {
	mu     sync.Mutex
	calls  []string
	failed int // Done fails this many times first
	ran    *fakeKindRunner
}

func (f *fakeJobKind) record(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, s)
}

func (f *fakeJobKind) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeJobKind) Start(_ context.Context, job *model.EditJob) error {
	// Nothing of the job is written yet.
	f.record("start " + job.ID + " after " + itoa(len(f.ran.Ran())))
	return nil
}

func (f *fakeJobKind) Done(_ context.Context, job *model.EditJob, steps model.EditJobSteps) error {
	f.mu.Lock()
	if f.failed > 0 {
		f.failed--
		f.mu.Unlock()
		return errors.New("the database is busy")
	}
	f.mu.Unlock()
	job.Args = `{"noted":true}`
	f.record("done " + job.ID + " with " + itoa(len(steps)))
	return nil
}

func (f *fakeJobKind) Undone(_ context.Context, job *model.EditJob, steps model.EditJobSteps) error {
	f.record("undone " + job.ID + " with " + itoa(len(steps)))
	return nil
}

func itoa(n int) string { return string(rune('0' + n)) }

var _ = Describe("Job kinds", func() {
	var (
		ctx    context.Context
		jobs   *tests.MockEditJobRepo
		runner *fakeKindRunner
		kind   *fakeJobKind
		q      *Queue
	)

	BeforeEach(func() {
		ctx = request.WithUser(context.Background(), model.User{ID: "admin-1", IsAdmin: true})
		jobs = &tests.MockEditJobRepo{}
		ds := &tests.MockDataStore{MockedEditJob: jobs, MockedEditJournal: &tests.MockEditJournalRepo{}}
		runner = &fakeKindRunner{}
		kind = &fakeJobKind{ran: runner}
		q = New(ds, nil, map[model.EditKind]StepKind{fakeKind: runner})
		q.Handle(renameJob, kind)
		runCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() { q.Run(runCtx); close(done) }()
		DeferCleanup(func() { cancel(); Eventually(done).Should(BeClosed()) })
	})

	wait := func(job *model.EditJob) *model.EditJob {
		GinkgoHelper()
		got, err := q.Wait(ctx, job.ID, 5*time.Second)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.State).To(Equal(model.EditJobDone))
		return got
	}

	It("starts a job before its first File, and finishes it after its last, keeping what it noted", func() {
		job, err := q.EnqueueWith(ctx, renameJob, `{"from":"A"}`, fileSteps("a.mp3", "b.mp3"))
		Expect(err).ToNot(HaveOccurred())
		wait(job)
		Expect(kind.Calls()).To(Equal([]string{"start " + job.ID + " after 0", "done " + job.ID + " with 2"}))
		got, err := jobs.Get(job.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Args).To(Equal(`{"noted":true}`))
	})

	It("tries finishing a job again, and only then is it done", func() {
		kind.failed = 1
		job, err := q.EnqueueWith(ctx, renameJob, "", fileSteps("a.mp3"))
		Expect(err).ToNot(HaveOccurred())
		// The next try is a minute away: waking the queue with another job brings it forward.
		Eventually(func() model.EditJobState {
			got, _ := jobs.Get(job.ID)
			return got.State
		}).Should(Equal(model.EditJobRunning))
		other, err := q.Enqueue(ctx, fakeKind, fileSteps("c.mp3"))
		Expect(err).ToNot(HaveOccurred())
		wait(job)
		wait(other)
		Expect(kind.Calls()).To(ContainElement("done " + job.ID + " with 1"))
		Expect(runner.Ran()).To(Equal([]string{"a.mp3", "c.mp3"}))
	})

	It("gives up on a kind that keeps failing, so the jobs after it still run", func() {
		q.hookTries = 1
		kind.failed = 100
		job, err := q.EnqueueWith(ctx, renameJob, "", fileSteps("a.mp3"))
		Expect(err).ToNot(HaveOccurred())
		other, err := q.Enqueue(ctx, fakeKind, fileSteps("b.mp3"))
		Expect(err).ToNot(HaveOccurred())
		wait(job)
		wait(other)
		Expect(runner.Ran()).To(Equal([]string{"a.mp3", "b.mp3"}))
	})

	It("undoes a job's kind once the undo's Files are written, and a redo starts and finishes it again", func() {
		job, err := q.EnqueueWith(ctx, renameJob, "", fileSteps("a.mp3", "b.mp3"))
		Expect(err).ToNot(HaveOccurred())
		wait(job)

		undo, err := q.EnqueueWith(ctx, model.EditUndo, UndoJobArgs(job.ID), fileSteps("b.mp3", "a.mp3"))
		Expect(err).ToNot(HaveOccurred())
		wait(undo)
		Expect(kind.Calls()).To(HaveLen(3))
		Expect(kind.Calls()[2]).To(Equal("undone " + job.ID + " with 2"))

		redo, err := q.EnqueueWith(ctx, model.EditUndo, UndoJobArgs(undo.ID), fileSteps("a.mp3"))
		Expect(err).ToNot(HaveOccurred())
		wait(redo)
		Expect(kind.Calls()[3:]).To(Equal([]string{"start " + job.ID + " after 4", "done " + job.ID + " with 1"}))
	})

	It("runs no kind for an undo of single Files, or of a job of a kind with none", func() {
		job, err := q.Enqueue(ctx, fakeKind, fileSteps("a.mp3"))
		Expect(err).ToNot(HaveOccurred())
		wait(job)
		undo, err := q.EnqueueWith(ctx, model.EditUndo, UndoJobArgs(job.ID), fileSteps("a.mp3"))
		Expect(err).ToNot(HaveOccurred())
		wait(undo)
		single, err := q.Enqueue(ctx, model.EditUndo, fileSteps("b.mp3"))
		Expect(err).ToNot(HaveOccurred())
		wait(single)
		Expect(kind.Calls()).To(BeEmpty())
	})
})
