package editjob

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server/events"
	"github.com/navidrome/navidrome/utils/singleton"
)

// TagsStep is the step that writes the edited fields (by tagedit.Field key) into the File of the
// Track trackID.
func TagsStep(trackID string, values tagedit.Values) (model.EditJobStep, error) {
	args, err := json.Marshal(values)
	if err != nil {
		return model.EditJobStep{}, err
	}
	return model.EditJobStep{Kind: model.EditTags, MediaFileID: trackID, Args: string(args)}, nil
}

// tagsKind writes tags steps, with tagedit.Editor.
type tagsKind struct {
	editor *tagedit.Editor
}

func (k tagsKind) Check(ctx context.Context, step *model.EditJobStep) error {
	mf, err := k.editor.Check(ctx, step.MediaFileID)
	if err != nil {
		return err
	}
	step.Path = mf.Path
	return nil
}

func (k tagsKind) Run(ctx context.Context, jobID string, step *model.EditJobStep) (Outcome, error) {
	var values tagedit.Values
	if err := json.Unmarshal([]byte(step.Args), &values); err != nil {
		return Outcome{}, fmt.Errorf("%w: %v", tagedit.ErrInvalidTags, err)
	}
	res, err := k.editor.EditInJob(ctx, jobID, step.MediaFileID, values)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{Changed: res.Changed, EntryID: res.Entry, Warning: res.Warning}, nil
}

func (k tagsKind) Code(err error) string { return tagedit.Code(err) }

// Kinds are the step kinds the app runs.
func Kinds(editor *tagedit.Editor) map[model.EditKind]StepKind {
	return map[model.EditKind]StepKind{model.EditTags: tagsKind{editor: editor}}
}

// GetInstance returns the app's one Queue, so the API and the worker share it.
func GetInstance(ds model.DataStore, broker events.Broker, editor *tagedit.Editor) *Queue {
	return singleton.GetInstance(func() *Queue {
		return New(ds, broker, Kinds(editor))
	})
}
