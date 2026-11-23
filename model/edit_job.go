package model

import "time"

// EditUndo is the kind of an edit job, or of one of its steps, that undoes Edit journal entries
// (one per step). It is never an entry's kind: an undo's entry has the kind of the entry it
// undoes, with UndoOf set.
const EditUndo EditKind = "undo"

// EditRewrite is the kind of a step that edits a File's tags as worked out from the tags the File
// holds when it is written (see editjob.RewriteStep): e.g. renaming one of its artists.
const EditRewrite EditKind = "rewrite"

// EditOrganize is the kind of an Organize job (#167): one move step (EditMove) per File, whose
// JobKind removes the folders the moves left empty and rescans the Files' old and new folders.
const EditOrganize EditKind = "organize"

// EditPromote is the kind of a Promote job (#171): an Organize job whose move steps (EditMove,
// each with a NewLibraryID) take reviewed Files from the Inbox into the Library; its JobKind also
// settles the Files' Rips once they have moved (or moved back, for its undo).
const EditPromote EditKind = "promote"

// EditRestore is the kind of a step that moves a File in Quarantine that no journaled move put
// there back to where it came from (#168): Files the library-cleanup recipes moved by hand. A
// File the app moved there is restored by undoing that move.
const EditRestore EditKind = "restore"

// EditEmptyQuarantine is the kind of the edit job, and of its one step, that deletes the Files in
// Quarantine for good (#168): the only way the app deletes a File. Its entries are EditDelete.
const EditEmptyQuarantine EditKind = "emptyQuarantine"

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
	// Kind is what the job does (tags, undo, or a job kind of its own, see editjob.JobKind).
	Kind EditKind `structs:"kind" json:"kind"`
	// Args are the job's own input, as JSON, read by its kind's JobKind (e.g. the Artist an artist
	// rename job renames). Most jobs have none: their steps hold all they write.
	Args  string       `structs:"args" json:"-"`
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

	// KeepArtistAnnotations keeps, with the artist rename (or merge) job jobID, every Listener's
	// star and rating of the Artist fromID, as they are before the job writes its Files: once the
	// Files name the Artist's new name, fromID is gone, and so are its annotations. Keeping again
	// replaces what was kept. Play counts aren't kept: each scan counts them from the Tracks.
	KeepArtistAnnotations(jobID, fromID string) error
	// MergeArtistAnnotations merges what the job kept into Artist toID's annotations, as an artist
	// merge Correction does: toID keeps its own star and rating, taking the kept ones only where
	// it has none, and fromID loses them. It merges what was kept once only, so it can run again.
	MergeArtistAnnotations(jobID, fromID, toID string) error
	// SplitArtistAnnotations undoes MergeArtistAnnotations: fromID gets back what was kept, and
	// toID loses the star and rating it took, unless they changed since. With all (a rename: toID
	// is only fromID's new name), fromID also gets the star and rating toID got since. Nothing is
	// kept afterwards.
	SplitArtistAnnotations(jobID, fromID, toID string, all bool) error

	// The same, for one part of a job that renames several Artists (the Correction migration,
	// #165): each part keeps its own. The methods above are part "".
	KeepArtistAnnotationsIn(jobID, part, fromID string) error
	MergeArtistAnnotationsIn(jobID, part, fromID, toID string) error
	SplitArtistAnnotationsIn(jobID, part, fromID, toID string, all bool) error
	// AdoptCorrectionAnnotations keeps with the job's part what the artist Correction
	// correctionID kept (see CorrectionRepository.KeepArtistAnnotations), merged already (an
	// artist merge's) or not (a plain rename's, which MergeArtistAnnotationsIn merges into the new
	// name again), so the job's undo splits it (SplitArtistAnnotationsIn) once the Correction is
	// gone. It returns how many Listeners' rows it kept.
	AdoptCorrectionAnnotations(jobID, part, correctionID string, merged bool) (int64, error)
}
