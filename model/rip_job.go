package model

import "time"

// RipStatus is where a Rip job is in its life.
type RipStatus string

const (
	RipQueued      RipStatus = "queued"
	RipDownloading RipStatus = "downloading"
	// RipReady means the Rip is in the Inbox as a playable Track, waiting for the admin to review it.
	RipReady  RipStatus = "ready"
	RipFailed RipStatus = "failed"
)

// RipJob is one request to Rip a web page's audio into the Inbox. Jobs are kept in the database
// so they survive an app restart.
type RipJob struct {
	ID     string    `structs:"id" json:"id"`
	UserID string    `structs:"user_id" json:"userId"`
	URL    string    `structs:"url" json:"url"`
	Status RipStatus `structs:"status" json:"status"`
	// Error is why the job failed, usually yt-dlp's own error message.
	Error string `structs:"error" json:"error"`
	// Title and Source come from the page once the job has looked at it.
	Title  string `structs:"title" json:"title"`
	Source string `structs:"source" json:"source"`
	// Path is where the File landed, relative to the Inbox.
	Path      string    `structs:"path" json:"path"`
	CreatedAt time.Time `structs:"created_at" json:"createdAt"`
	UpdatedAt time.Time `structs:"updated_at" json:"updatedAt"`
}

type RipJobs []RipJob

type RipJobRepository interface {
	// Put inserts the job (giving it an ID if it has none) or updates it.
	Put(job *RipJob) error
	Get(id string) (*RipJob, error)
	// GetAll returns every job, newest first.
	GetAll() (RipJobs, error)
	// NextQueued returns the oldest queued job, or ErrNotFound when none is waiting.
	NextQueued() (*RipJob, error)
	// RequeueInterrupted puts jobs that were downloading back in the queue, for when the app
	// stopped in the middle of a Rip. It returns how many there were.
	RequeueInterrupted() (int64, error)
}
