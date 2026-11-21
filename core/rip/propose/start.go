package propose

import (
	"strings"

	"github.com/navidrome/navidrome/core/rip/match"
)

// From says where a proposal's artist and title came from.
type From string

const (
	// FromYouTubeMusic is YouTube Music's own metadata, which it gives auto-generated "Topic"
	// uploads.
	FromYouTubeMusic From = "youtube-music"
	// FromTitle is the video's title (or a File's own tags), cleaned up.
	FromTitle From = "title"
)

// Start is where a proposal starts, before MusicBrainz is asked for the album and year.
type Start struct {
	Artist string
	Title  string
	// AlbumArtist is the main artist alone, for the album artist when MusicBrainz finds nothing.
	AlbumArtist string
	// Album and Year are YouTube Music's, or the input's own, if any: the fallback when MusicBrainz
	// finds nothing.
	Album string
	Year  int
	From  From
}

// StartFrom picks the starting point: YouTube Music's metadata when the upload is a "Topic" one
// (it has an artist and a track), else the input's artist and title with the YouTube noise
// ("(Official Video)", "[HD]", "Lyrics", a channel's " - Topic" or "VEVO") taken out and an
// "Artist - Title" title split.
func StartFrom(in Input) Start {
	if ytm := in.YouTubeMusic; ytm != nil && strings.TrimSpace(ytm.Artist) != "" && strings.TrimSpace(ytm.Track) != "" {
		s := Start{Artist: strings.TrimSpace(ytm.Artist), Title: strings.TrimSpace(ytm.Track),
			Album: strings.TrimSpace(ytm.Album), Year: ytm.Year, From: FromYouTubeMusic}
		if s.Album == "" {
			s.Album = strings.TrimSpace(in.Album)
		}
		if s.Year == 0 {
			s.Year = in.Year
		}
		s.AlbumArtist = s.Artist
		if len(ytm.Artists) > 0 && strings.TrimSpace(ytm.Artists[0]) != "" {
			s.AlbumArtist = strings.TrimSpace(ytm.Artists[0])
		}
		return s
	}
	s := Start{Artist: strings.TrimSpace(in.Artist), Title: strings.TrimSpace(in.Title),
		Album: strings.TrimSpace(in.Album), Year: in.Year, From: FromTitle}
	if gs := match.Guesses(in.Artist, in.Title); len(gs) > 0 {
		s.Artist, s.Title = gs[0].Artist, gs[0].Title
	}
	s.AlbumArtist = s.Artist
	return s
}
