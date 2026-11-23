package editjob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/navidrome/navidrome/core/quarantine"
	"github.com/navidrome/navidrome/core/safefile"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
)

// Quarantine's steps (#168): a Track sent to Quarantine, a File restored from it, and Empty
// Quarantine. Undoing a move into Quarantine is an undo step, as for any write.

var (
	// ErrNothingInQuarantine is an Empty Quarantine with no Files.
	ErrNothingInQuarantine = errors.New("there is nothing in Quarantine")
	// ErrQuarantineChanged is an Empty Quarantine confirmed for other Files than are there now.
	ErrQuarantineChanged = errors.New("Quarantine changed since you confirmed; check it again")
)

// QuarantineStep is the step that moves the File of the Track trackID into Quarantine
// (tagedit.Editor.QuarantineInJob). Queue one per Track, as one job, to send many.
func QuarantineStep(trackID string) model.EditJobStep {
	return model.EditJobStep{Kind: model.EditQuarantine, MediaFileID: trackID}
}

type restoreArgs struct {
	LibraryID int    `json:"libraryId"`
	Path      string `json:"path"`
}

// RestoreStep is the step that moves the File at path in Quarantine, which no journaled move put
// there, back to its path beneath Quarantine (tagedit.Editor.RestoreInJob).
func RestoreStep(libID int, path string) model.EditJobStep {
	args, _ := json.Marshal(restoreArgs{LibraryID: libID, Path: path})
	return model.EditJobStep{Kind: model.EditRestore, Path: path, Args: string(args)}
}

// RestoreFileStep is the step that restores the File at path in Quarantine (in the library
// libID): the undo of the move that put it there, if the Edit journal has one, so the File goes
// back where it was; otherwise a RestoreStep.
func RestoreFileStep(ctx context.Context, ds model.DataStore, libID int, path string) (model.EditJobStep, error) {
	if !quarantine.Within(path) {
		return model.EditJobStep{}, tagedit.ErrNotInQuarantine
	}
	last, err := ds.EditJournal(ctx).LastWritten(libID, path)
	switch {
	case err == nil && last.Kind == model.EditQuarantine:
		return UndoStep(last.ID)
	case err != nil && !errors.Is(err, model.ErrNotFound):
		return model.EditJobStep{}, err
	}
	return RestoreStep(libID, path), nil
}

// emptyStepFile is the File an Empty Quarantine step stands for: no Track's, so a restart never
// takes one of the job's deletes for the step's outcome, and runs the step again (its deletes
// pass over the Files gone already).
const emptyStepFile = "quarantine"

type emptyArgs struct {
	Files []tagedit.QuarantinedFile `json:"files"`
}

// EmptyStep is the one step of Empty Quarantine (tagedit.Editor.EmptyInJob): it deletes the
// Files, as the admin confirmed them (path and size), for good.
func EmptyStep(files []tagedit.QuarantinedFile) (model.EditJobStep, error) {
	if len(files) == 0 {
		return model.EditJobStep{}, ErrNothingInQuarantine
	}
	args, err := json.Marshal(emptyArgs{Files: files})
	if err != nil {
		return model.EditJobStep{}, err
	}
	return model.EditJobStep{Kind: model.EditEmptyQuarantine, MediaFileID: emptyStepFile,
		Path: quarantine.Folder(), Args: string(args)}, nil
}

// ConfirmedEmptyStep is the EmptyStep for every File in Quarantine now (quarantine.Contents),
// provided they are as many, and as large, as the admin confirmed (count and size): otherwise
// ErrQuarantineChanged, so Empty Quarantine never deletes what the confirmation didn't state.
func ConfirmedEmptyStep(ctx context.Context, ds model.DataStore, count int, size int64) (model.EditJobStep, error) {
	c, err := quarantine.Contents(ctx, ds, safefile.OS{})
	if err != nil {
		return model.EditJobStep{}, err
	}
	if c.Count == 0 {
		return model.EditJobStep{}, ErrNothingInQuarantine
	}
	if c.Count != count || c.Size != size {
		return model.EditJobStep{}, fmt.Errorf("%w: %d Files (%d bytes), not %d (%d bytes)", ErrQuarantineChanged, c.Count, c.Size, count, size)
	}
	files := make([]tagedit.QuarantinedFile, len(c.Files))
	for i, f := range c.Files {
		files[i] = tagedit.QuarantinedFile{LibraryID: f.LibraryID, Path: f.Path, Size: f.Size}
	}
	return EmptyStep(files)
}

// quarantineKind writes quarantine steps.
type quarantineKind struct {
	editor *tagedit.Editor
}

func (k quarantineKind) Check(ctx context.Context, step *model.EditJobStep) error {
	mf, err := k.editor.CheckQuarantine(ctx, step.MediaFileID)
	if err != nil {
		return err
	}
	step.Path = mf.Path
	return nil
}

func (k quarantineKind) Run(ctx context.Context, jobID string, step *model.EditJobStep) (Outcome, error) {
	return outcome(k.editor.QuarantineInJob(ctx, jobID, step.MediaFileID))
}

func (k quarantineKind) Code(err error) string { return tagedit.Code(err) }

// restoreKind writes restore steps.
type restoreKind struct {
	editor *tagedit.Editor
}

func (k restoreKind) args(step *model.EditJobStep) (restoreArgs, error) {
	var a restoreArgs
	if err := json.Unmarshal([]byte(step.Args), &a); err != nil || a.Path == "" {
		return a, fmt.Errorf("%w: a restore step needs its File", tagedit.ErrNotInQuarantine)
	}
	return a, nil
}

func (k restoreKind) Check(_ context.Context, step *model.EditJobStep) error {
	a, err := k.args(step)
	if err != nil {
		return err
	}
	if !tagedit.Writable(a.LibraryID) {
		return tagedit.ErrEditingOff
	}
	if !quarantine.Within(a.Path) {
		return tagedit.ErrNotInQuarantine
	}
	step.Path = a.Path
	return nil
}

func (k restoreKind) Run(ctx context.Context, jobID string, step *model.EditJobStep) (Outcome, error) {
	a, err := k.args(step)
	if err != nil {
		return Outcome{}, err
	}
	return outcome(k.editor.RestoreInJob(ctx, jobID, a.LibraryID, a.Path))
}

func (k restoreKind) Code(err error) string { return tagedit.Code(err) }

// emptyKind writes the Empty Quarantine step.
type emptyKind struct {
	editor *tagedit.Editor
}

func (k emptyKind) args(step *model.EditJobStep) (emptyArgs, error) {
	var a emptyArgs
	if err := json.Unmarshal([]byte(step.Args), &a); err != nil || len(a.Files) == 0 {
		return a, ErrNothingInQuarantine
	}
	return a, nil
}

func (k emptyKind) Check(_ context.Context, step *model.EditJobStep) error {
	a, err := k.args(step)
	if err != nil {
		return err
	}
	for _, f := range a.Files {
		if !tagedit.Writable(f.LibraryID) {
			return tagedit.ErrEditingOff
		}
	}
	return nil
}

func (k emptyKind) Run(ctx context.Context, jobID string, step *model.EditJobStep) (Outcome, error) {
	a, err := k.args(step)
	if err != nil {
		return Outcome{}, err
	}
	return outcome(k.editor.EmptyInJob(ctx, jobID, a.Files))
}

func (k emptyKind) Code(err error) string { return Code(err) }

func outcome(res *tagedit.Result, err error) (Outcome, error) {
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{Changed: res.Changed, EntryID: res.Entry, Warning: res.Warning}, nil
}
