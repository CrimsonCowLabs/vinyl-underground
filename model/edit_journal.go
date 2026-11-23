package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// EditKind is what a write to a File did. Tag edits are the first; moves (Organize), Quarantine
// moves, cover writes and undos are recorded the same way later.
type EditKind string

const (
	EditTags EditKind = "tags"
)

// EditState is how far a write got.
type EditState string

const (
	// EditPending is a write recorded before the File is touched. Left pending after a restart,
	// it is settled from what is on disk (see safefile.Settle).
	EditPending EditState = "pending"
	// EditWritten is a write whose new File is in place.
	EditWritten EditState = "written"
	// EditFailed is a write that didn't happen: the File is as it was.
	EditFailed EditState = "failed"
)

// EditEntry is one write the app made (or tried to make) to one File, in the Edit journal
// (ADR-0011). It is recorded before the File is written, with the File's original tags, and kept
// forever, so the write can be undone (#161). An undo is an entry too, pointing at the entry it
// undoes.
type EditEntry struct {
	ID string `structs:"id" json:"id"`
	// JobID groups the writes of one job (e.g. one Correction over many Files), so a whole job
	// can be undone. A single edit is a job of its own.
	JobID string    `structs:"job_id" json:"jobId"`
	Kind  EditKind  `structs:"kind" json:"kind"`
	State EditState `structs:"state" json:"state"`
	// UserID is the admin who made the write.
	UserID      string `structs:"user_id" json:"userId"`
	LibraryID   int    `structs:"library_id" json:"libraryId"`
	MediaFileID string `structs:"media_file_id" json:"mediaFileId"`
	// Path is where the File was, relative to its library; NewPath where it is after the write
	// (the same, for a tag edit).
	Path    string `structs:"path" json:"path"`
	NewPath string `structs:"new_path" json:"newPath"`
	// OriginalTags are all the File's tags before the write, by TagLib property name (TITLE,
	// TRACKNUMBER, ...). Changes are the tags written: their new values, where no values means
	// the tag was removed. Every other tag was left as it was.
	OriginalTags EditTagValues `structs:"original_tags" json:"originalTags"`
	Changes      EditTagValues `structs:"changes" json:"changes"`
	// CopySum is the SHA-256 (hex) of the checked copy that replaces the File, recorded before it
	// does, so an interrupted write can tell which of the two is in place.
	CopySum string `structs:"copy_sum" json:"-"`
	// UndoOf is the entry an undo undoes (#161).
	UndoOf    string    `structs:"undo_of" json:"undoOf,omitempty"`
	Error     string    `structs:"error" json:"error,omitempty"`
	CreatedAt time.Time `structs:"created_at" json:"createdAt"`
	UpdatedAt time.Time `structs:"updated_at" json:"updatedAt"`
}

// EditTagValues are tags by TagLib property name, stored as a JSON object.
type EditTagValues map[string][]string

func (t EditTagValues) Value() (driver.Value, error) {
	if t == nil {
		return "{}", nil
	}
	b, err := json.Marshal(t)
	return string(b), err
}

func (t *EditTagValues) Scan(src any) error {
	var b []byte
	switch v := src.(type) {
	case nil:
	case string:
		b = []byte(v)
	case []byte:
		b = v
	default:
		return fmt.Errorf("can't read tags from %T", src)
	}
	*t = EditTagValues{}
	if len(b) == 0 {
		return nil
	}
	return json.Unmarshal(b, t)
}

type EditJournalRepository interface {
	// Put records an entry, or updates it. A new entry gets an ID.
	Put(*EditEntry) error
	Get(id string) (*EditEntry, error)
	// Pending returns the entries still EditPending, oldest first.
	Pending() ([]EditEntry, error)
}
