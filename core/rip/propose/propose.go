// Package propose proposes tags for a Track in the Inbox (#34): the artist and title from YouTube
// Music's own metadata or a cleaned-up video title, then the album and year from MusicBrainz, by
// the rule in Pick. It takes plain fields, not yt-dlp's, so uploads and dropped Files (#35) can be
// given proposals too.
package propose

import "time"

// Input is what is known about a Track to propose its tags from. Every field is optional.
type Input struct {
	// Artist and Title are a video's uploader (or channel) and title, or a File's own tags.
	Artist string
	Title  string
	// Album and Year are a File's own tags, if any.
	Album string
	Year  int
	// Duration is the Track's length, if known.
	Duration time.Duration
	// YouTubeMusic is YouTube Music's own metadata, when the video is an auto-generated "Topic"
	// upload. Nil when there is none.
	YouTubeMusic *YouTubeMusic
}

// YouTubeMusic is the metadata YouTube Music gives its auto-generated uploads (yt-dlp's artist,
// track, album and release_year).
type YouTubeMusic struct {
	// Artist is the whole credit ("Daft Punk, Pharrell Williams"), and Artists its artists, the
	// main one first, when known.
	Artist  string
	Artists []string
	Track   string
	Album   string
	Year    int
}
