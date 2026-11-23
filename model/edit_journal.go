package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// EditKind is what a write to a File did: tag edits, cover writes and embeds (an undo has the
// kind of the write it undoes); moves (Organize) and Quarantine moves are recorded the same way
// later.
type EditKind string

const (
	EditTags EditKind = "tags"
	// EditCover is a write of an album's folder cover (its cover.jpg): Path and NewPath are the
	// cover File, OriginalImage the cover it replaced (none for a new one) and Image the one it
	// wrote (none for an undo that removed it). It has no Track.
	EditCover EditKind = "cover"
	// EditEmbed is an album cover embedded into one File (Embed in Files): the File's first
	// picture is replaced by Image (or added), and OriginalImage is the picture it had there.
	EditEmbed EditKind = "embed"
	// EditQuarantine is a move of a File into Quarantine, or out of it (a restore, or an undo)
	// (#168): Path is where the File was and NewPath where the move left it, both relative to the
	// library. MediaFileID is its Track, if it has one. It holds no tags and no image.
	EditQuarantine EditKind = "quarantine"
	// EditDelete is a File in Quarantine that Empty Quarantine deleted for good (#168): Path is
	// where it was, and NewPath is empty. It can't be undone.
	EditDelete EditKind = "delete"
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
	// UndoOf is the entry an undo undoes (#161). An undo has the kind of the entry it undoes, and
	// can be undone in turn (a redo). See undoers in core/tagedit for how
	// each kind undoes its entries.
	UndoOf string `structs:"undo_of" json:"undoOf,omitempty"`
	// OriginalImage and Image are, for a cover write or an embed, the image the write replaced and
	// the one it wrote; an empty Sum means none. Empty for a tag edit.
	OriginalImage EditImage `structs:"original_image,omitnested" json:"originalImage"`
	Image         EditImage `structs:"image,omitnested" json:"image"`
	Error         string    `structs:"error" json:"error,omitempty"`
	CreatedAt     time.Time `structs:"created_at" json:"createdAt"`
	UpdatedAt     time.Time `structs:"updated_at" json:"updatedAt"`
}

// EditImage is an image a cover write or an embed replaced or wrote. Its bytes are kept apart from
// the journal, by Sum (see tagedit's journal images), so an undo can put it back byte for byte.
type EditImage struct {
	// Sum is the image's SHA-256 (hex); empty means no image.
	Sum  string `json:"sum,omitempty"`
	Mime string `json:"mime,omitempty"`
	// Type and Description are an embedded picture's (e.g. "Front Cover").
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
}

func (i EditImage) Value() (driver.Value, error) {
	b, err := json.Marshal(i)
	return string(b), err
}

func (i *EditImage) Scan(src any) error {
	var b []byte
	switch v := src.(type) {
	case nil:
	case string:
		b = []byte(v)
	case []byte:
		b = v
	default:
		return fmt.Errorf("can't read an image from %T", src)
	}
	*i = EditImage{}
	if len(b) == 0 {
		return nil
	}
	return json.Unmarshal(b, i)
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
	// LastWritten returns the latest EditWritten entry that left a File at path (its NewPath) in
	// the library libraryID, or ErrNotFound.
	LastWritten(libraryID int, path string) (*EditEntry, error)
	// ForJob returns the entries of the edit job jobID, oldest first.
	ForJob(jobID string) ([]EditEntry, error)
	// After returns the entries recorded after e that wrote, or may have written (pending), to
	// the same File: the same Track, or the File at e's NewPath in its library. Oldest first.
	After(e *EditEntry) ([]EditEntry, error)
	// GetAll returns entries, newest first unless options sort them otherwise.
	GetAll(options ...QueryOptions) ([]EditEntry, error)
	CountAll(options ...QueryOptions) (int64, error)
}
