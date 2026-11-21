package model

import "time"

// StationSkip is a Track a Listener skipped on one of their Stations, kept for a while so that
// Station plays the Track and its artist a little less.
type StationSkip struct {
	UserID      string    `structs:"user_id" json:"userId"`
	DJID        string    `structs:"dj_id" json:"djId"`
	MediaFileID string    `structs:"media_file_id" json:"mediaFileId"`
	Artist      string    `structs:"artist" json:"artist"`
	SkippedAt   time.Time `structs:"skipped_at" json:"skippedAt"`
}

type StationSkips []StationSkip

// StationSkipRepository holds the logged-in Listener's skips. A Station is a Listener's with one
// DJ, so skips are kept per DJ, and each Listener only sees and changes their own.
type StationSkipRepository interface {
	// Add records a skip.
	Add(skip StationSkip) error
	// Since returns the skips on the Station with the DJ at or after since, newest first.
	Since(djID string, since time.Time) (StationSkips, error)
	// DeleteBefore forgets the skips, on all the Listener's Stations, before t.
	DeleteBefore(t time.Time) error
}
