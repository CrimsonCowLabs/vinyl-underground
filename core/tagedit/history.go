package tagedit

import (
	"context"
	"errors"
	"maps"
	"slices"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/model"
)

// HistoryEntry is one Edit journal entry as the Edit history page shows it: when, which File and
// Track, and each field's old and new value.
type HistoryEntry struct {
	ID    string `json:"id"`
	JobID string `json:"jobId"`
	// JobSize is how many entries the entry's job has (#162): the history groups them, and a job
	// of several can be undone as a whole.
	JobSize   int             `json:"jobSize"`
	Kind      model.EditKind  `json:"kind"`
	State     model.EditState `json:"state"`
	UserID    string          `json:"userId,omitempty"`
	LibraryID int             `json:"libraryId"`
	Path      string          `json:"path"`
	NewPath   string          `json:"newPath"`
	// Track is the File's Track as the app has it now; nil when it is gone.
	Track   *HistoryTrack `json:"track,omitempty"`
	Changes []TagChange   `json:"changes"`
	// UndoOf is the entry this one undid, for an undo. Redo is an undo of an undo: it wrote the
	// undone values again.
	UndoOf    string    `json:"undoOf,omitempty"`
	Redo      bool      `json:"redo,omitempty"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// HistoryTrack is the Track an entry wrote to.
type HistoryTrack struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Artist  string `json:"artist"`
	Album   string `json:"album"`
	Missing bool   `json:"missing,omitempty"`
}

// TagChange is one tag an entry wrote (by TagLib property name), with its values before and
// after. No values means the tag wasn't there, or was removed.
type TagChange struct {
	Tag string   `json:"tag"`
	Old []string `json:"old"`
	New []string `json:"new"`
}

// History returns a page of the Edit journal, newest first, and how many entries it holds. It
// reads the journal only, so it works with editing off too.
func (e *Editor) History(ctx context.Context, offset, limit int) ([]HistoryEntry, int64, error) {
	journal := e.ds.EditJournal(ctx)
	total, err := journal.CountAll()
	if err != nil {
		return nil, 0, err
	}
	entries, err := journal.GetAll(model.QueryOptions{Offset: offset, Max: limit})
	if err != nil {
		return nil, 0, err
	}
	tracks := map[string]*HistoryTrack{}
	jobSizes := map[string]int{}
	// undos tells, by entry ID, whether an entry an undo undid was an undo itself.
	undos := map[string]bool{}
	res := make([]HistoryEntry, 0, len(entries))
	for _, en := range entries {
		h := HistoryEntry{
			ID: en.ID, JobID: en.JobID, Kind: en.Kind, State: en.State, UserID: en.UserID,
			LibraryID: en.LibraryID, Path: en.Path, NewPath: en.NewPath, UndoOf: en.UndoOf,
			Error: en.Error, CreatedAt: en.CreatedAt, Changes: []TagChange{},
		}
		for _, k := range slices.Sorted(maps.Keys(en.Changes)) {
			h.Changes = append(h.Changes, TagChange{Tag: k, Old: nonNil(en.OriginalTags[k]), New: nonNil(en.Changes[k])})
		}
		if en.JobID != "" {
			size, ok := jobSizes[en.JobID]
			if !ok {
				n, err := journal.CountAll(model.QueryOptions{Filters: squirrel.Eq{"job_id": en.JobID}})
				if err != nil {
					return nil, 0, err
				}
				size = int(n)
				jobSizes[en.JobID] = size
			}
			h.JobSize = size
		}
		if en.MediaFileID != "" {
			t, ok := tracks[en.MediaFileID]
			if !ok {
				mf, err := e.ds.MediaFile(ctx).Get(en.MediaFileID)
				switch {
				case err == nil:
					t = &HistoryTrack{ID: mf.ID, Title: mf.Title, Artist: mf.Artist, Album: mf.Album, Missing: mf.Missing}
				case !errors.Is(err, model.ErrNotFound):
					return nil, 0, err
				}
				tracks[en.MediaFileID] = t
			}
			h.Track = t
		}
		if en.UndoOf != "" {
			undone, ok := undos[en.UndoOf]
			if !ok {
				u, err := journal.Get(en.UndoOf)
				if err != nil && !errors.Is(err, model.ErrNotFound) {
					return nil, 0, err
				}
				undone = u != nil && u.UndoOf != ""
				undos[en.UndoOf] = undone
			}
			h.Redo = undone
		}
		res = append(res, h)
	}
	return res, total, nil
}

func nonNil(vs []string) []string {
	if vs == nil {
		return []string{}
	}
	return vs
}
