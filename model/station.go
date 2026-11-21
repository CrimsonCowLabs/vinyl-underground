package model

import "time"

// StationTrack is a Track a Listener's Station served, kept for a while so the Station doesn't
// repeat itself.
type StationTrack struct {
	UserID      string    `structs:"user_id" json:"userId"`
	MediaFileID string    `structs:"media_file_id" json:"mediaFileId"`
	Artist      string    `structs:"artist" json:"artist"`
	ServedAt    time.Time `structs:"served_at" json:"servedAt"`
}

type StationTracks []StationTrack

// StationTrackRepository holds what the logged-in Listener's Station has served. Every Listener
// has their own Station, so each only sees and changes their own rows.
type StationTrackRepository interface {
	// Add records Tracks the Station just served.
	Add(tracks ...StationTrack) error
	// Since returns the Tracks served at or after since, newest first.
	Since(since time.Time) (StationTracks, error)
	// DeleteBefore forgets the Tracks served before t.
	DeleteBefore(t time.Time) error
}
