// Package editjob runs the app's writes to Files as background jobs (#162, ADR-0011). A job is a
// list of steps, one per File, kept in the database. Jobs run one at a time, and a job's steps
// one at a time, in order, so the Library is never written to in parallel. A job a restart
// interrupted carries on first, from where it stopped.
//
// What a step writes is up to its kind (StepKind): tag edits for now; moves, Quarantine moves,
// cover writes and undos later. Each kind writes through the Edit journal, with the job's ID.
package editjob

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/server/events"
)

// CodeInterrupted is the code of a File a restart interrupted, and that was rolled back (or is,
// at the next start): it wasn't written.
const CodeInterrupted = "interrupted"

// Outcome is what a step did to its File.
type Outcome struct {
	// Changed is false for a File that already held what the step would write.
	Changed bool
	// EntryID is the write's entry in the Edit journal.
	EntryID string
	// Warning is a note on a File that was written (e.g. its rescan failed).
	Warning string
}

// StepKind writes one kind of step.
type StepKind interface {
	// Check refuses a step before it is queued, cheaply, without reading its File: e.g. a Track
	// that is gone, or whose library is read-only. It may fill in the step's Path.
	Check(ctx context.Context, step *model.EditJobStep) error
	// Run writes the step's File, as part of the job jobID, through the Edit journal. An error
	// means the File wasn't written, and is as it was.
	Run(ctx context.Context, jobID string, step *model.EditJobStep) (Outcome, error)
	// Code is the code (which the UI translates) of an error Run returned: why the File was
	// skipped.
	Code(err error) string
}

// Queue keeps the edit jobs and runs them, in the background (see Run).
type Queue struct {
	ds     model.DataStore
	broker events.Broker
	kinds  map[model.EditKind]StepKind
	wake   chan struct{}

	// mu guards changed, which is closed (and replaced) whenever a job changes, for Wait.
	mu      sync.Mutex
	changed chan struct{}
}

// New returns a Queue that runs steps of the given kinds.
func New(ds model.DataStore, broker events.Broker, kinds map[model.EditKind]StepKind) *Queue {
	if broker == nil {
		broker = events.NoopBroker()
	}
	return &Queue{ds: ds, broker: broker, kinds: kinds, wake: make(chan struct{}, 1), changed: make(chan struct{})}
}

