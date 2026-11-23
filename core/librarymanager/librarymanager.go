// Package librarymanager is the Library manager's bulk tag edit (#166): set or clear one field
// across many Tracks, picked one by one or as everything matching the listing's filters. The
// edit is previewed (old → new per Track), then runs as one edit job, with one tags step per
// File (editjob.TagsStep), so it is written, journaled and undone as a whole like any other job.
package librarymanager

import (
	"context"
	"errors"
	"fmt"
	"maps"

	. "github.com/Masterminds/squirrel"
	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
)

// MaxSelected bounds the Tracks a bulk edit names one by one; more are selected by filter
// ("all matching"), which the server resolves itself.
const MaxSelected = 1000

var (
	// ErrNoSelection is a bulk edit of neither Tracks nor a filter.
	ErrNoSelection = errors.New("select the Tracks to edit, or edit all matching the filter")
	// ErrTooMany is a selection of more than MaxSelected Tracks by ID.
	ErrTooMany = fmt.Errorf("at most %d Tracks can be selected one by one; edit all matching the filter instead", MaxSelected)
)

// Selection is the Tracks a bulk edit is made to: the Tracks IDs, or, when Filter is set (even
// empty), every Track matching it. Filter holds the Library manager listing's REST filters
// (folder, artists_id, album_id, format, rip, missing_tag, title for free text, library_id), and
// is resolved by the same repository, so "all matching" is exactly what the listing shows.
type Selection struct {
	IDs    []string       `json:"ids,omitempty"`
	Filter map[string]any `json:"filter,omitempty"`
}

// Manager previews and runs bulk edits.
type Manager struct {
	ds    model.DataStore
	queue *editjob.Queue
}

// New returns a Manager that queues its edits on queue.
func New(ds model.DataStore, queue *editjob.Queue) *Manager {
	return &Manager{ds: ds, queue: queue}
}

// Tracks returns the selected Tracks whose Files are present (a missing File can't be edited),
// in path order.
func (m *Manager) Tracks(ctx context.Context, sel Selection) (model.MediaFiles, error) {
	switch {
	case sel.Filter != nil:
		filters := maps.Clone(sel.Filter)
		filters["missing"] = "false"
		res, err := m.ds.Resource(ctx, model.MediaFile{}).ReadAll(rest.QueryOptions{Filters: filters, Sort: "path"})
		if err != nil {
			return nil, err
		}
		mfs, ok := res.(model.MediaFiles)
		if !ok {
			return nil, fmt.Errorf("unexpected Track listing %T", res)
		}
		return mfs, nil
	case len(sel.IDs) > MaxSelected:
		return nil, ErrTooMany
	case len(sel.IDs) > 0:
		return m.ds.MediaFile(ctx).GetAll(model.QueryOptions{
			Filters: And{Eq{"media_file.id": sel.IDs}, Eq{"media_file.missing": false}},
			Sort:    "path",
		})
	}
	return nil, ErrNoSelection
}

// ErrNothingToChange is a bulk edit that would write no File: each already holds the value, or
// can't be edited.
var ErrNothingToChange = errors.New("none of the selected Tracks would change")

// Run queues the bulk edit as one edit job, with one tags step (editjob.TagsStep) for each File
// the preview shows changing, in path order. Tracks that already hold the value, or that can't
// be edited, are left out. The job runs in the background (see editjob.Queue), is listed in the
// Edit history and can be undone as a whole, as any job.
func (m *Manager) Run(ctx context.Context, edit BulkEdit) (*model.EditJob, error) {
	rows, values, err := m.plan(ctx, edit)
	if err != nil {
		return nil, err
	}
	var steps []model.EditJobStep
	for _, r := range rows {
		if r.Status != StatusChange {
			continue
		}
		s, err := editjob.TagsStep(r.ID, tagedit.Values{edit.Field: values})
		if err != nil {
			return nil, err
		}
		steps = append(steps, s)
	}
	if len(steps) == 0 {
		return nil, ErrNothingToChange
	}
	return m.queue.Enqueue(ctx, model.EditTags, steps)
}

// Code is the code a bulk edit's error is shown with (see editjob.Code).
func Code(err error) string {
	switch {
	case errors.Is(err, ErrNoSelection):
		return "noSelection"
	case errors.Is(err, ErrTooMany):
		return "tooMany"
	case errors.Is(err, ErrNothingToChange):
		return "nothingToChange"
	}
	return editjob.Code(err)
}
