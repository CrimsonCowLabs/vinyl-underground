package editjob

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
)

// The case merge's steps (#169): a File or folder renamed or moved with all it holds
// (tagedit.Editor.RenameInJob), a folder its moves left empty removed, and a Duplicate sent to
// Quarantine once confirmed byte-identical. A File's move into its partner is a move step
// (MoveStep). Organize queues them (core/organize's casemerge plan).

// RenameStep is the step that renames (or moves) the File or folder r.Path to r.NewPath, with all
// it holds. The step's File is r.Path.
func RenameStep(r tagedit.Rename) (model.EditJobStep, error) {
	args, err := json.Marshal(r)
	if err != nil {
		return model.EditJobStep{}, err
	}
	return model.EditJobStep{Kind: model.EditRename, Path: r.Path, Args: string(args)}, nil
}

func readRename(step *model.EditJobStep) (tagedit.Rename, error) {
	var r tagedit.Rename
	if err := json.Unmarshal([]byte(step.Args), &r); err != nil || r.Path == "" || r.NewPath == "" {
		return r, fmt.Errorf("%w: a rename step needs its old and new path", tagedit.ErrInvalidTags)
	}
	return r, nil
}

type renameKind struct {
	editor *tagedit.Editor
}

func (k renameKind) Check(_ context.Context, step *model.EditJobStep) error {
	r, err := readRename(step)
	if err != nil {
		return err
	}
	step.Path = r.Path
	return k.editor.CheckRename(r)
}

func (k renameKind) Run(ctx context.Context, jobID string, step *model.EditJobStep) (Outcome, error) {
	r, err := readRename(step)
	if err != nil {
		return Outcome{}, err
	}
	return outcome(k.editor.RenameInJob(ctx, jobID, r))
}

func (k renameKind) Code(err error) string { return tagedit.Code(err) }

type removeDirArgs struct {
	LibraryID int    `json:"libraryId"`
	Path      string `json:"path"`
}

// RemoveDirStep is the step that removes the folder path of the library libID, which the steps
// before it left empty (tagedit.Editor.RemoveEmptyDir).
func RemoveDirStep(libID int, path string) model.EditJobStep {
	args, _ := json.Marshal(removeDirArgs{LibraryID: libID, Path: path})
	return model.EditJobStep{Kind: model.EditRemoveDir, Path: path, Args: string(args)}
}

type removeDirKind struct {
	editor *tagedit.Editor
}

func (k removeDirKind) args(step *model.EditJobStep) (removeDirArgs, error) {
	var a removeDirArgs
	if err := json.Unmarshal([]byte(step.Args), &a); err != nil || a.Path == "" {
		return a, fmt.Errorf("%w: a folder removal needs its folder", tagedit.ErrInvalidTags)
	}
	return a, nil
}

func (k removeDirKind) Check(_ context.Context, step *model.EditJobStep) error {
	a, err := k.args(step)
	if err != nil {
		return err
	}
	if !tagedit.Writable(a.LibraryID) {
		return tagedit.ErrEditingOff
	}
	step.Path = a.Path
	return nil
}

func (k removeDirKind) Run(ctx context.Context, _ string, step *model.EditJobStep) (Outcome, error) {
	a, err := k.args(step)
	if err != nil {
		return Outcome{}, err
	}
	return outcome(k.editor.RemoveEmptyDir(ctx, a.LibraryID, a.Path))
}

func (k removeDirKind) Code(err error) string { return tagedit.Code(err) }

// DuplicateStep is the step that sends the File d.Path to Quarantine as a Duplicate of d.Keep,
// once it is confirmed byte-identical (tagedit.Editor.QuarantineDuplicateInJob).
func DuplicateStep(d tagedit.Duplicate) (model.EditJobStep, error) {
	args, err := json.Marshal(d)
	if err != nil {
		return model.EditJobStep{}, err
	}
	return model.EditJobStep{Kind: model.EditQuarantineDuplicate, MediaFileID: d.TrackID, Path: d.Path, Args: string(args)}, nil
}

type duplicateKind struct {
	editor *tagedit.Editor
}

func (k duplicateKind) args(step *model.EditJobStep) (tagedit.Duplicate, error) {
	var d tagedit.Duplicate
	if err := json.Unmarshal([]byte(step.Args), &d); err != nil || d.Path == "" {
		return d, fmt.Errorf("%w: a Duplicate step needs its File", tagedit.ErrInvalidTags)
	}
	return d, nil
}

func (k duplicateKind) Check(ctx context.Context, step *model.EditJobStep) error {
	d, err := k.args(step)
	if err != nil {
		return err
	}
	step.MediaFileID, step.Path = d.TrackID, d.Path
	return k.editor.CheckDuplicate(ctx, d)
}

func (k duplicateKind) Run(ctx context.Context, jobID string, step *model.EditJobStep) (Outcome, error) {
	d, err := k.args(step)
	if err != nil {
		return Outcome{}, err
	}
	return outcome(k.editor.QuarantineDuplicateInJob(ctx, jobID, d))
}

func (k duplicateKind) Code(err error) string { return tagedit.Code(err) }
