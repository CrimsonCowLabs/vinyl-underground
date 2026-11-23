package editjob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
)

// imageArgs are the Args of a cover or embed step: the album, and the image, kept by its checksum
// in the Edit journal's images (tagedit.Editor.KeepImage) when the step is made, so a job queued
// or interrupted writes the image it was given.
type imageArgs struct {
	AlbumID string `json:"albumId"`
	Sum     string `json:"sum"`
}

func readImageArgs(step *model.EditJobStep) (imageArgs, error) {
	var a imageArgs
	if err := json.Unmarshal([]byte(step.Args), &a); err != nil || a.Sum == "" {
		return a, fmt.Errorf("%w: a cover step needs its image", tagedit.ErrInvalidImage)
	}
	return a, nil
}

func imageStep(kind model.EditKind, albumID, sum, trackID string) (model.EditJobStep, error) {
	args, err := json.Marshal(imageArgs{AlbumID: albumID, Sum: sum})
	if err != nil {
		return model.EditJobStep{}, err
	}
	return model.EditJobStep{Kind: kind, MediaFileID: trackID, Args: string(args)}, nil
}

// CoverStep is the step that writes the image as the album's folder cover (cover.jpg, see
// tagedit.Editor.WriteCover). The image is checked, and made JPEG, now (tagedit.ErrInvalidImage).
// The step has no Track: the queue tells it by the cover's path, which Check fills in.
func CoverStep(editor *tagedit.Editor, albumID string, image []byte) (model.EditJobStep, error) {
	data, err := tagedit.CoverJPEG(image)
	if err != nil {
		return model.EditJobStep{}, err
	}
	sum, err := editor.KeepImage(data)
	if err != nil {
		return model.EditJobStep{}, err
	}
	return imageStep(model.EditCover, albumID, sum, "")
}

// EmbedSteps are the steps of Embed in Files for the album: one per Track, in path order, each
// embedding the album's folder cover (tagedit.ErrNoFolderCover when it has none) as its File's
// first picture.
func EmbedSteps(ctx context.Context, ds model.DataStore, editor *tagedit.Editor, albumID string) ([]model.EditJobStep, error) {
	image, err := editor.FolderCover(ctx, albumID)
	if err != nil {
		return nil, err
	}
	sum, err := editor.KeepImage(image)
	if err != nil {
		return nil, err
	}
	tracks, err := ds.MediaFile(ctx).GetAll(model.QueryOptions{Sort: "path",
		Filters: squirrel.Eq{"media_file.album_id": albumID, "media_file.missing": false}})
	if err != nil {
		return nil, err
	}
	if len(tracks) == 0 {
		return nil, model.ErrNotFound
	}
	steps := make([]model.EditJobStep, 0, len(tracks))
	for _, mf := range tracks {
		s, err := imageStep(model.EditEmbed, albumID, sum, mf.ID)
		if err != nil {
			return nil, err
		}
		s.Path = mf.Path
		steps = append(steps, s)
	}
	return steps, nil
}

// coverKind writes cover steps, with tagedit.Editor.WriteCoverInJob.
type coverKind struct {
	editor *tagedit.Editor
}

func (k coverKind) Check(ctx context.Context, step *model.EditJobStep) error {
	a, err := readImageArgs(step)
	if err != nil {
		return err
	}
	rel, err := k.editor.CheckCover(ctx, a.AlbumID)
	if err != nil {
		return err
	}
	step.Path = rel
	return nil
}

func (k coverKind) Run(ctx context.Context, jobID string, step *model.EditJobStep) (Outcome, error) {
	a, err := readImageArgs(step)
	if err != nil {
		return Outcome{}, err
	}
	image, err := k.editor.KeptImage(a.Sum)
	if err != nil {
		return Outcome{}, err
	}
	res, err := k.editor.WriteCoverInJob(ctx, jobID, a.AlbumID, image)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{Changed: res.Changed, EntryID: res.Entry, Warning: res.Warning}, nil
}

func (k coverKind) Code(err error) string { return tagedit.Code(err) }

// embedKind writes embed steps, with tagedit.Editor.EmbedCoverInJob.
type embedKind struct {
	editor *tagedit.Editor
}

// Check refuses the job while the Track's library is read-only, or a Track is gone. A File of a
// kind that can't be checked (FLAC, ...) is skipped when the step runs, so the album's other Files
// still get the cover.
func (k embedKind) Check(ctx context.Context, step *model.EditJobStep) error {
	if _, err := readImageArgs(step); err != nil {
		return err
	}
	mf, err := k.editor.Check(ctx, step.MediaFileID)
	if errors.Is(err, tagedit.ErrUnsupportedFormat) {
		return nil
	}
	if err != nil {
		return err
	}
	step.Path = mf.Path
	return nil
}

func (k embedKind) Run(ctx context.Context, jobID string, step *model.EditJobStep) (Outcome, error) {
	a, err := readImageArgs(step)
	if err != nil {
		return Outcome{}, err
	}
	image, err := k.editor.KeptImage(a.Sum)
	if err != nil {
		return Outcome{}, err
	}
	res, err := k.editor.EmbedCoverInJob(ctx, jobID, step.MediaFileID, image)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{Changed: res.Changed, EntryID: res.Entry, Warning: res.Warning}, nil
}

func (k embedKind) Code(err error) string { return tagedit.Code(err) }
