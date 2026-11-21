package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// RipStatus is where a Rip job, or one of its items, is in its life.
type RipStatus string

const (
	RipQueued      RipStatus = "queued"
	RipDownloading RipStatus = "downloading"
	// RipReady means the File is in the Inbox. For a job it means the job has finished, at least one
	// of its items is in the Inbox as a playable Track, and it waits for the admin to review it. A
	// ready job can still have failed items ("ready with failures"): its Error then says how many.
	RipReady RipStatus = "ready"
	// RipFailed is an item that could not be ripped, or a job none of whose items could be.
	RipFailed RipStatus = "failed"
)

// RipJob is one request to Rip a web page's audio into the Inbox: one video, or a whole playlist or
// album. A job has one RipItem per video (one for a single video), and items are ripped one at a
// time, in order. Jobs and items are kept in the database so they survive an app restart.
type RipJob struct {
	ID     string `structs:"id" json:"id"`
	UserID string `structs:"user_id" json:"userId"`
	// URL is the page the admin pasted.
	URL    string    `structs:"url" json:"url"`
	Status RipStatus `structs:"status" json:"status"`
	// Error is why the job failed (usually yt-dlp's own message), or for a ready job, a note such as
	// how many of its items failed.
	Error string `structs:"error" json:"error"`
	// Title is the video's title, or the playlist's or album's. Source is the site's name.
	Title  string `structs:"title" json:"title"`
	Source string `structs:"source" json:"source"`
	// Folder is the Inbox subfolder a playlist's or album's Files go into, relative to the Inbox.
	// It is empty for a single video, whose File goes straight into the Inbox.
	Folder    string    `structs:"folder" json:"folder"`
	CreatedAt time.Time `structs:"created_at" json:"createdAt"`
	UpdatedAt time.Time `structs:"updated_at" json:"updatedAt"`

	// Items are the job's items in order. They are stored in rip_item, and only filled in by
	// callers that ask for them.
	Items RipItems `structs:"-" json:"items"`
}

type RipJobs []RipJob

// RipItem is one video of a Rip job, which becomes one File (and so one Track) in the Inbox.
type RipItem struct {
	ID    string `structs:"id" json:"id"`
	JobID string `structs:"job_id" json:"jobId"`
	// Position is the item's place in the playlist, from 1.
	Position int `structs:"position" json:"position"`
	// URL is the item's own page, ripped on its own (--no-playlist).
	URL string `structs:"url" json:"url"`
	// Title and Artist come from yt-dlp: a first guess from the playlist's listing, replaced by the
	// item's own metadata once it is ripped.
	Title  string    `structs:"title" json:"title"`
	Artist string    `structs:"artist" json:"artist"`
	Status RipStatus `structs:"status" json:"status"`
	// Error is why the item failed, usually yt-dlp's own message.
	Error string `structs:"error" json:"error"`
	// Path is where the File landed, relative to the Inbox (inside the job's Folder, if any). It is
	// set as soon as the File is there, and is how a restart knows not to rip the item again.
	Path string `structs:"path" json:"path"`
	// AlreadyHave are the Tracks the Library or the Inbox already seemed to have for this item
	// when it was queued ("already have: …"). They are only a warning: the item is ripped anyway.
	AlreadyHave RipMatches `structs:"already_have,omitnested" json:"alreadyHave"`
	// PromoteDest is where a Promote is moving the File (an absolute path), while it does. A
	// restart finishes or undoes every move it finds here (#36).
	PromoteDest string    `structs:"promote_dest" json:"-"`
	CreatedAt   time.Time `structs:"created_at" json:"createdAt"`
	UpdatedAt   time.Time  `structs:"updated_at" json:"updatedAt"`
}

type RipItems []RipItem

// RipMatch is a Track that seems to be the same recording as a video about to be ripped: a
// likely match on artist and title, not a Duplicate (that is about identical Files). It is a
// snapshot of the Track when the Rip was queued.
type RipMatch struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Artist  string `json:"artist"`
	Album   string `json:"album"`
	Library string `json:"libraryName"`
	// Source and SourceURL are the Track's, so the UI can show the "Ripped" badge.
	Source    string `json:"source,omitempty"`
	SourceURL string `json:"sourceUrl,omitempty"`
}

// RipMatches are stored as a JSON array.
type RipMatches []RipMatch

// MarshalJSON writes no matches as [], not null.
func (m RipMatches) MarshalJSON() ([]byte, error) {
	if m == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]RipMatch(m))
}

func (m RipMatches) Value() (driver.Value, error) {
	if len(m) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(m)
	return string(b), err
}

func (m *RipMatches) Scan(src any) error {
	var b []byte
	switch v := src.(type) {
	case nil:
	case string:
		b = []byte(v)
	case []byte:
		b = v
	default:
		return fmt.Errorf("can't read Rip matches from %T", src)
	}
	*m = RipMatches{}
	if len(b) == 0 {
		return nil
	}
	return json.Unmarshal(b, m)
}

type RipJobRepository interface {
	// Put inserts the job (giving it an ID if it has none) or updates it. It doesn't save Items.
	Put(job *RipJob) error
	Get(id string) (*RipJob, error)
	// GetAll returns every job, newest first, without Items.
	GetAll() (RipJobs, error)
	// NextQueued returns the oldest queued job, or ErrNotFound when none is waiting.
	NextQueued() (*RipJob, error)
	// RequeueInterrupted puts jobs that were downloading back in the queue, for when the app
	// stopped in the middle of a Rip. It returns how many there were.
	RequeueInterrupted() (int64, error)
	// Delete deletes the job and its items (a Discard, #33), or returns ErrNotFound.
	Delete(id string) error
}

type RipItemRepository interface {
	// Put inserts the item (giving it an ID if it has none) or updates it.
	Put(item *RipItem) error
	// ForJobs returns the items of the given jobs, by job and then position.
	ForJobs(jobIDs ...string) (RipItems, error)
	// RequeueInterrupted puts items that were downloading back in the queue. It returns how many
	// there were.
	RequeueInterrupted() (int64, error)
	// Delete deletes one item (a single Track's Discard, #33), or returns ErrNotFound.
	Delete(id string) error
	// PromotesInterrupted returns the items a Promote was moving (PromoteDest set) when the app
	// stopped.
	PromotesInterrupted() (RipItems, error)
	// MoveMatches updates every item's AlreadyHave snapshots of the Track trackID to say it is
	// now in the library libraryName (a promoted Track, #36).
	MoveMatches(trackID, libraryName string) error
}
