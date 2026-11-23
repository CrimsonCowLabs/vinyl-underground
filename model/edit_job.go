package model

import "time"

// EditJobState is where an edit job is in its life.
type EditJobState string

const (
	// EditJobQueued is a job waiting for the job before it to finish.
	EditJobQueued EditJobState = "queued"
	// EditJobRunning is the job writing its Files now (or the one a restart interrupted, which
	// carries on first).
	EditJobRunning EditJobState = "running"
	// EditJobDone is a job every step of which has ended: written, unchanged or skipped.
	EditJobDone EditJobState = "done"
)

// EditStepState is how far one step of an edit job got.
type EditStepState string

const (
	EditStepQueued  EditStepState = "queued"
	EditStepRunning EditStepState = "running"
	// EditStepWritten is a step whose write is in the File (and in the Edit journal).
	EditStepWritten EditStepState = "written"
	// EditStepUnchanged is a step with nothing to write: the File already held it.
	EditStepUnchanged EditStepState = "unchanged"
	// EditStepSkipped is a step that wasn't written, and why (Code, Error): the File is as it was.
	EditStepSkipped EditStepState = "skipped"
)

// EditJob is a batch of writes to Files, run in the background (#162, ADR-0011): one step per
// File, one job at a time, so the Library is never written to in parallel. Jobs and their steps
// are kept in the database, so an interrupted job carries on after a restart. Each step's write
// is recorded in the Edit journal with the job's ID, so a whole job can be undone.
type EditJob struct {
	ID     string `structs:"id" json:"id"`
	UserID string `structs:"user_id" json:"userId"`
	// Kind is what the job does (tags, for now).
	Kind  EditKind     `structs:"kind" json:"kind"`
	State EditJobState `structs:"state" json:"state"`
	// Total is how many steps (Files) the job has. Done is how many have ended, Skipped how many
	// of those weren't written. Current is the File being written now, relative to its library.
	Total     int       `structs:"total" json:"total"`
	Done      int       `structs:"done" json:"done"`
	Skipped   int       `structs:"skipped" json:"skipped"`
	Current   string    `structs:"current" json:"current"`
	CreatedAt time.Time `structs:"created_at" json:"createdAt"`
	UpdatedAt time.Time `structs:"updated_at" json:"updatedAt"`

	// Steps are the job's steps, in order, only filled in by callers that ask for them.
	Steps EditJobSteps `structs:"-" json:"steps,omitempty"`
}

type EditJobs []EditJob

// EditJobStep is one write to one File, in an edit job. Kind says what it writes, and Args are
// its input, as JSON, read by the step's kind: for tags, the fields to change. Later kinds
// (moves, Quarantine moves, cover writes, undos) are steps too.
type EditJobStep struct {
	ID    string `structs:"id" json:"id"`
	JobID string `structs:"job_id" json:"jobId"`
	// Position is the step's place in its job, from 1.
	Position    int      `structs:"position" json:"position"`
	Kind        EditKind `structs:"kind" json:"kind"`
	MediaFileID string   `structs:"media_file_id" json:"mediaFileId"`
	// Path is the File's, relative to its library, for showing.
	Path  string        `structs:"path" json:"path"`
	Args  string        `structs:"args" json:"-"`
	State EditStepState `structs:"state" json:"state"`
	// Code is why a step was skipped, a code the UI translates (changedOnDisk, ...), and Error the
	// reason in words. Warning is a note on a written step (e.g. the rescan after it failed).
	Code    string `structs:"code" json:"code,omitempty"`
	Error   string `structs:"error" json:"error,omitempty"`
	Warning string `structs:"warning" json:"warning,omitempty"`
	// EntryID is the step's entry in the Edit journal, once written.
	EntryID   string    `structs:"entry_id" json:"entryId,omitempty"`
	CreatedAt time.Time `structs:"created_at" json:"createdAt"`
	UpdatedAt time.Time `structs:"updated_at" json:"updatedAt"`
}

type EditJobSteps []EditJobStep

type EditJobRepository interface {
	// Put inserts the job (giving it an ID if it has none) or updates it. It doesn't save Steps.
	Put(job *EditJob) error
	Get(id string) (*EditJob, error)
	// GetAll returns the latest jobs, newest first, without Steps (at most limit, 0 for all).
	GetAll(limit int) (EditJobs, error)
	// Next returns the job to run next: an interrupted (running) one first, then the oldest
	// queued, or ErrNotFound.
	Next() (*EditJob, error)
	// PutStep inserts the step (giving it an ID if it has none) or updates it.
	PutStep(step *EditJobStep) error
	// Steps returns the steps of the job, in order; with states, only those in the states.
	Steps(jobID string, states ...EditStepState) (EditJobSteps, error)
}
