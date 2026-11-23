package editjob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
)

// ErrJobUnfinished is an undo of an edit job that is still queued or running: undoing the Files
// it has written so far would leave the rest edited.
var ErrJobUnfinished = errors.New("the job hasn't finished; undo it once it has")

// undoArgs are an undo step's Args: the Edit journal entry it undoes.
type undoArgs struct {
	EntryID string `json:"entryId"`
	// LibraryID is the entry's library, filled in when the step is checked: a File that is no
	// Track's is told apart from the same path in another library by it (see fileKey).
	LibraryID int `json:"libraryId,omitempty"`
}

// UndoStep is the step that undoes the written Edit journal entry entryID (tagedit.Editor.Undo).
// The step's File is filled in when it is queued.
func UndoStep(entryID string) (model.EditJobStep, error) {
	args, err := json.Marshal(undoArgs{EntryID: entryID})
	if err != nil {
		return model.EditJobStep{}, err
	}
	return model.EditJobStep{Kind: model.EditUndo, Args: string(args)}, nil
}

// UndoJobSteps are the steps that undo the whole edit job jobID: one per entry it wrote, newest
// first, so each File is put back in the reverse order it was written. Entries that weren't
// written are left out; a job with none written can't be undone (tagedit.ErrNotUndoable), and a
// job with no entries at all is model.ErrNotFound. Queued as a job of their own, the undo is
// journaled with that job's ID, so undoing it in turn redoes the job. A job still queued or
// running is refused (ErrJobUnfinished). A job with no edit_job row (a write made before the
// queue, a job of its own) is undone from its entries alone.
func UndoJobSteps(ctx context.Context, ds model.DataStore, jobID string) ([]model.EditJobStep, error) {
	job, err := ds.EditJob(ctx).Get(jobID)
	switch {
	case err == nil && job.State != model.EditJobDone:
		return nil, ErrJobUnfinished
	case err != nil && !errors.Is(err, model.ErrNotFound):
		return nil, err
	}
	entries, err := ds.EditJournal(ctx).ForJob(jobID)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, model.ErrNotFound
	}
	var steps []model.EditJobStep
	for _, e := range slices.Backward(entries) {
		if e.State != model.EditWritten {
			continue
		}
		s, err := UndoStep(e.ID)
		if err != nil {
			return nil, err
		}
		steps = append(steps, s)
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("%w: none of the job's writes were written", tagedit.ErrNotUndoable)
	}
	return steps, nil
}

// undoJobArgs are an undo job's Args: the job whose Files it undoes, whose JobKind (see
// Queue.Handle) undoes the rest once they are.
type undoJobArgs struct {
	Undoes string `json:"undoes,omitempty"`
}

// UndoJobArgs are the Args of a job of UndoJobSteps(jobID) (see Queue.EnqueueWith).
func UndoJobArgs(jobID string) string {
	b, _ := json.Marshal(undoJobArgs{Undoes: jobID})
	return string(b)
}

// undoJobOf returns the job an undo job undoes, or "" for an undo of single Files.
func undoJobOf(job *model.EditJob) string {
	var a undoJobArgs
	if job.Args == "" || json.Unmarshal([]byte(job.Args), &a) != nil {
		return ""
	}
	return a.Undoes
}

// undoKind runs undo steps, with tagedit.Editor.UndoInJob. A refused undo (a later write changed
// the same fields, it was undone already, ...) skips the File with the refusal's code.
type undoKind struct {
	ds     model.DataStore
	editor *tagedit.Editor
}

func (k undoKind) args(step *model.EditJobStep) (undoArgs, error) {
	var a undoArgs
	if err := json.Unmarshal([]byte(step.Args), &a); err != nil || a.EntryID == "" {
		return a, fmt.Errorf("%w: an undo step needs the entry it undoes", tagedit.ErrNotUndoable)
	}
	return a, nil
}

// Check refuses an entry that is gone or wasn't written, or whose library is read-only now, and
// fills in the step's File from the entry: where the write left it. Other refusals (the Track is
// gone, a later write changed the File, ...) skip the File when the step runs, so a whole job's
// undo still puts back the Files it can.
func (k undoKind) Check(ctx context.Context, step *model.EditJobStep) error {
	a, err := k.args(step)
	if err != nil {
		return err
	}
	entry, err := k.ds.EditJournal(ctx).Get(a.EntryID)
	if err != nil {
		return err
	}
	if entry.State != model.EditWritten {
		return fmt.Errorf("%w: it was never written (%s)", tagedit.ErrNotUndoable, entry.State)
	}
	// Empty Quarantine's deletes (#168) are for good.
	if entry.Kind == model.EditDelete {
		return fmt.Errorf("%w: a File deleted for good can't be brought back", tagedit.ErrNotUndoable)
	}
	// The entry's library is checked directly: a folder cover (#164), or a File moved with its
	// album (#167), is no Track's, and a moved Track may be gone (its File goes back alone). A
	// Promote (#171) moved its File out of the Inbox, which is no writable library but which the
	// app writes: both its libraries are checked, and its Track may be in either.
	if !k.editor.EntryWritable(ctx, entry) {
		return tagedit.ErrEditingOff
	}
	if !tagedit.MovesLibraries(entry) {
		if _, err := k.editor.Check(ctx, entry.MediaFileID); errors.Is(err, tagedit.ErrEditingOff) {
			return err
		}
	}
	step.MediaFileID, step.Path = entry.MediaFileID, entry.NewPath
	a.LibraryID = entry.DestLibraryID() // the library of step.Path
	b, err := json.Marshal(a)
	if err != nil {
		return err
	}
	step.Args = string(b)
	return nil
}

func (k undoKind) Run(ctx context.Context, jobID string, step *model.EditJobStep) (Outcome, error) {
	a, err := k.args(step)
	if err != nil {
		return Outcome{}, err
	}
	res, err := k.editor.UndoInJob(ctx, jobID, a.EntryID)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{Changed: res.Changed, EntryID: res.Entry, Warning: res.Warning}, nil
}

func (k undoKind) Code(err error) string { return tagedit.Code(err) }
