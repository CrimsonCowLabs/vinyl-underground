package editjob

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
)

// undoArgs are an undo step's Args: the Edit journal entry it undoes.
type undoArgs struct {
	EntryID string `json:"entryId"`
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
// journaled with that job's ID, so undoing it in turn redoes the job.
func UndoJobSteps(ctx context.Context, ds model.DataStore, jobID string) ([]model.EditJobStep, error) {
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

// Check refuses an entry that is gone or wasn't written, and fills in the step's File from the
// entry: where the write left it.
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
	step.MediaFileID, step.Path = entry.MediaFileID, entry.NewPath
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
