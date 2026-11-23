package editjob

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
)

// MoveStep is the step that moves a File within its library (tagedit.Editor.MoveInJob): the
// Track's File when m.TrackID is set, keeping the Track's ID, or a File that is no Track's (a
// cover.jpg moved with its album). The step's File is m.Path.
func MoveStep(m tagedit.Move) (model.EditJobStep, error) {
	args, err := json.Marshal(m)
	if err != nil {
		return model.EditJobStep{}, err
	}
	return model.EditJobStep{Kind: model.EditMove, MediaFileID: m.TrackID, Path: m.Path, Args: string(args)}, nil
}

// ReadMove is the move a move step makes.
func ReadMove(step *model.EditJobStep) (tagedit.Move, error) {
	var m tagedit.Move
	if err := json.Unmarshal([]byte(step.Args), &m); err != nil || m.Path == "" || m.NewPath == "" {
		return m, fmt.Errorf("%w: a move step needs its old and new path", tagedit.ErrInvalidTags)
	}
	return m, nil
}

// moveKind runs move steps, with tagedit.Editor.MoveInJob.
type moveKind struct {
	editor *tagedit.Editor
}

// Check refuses the job while the library is read-only, or a Track is gone. A File that changed
// since it was planned is skipped when its step runs, so the job's other Files still move.
func (k moveKind) Check(ctx context.Context, step *model.EditJobStep) error {
	m, err := ReadMove(step)
	if err != nil {
		return err
	}
	step.MediaFileID, step.Path = m.TrackID, m.Path
	return k.editor.CheckMove(ctx, m)
}

func (k moveKind) Run(ctx context.Context, jobID string, step *model.EditJobStep) (Outcome, error) {
	m, err := ReadMove(step)
	if err != nil {
		return Outcome{}, err
	}
	res, err := k.editor.MoveInJob(ctx, jobID, m)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{Changed: res.Changed, EntryID: res.Entry, Warning: res.Warning}, nil
}

func (k moveKind) Code(err error) string { return tagedit.Code(err) }