// Enqueue queues a job of the given kind, for the user in ctx, with one step per File, in
// order. Every step is checked by its kind first: if any is refused, nothing is queued.
func (q *Queue) Enqueue(ctx context.Context, kind model.EditKind, steps []model.EditJobStep) (*model.EditJob, error) {
	if len(steps) == 0 {
		return nil, errors.New("an edit job needs at least one File")
	}
	for i := range steps {
		s := &steps[i]
		s.Kind = cmp.Or(s.Kind, kind)
		k, ok := q.kinds[s.Kind]
		if !ok {
			return nil, fmt.Errorf("no edit step of kind %q", s.Kind)
		}
		if err := k.Check(ctx, s); err != nil {
			return nil, err
		}
	}
	job := &model.EditJob{Kind: kind, State: model.EditJobQueued, Total: len(steps)}
	if u, ok := request.UserFrom(ctx); ok {
		job.UserID = u.ID
	}
	err := q.ds.WithTx(func(tx model.DataStore) error {
		repo := tx.EditJob(ctx)
		if err := repo.Put(job); err != nil {
			return err
		}
		for i := range steps {
			s := &steps[i]
			s.ID, s.JobID, s.Position, s.State = "", job.ID, i+1, model.EditStepQueued
			if err := repo.PutStep(s); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("queueing an edit job: %w", err)
	}
	log.Info(ctx, "Queued an edit job", "job", job.ID, "kind", kind, "files", job.Total)
	q.changedJob(ctx, job)
	select {
	case q.wake <- struct{}{}:
	default:
	}
	return job, nil
}

// Job returns the job id, with all its steps.
func (q *Queue) Job(ctx context.Context, id string) (*model.EditJob, error) {
	repo := q.ds.EditJob(ctx)
	job, err := repo.Get(id)
	if err != nil {
		return nil, err
	}
	if job.Steps, err = repo.Steps(id); err != nil {
		return nil, err
	}
	return job, nil
}

// Jobs returns the latest jobs (at most limit), newest first, each with only its skipped steps.
func (q *Queue) Jobs(ctx context.Context, limit int) (model.EditJobs, error) {
	repo := q.ds.EditJob(ctx)
	jobs, err := repo.GetAll(limit)
	if err != nil {
		return nil, err
	}
	for i := range jobs {
		jobs[i].Steps = model.EditJobSteps{}
		if jobs[i].Skipped == 0 {
			continue
		}
		if jobs[i].Steps, err = repo.Steps(jobs[i].ID, model.EditStepSkipped); err != nil {
			return nil, err
		}
	}
	return jobs, nil
}

// Wait waits up to d for the job id to end, and returns it (with all its steps) as it is then:
// done, or still queued or running.
func (q *Queue) Wait(ctx context.Context, id string, d time.Duration) (*model.EditJob, error) {
	timeout := time.NewTimer(d)
	defer timeout.Stop()
	for {
		q.mu.Lock()
		changed := q.changed
		q.mu.Unlock()
		job, err := q.Job(ctx, id)
		if err != nil || job.State == model.EditJobDone {
			return job, err
		}
		select {
		case <-changed:
		case <-timeout.C:
			return job, nil
		case <-ctx.Done():
			return job, nil
		}
	}
}

// Run runs the queued jobs, one at a time, until ctx ends: a job a stop interrupted first, then
// the oldest queued. Run it after the Edit journal is settled (tagedit.Editor.Settle), so a File
// the stop cut off is finished or rolled back before its job carries on.
func (q *Queue) Run(ctx context.Context) {
	for ctx.Err() == nil {
		job, err := q.ds.EditJob(ctx).Next()
		switch {
		case err == nil:
			if q.process(ctx, job) {
				continue
			}
			// Its progress couldn't be recorded: it is tried again in a while.
		case !errors.Is(err, model.ErrNotFound):
			log.Error(ctx, "Could not read the edit job queue", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-q.wake:
		case <-time.After(time.Minute):
		}
	}
}

// process runs the job's steps still to do, in order. A step that fails is skipped, with its
// reason, and the others still run. If ctx ends, the job stops between two Files, still running,
// and carries on at the next start. It returns false when the job's Files couldn't be read or
// recorded.
func (q *Queue) process(ctx context.Context, job *model.EditJob) bool {
	repo := q.ds.EditJob(ctx)
	steps, err := repo.Steps(job.ID)
	if err != nil {
		log.Error(ctx, "Could not read an edit job's Files", "job", job.ID, err)
		return false
	}
	// The job's writes are the admin's who queued it.
	if job.UserID != "" {
		if u, err := q.ds.User(ctx).Get(job.UserID); err == nil {
			ctx = request.WithUser(ctx, *u)
		}
	}
	if job.State == model.EditJobQueued {
		log.Info(ctx, "Starting an edit job", "job", job.ID, "files", len(steps))
	} else {
		log.Info(ctx, "Carrying on with an edit job a stop interrupted", "job", job.ID)
	}
	job.State = model.EditJobRunning
	for i := range steps {
		s := &steps[i]
		if s.State == model.EditStepRunning {
			q.recover(ctx, job, s)
		}
		if s.State != model.EditStepQueued {
			continue
		}
		if ctx.Err() != nil {
			return true
		}
		job.Current = s.Path
		q.save(ctx, job, steps)
		s.State = model.EditStepRunning
		if err := repo.PutStep(s); err != nil {
			log.Error(ctx, "Could not record an edit job's File as started", "job", job.ID, "path", s.Path, err)
			return false
		}
		q.runStep(ctx, job, s)
		if err := repo.PutStep(s); err != nil {
			log.Error(ctx, "Could not record how an edit job's File ended", "job", job.ID, "path", s.Path, "state", s.State, err)
		}
	}
	job.State, job.Current = model.EditJobDone, ""
	q.save(ctx, job, steps)
	log.Info(ctx, "Finished an edit job", "job", job.ID, "files", job.Total, "skipped", job.Skipped)
	return true
}

// runStep writes the step's File, and records its outcome in the step.
func (q *Queue) runStep(ctx context.Context, job *model.EditJob, s *model.EditJobStep) {
	k, ok := q.kinds[s.Kind]
	if !ok {
		s.State, s.Code, s.Error = model.EditStepSkipped, "internal", fmt.Sprintf("no edit step of kind %q", s.Kind)
		return
	}
	// Once started, a File's write runs to the end, even if the app is stopping.
	out, err := k.Run(context.WithoutCancel(ctx), job.ID, s)
	if err != nil {
		s.State, s.Code, s.Error = model.EditStepSkipped, k.Code(err), err.Error()
		log.Warn(ctx, "An edit job skipped a File", "job", job.ID, "path", s.Path, "code", s.Code, err)
		return
	}
	s.State, s.EntryID, s.Warning = model.EditStepWritten, out.EntryID, out.Warning
	if !out.Changed {
		s.State = model.EditStepUnchanged
	}
}

// recover settles a step a stop interrupted, from its entry in the Edit journal, which the
// startup settle has already finished or rolled back: written, it is written; rolled back, or
// still pending (the next start settles it), it is skipped as interrupted. With no entry the
// File was never touched, and the step runs again.
func (q *Queue) recover(ctx context.Context, job *model.EditJob, s *model.EditJobStep) {
	entries, err := q.ds.EditJournal(ctx).ForJob(job.ID)
	if err != nil {
		log.Error(ctx, "Could not read an interrupted edit job's journal", "job", job.ID, err)
		s.State, s.Code, s.Error = model.EditStepSkipped, CodeInterrupted, "interrupted by a restart"
		return
	}
	var entry *model.EditEntry
	for i := range entries {
		// A job has one step per File: its latest entry for the File is the step's.
		if entries[i].MediaFileID == s.MediaFileID {
			entry = &entries[i]
		}
	}
	switch {
	case entry == nil:
		s.State = model.EditStepQueued
	case entry.State == model.EditWritten:
		s.State, s.EntryID = model.EditStepWritten, entry.ID
	case entry.State == model.EditFailed:
		s.State, s.Code, s.EntryID = model.EditStepSkipped, CodeInterrupted, entry.ID
		s.Error = cmp.Or(entry.Error, "interrupted by a restart")
	default:
		s.State, s.Code, s.EntryID = model.EditStepSkipped, CodeInterrupted, entry.ID
		s.Error = "interrupted by a restart; the File is put back as it was at the next start"
	}
	log.Info(ctx, "Settled an edit job's interrupted File", "job", job.ID, "path", s.Path, "state", s.State)
	if s.State != model.EditStepQueued {
		if err := q.ds.EditJob(ctx).PutStep(s); err != nil {
			log.Error(ctx, "Could not record an interrupted File's outcome", "job", job.ID, "path", s.Path, err)
		}
	}
}

// save records the job's progress, counted from its steps, and tells the UI.
func (q *Queue) save(ctx context.Context, job *model.EditJob, steps model.EditJobSteps) {
	job.Done, job.Skipped = 0, 0
	for _, s := range steps {
		switch s.State {
		case model.EditStepWritten, model.EditStepUnchanged:
			job.Done++
		case model.EditStepSkipped:
			job.Done++
			job.Skipped++
		}
	}
	if err := q.ds.EditJob(ctx).Put(job); err != nil {
		log.Error(ctx, "Could not record an edit job's progress", "job", job.ID, err)
	}
	q.changedJob(ctx, job)
}

// changedJob tells Wait, and the UI, that the job changed.
func (q *Queue) changedJob(ctx context.Context, job *model.EditJob) {
	q.mu.Lock()
	close(q.changed)
	q.changed = make(chan struct{})
	q.mu.Unlock()
	q.broker.SendBroadcastMessage(ctx, &events.EditJob{ID: job.ID, State: job.State, Total: job.Total,
		Done: job.Done, Skipped: job.Skipped, Current: job.Current})
}
