package propose

import (
	"regexp"
	"slices"
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
	// FromTags is a File's own artist and title tags (an upload or a dropped File), cleaned up.
	FromTags From = "tags"
)

// Start is where a proposal starts, before MusicBrainz is asked for the album and year.
type Start struct {
	Artist string
	Title  string
	// AlbumArtist is the lead artist of the credit (YouTube Music's first artist, else the credit
	// without its featured artists): the album artist when MusicBrainz finds nothing, and the
	// artist MusicBrainz is searched for.
	AlbumArtist string
	// Album and Year are YouTube Music's, or the input's own, if any: the fallback when MusicBrainz
	// finds nothing.
	Album string
	Year  int
	From  From
}

// StartFrom picks the starting point: the first of StartsFrom.
func StartFrom(in Input) Start {
	return StartsFrom(in)[0]
}

// StartsFrom lists the readings of the input, likeliest first: YouTube Music's metadata when the
// upload is a "Topic" one (it has an artist and a track), else the input's artist and title with
// the YouTube noise ("(Official Video)", "[HD]", "Lyrics", a channel's " - Topic" or "VEVO") taken
// out. A title with a dash is read as "Artist - Title" first and, unless the left part is the
// uploader, whole with the uploader as the artist second ("Creep - Acoustic" by Radiohead), for
// MusicBrainz to tell apart. There is always at least one.
func StartsFrom(in Input) []Start {
	if ytm := in.YouTubeMusic; ytm != nil && strings.TrimSpace(ytm.Artist) != "" && strings.TrimSpace(ytm.Track) != "" {
		s := Start{Artist: strings.TrimSpace(ytm.Artist), Title: strings.TrimSpace(ytm.Track),
			Album: strings.TrimSpace(ytm.Album), Year: ytm.Year, From: FromYouTubeMusic}
		if s.Album == "" {
			s.Album = strings.TrimSpace(in.Album)
		}
		if s.Year == 0 {
			s.Year = in.Year
		}
		s.AlbumArtist = leadArtist(s.Artist)
		if len(ytm.Artists) > 0 && strings.TrimSpace(ytm.Artists[0]) != "" {
			s.AlbumArtist = strings.TrimSpace(ytm.Artists[0])
		}
		return []Start{s}
	}
	base := Start{Artist: strings.TrimSpace(in.Artist), Title: strings.TrimSpace(in.Title),
		Album: strings.TrimSpace(in.Album), Year: in.Year, From: FromTitle}
	var res []Start
	guesses := match.Guesses(in.Artist, in.Title)
	if in.Tagged && strings.TrimSpace(in.Artist) != "" {
		// The File's artist tag is its artist: the whole title is the likelier reading. A title
		// that starts with that artist ("Adele - Hello") has only the split one.
		base.From = FromTags
		slices.Reverse(guesses)
	}
	for _, g := range guesses {
		s := base
		s.Artist, s.Title = g.Artist, g.Title
		s.AlbumArtist = leadArtist(s.Artist)
		res = append(res, s)
	}
	if len(res) == 0 {
		base.AlbumArtist = leadArtist(base.Artist)
		res = append(res, base)
	}
	return res
}

// leadSplit is where a credit's lead artist ends: a featured artist, or a comma (yt-dlp joins an
// upload's artists with ", "). "&" and "and" are kept: "Simon & Garfunkel" is one act.
var leadSplit = regexp.MustCompile(`(?i)\s+(?:feat\.?|ft\.?|featuring)\s+|,\s+`)

func leadArtist(credit string) string {
	return strings.TrimSpace(leadSplit.Split(strings.TrimSpace(credit), 2)[0])
}
