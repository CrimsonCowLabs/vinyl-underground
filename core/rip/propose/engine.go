package propose

import (
	"cmp"
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/navidrome/navidrome/core/musicbrainz"
	"github.com/navidrome/navidrome/core/rip/match"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// minScore is the lowest search score a recording may have to count.
const minScore = 80

// Where a proposal's album came from (model.RipProposal.AlbumFrom).
const (
	AlbumFromMusicBrainz  = "musicbrainz"
	AlbumFromYouTubeMusic = "youtube-music"
	AlbumFromTags         = "tags"
	AlbumFromTitle        = "title"
)

// MusicBrainz is the part of *musicbrainz.Client the Engine uses.
type MusicBrainz interface {
	SearchRecordings(ctx context.Context, query string) ([]musicbrainz.Recording, error)
}

// Engine proposes tags. With no MusicBrainz, it proposes from the starting point alone.
type Engine struct {
	mb MusicBrainz
}

// New returns an Engine asking mb for albums and years; nil turns MusicBrainz lookups off.
func New(mb MusicBrainz) *Engine {
	return &Engine{mb: mb}
}

// Propose works out the tags for one Track: the artist and title from StartFrom, then the album
// and year from MusicBrainz (see Pick). When MusicBrainz finds nothing, the album is YouTube
// Music's or the File's own, if any, else the Track's own title, as for a single. It always
// returns a ready proposal: a failed lookup only adds a Note. One proposal is one MusicBrainz
// request.
func (e *Engine) Propose(ctx context.Context, in Input) model.RipProposal {
	s := StartFrom(in)
	p := model.RipProposal{
		Status: model.RipProposalReady, Title: s.Title, Artist: s.Artist, From: string(s.From),
		AlbumArtist: s.AlbumArtist, UpdatedAt: time.Now(),
	}
	var choice *Choice
	switch artist := MainArtist(s.AlbumArtist); {
	case e.mb == nil:
		p.Note = "MusicBrainz lookups are off"
	case s.Title == "" || artist == "":
		p.Note = "no artist and title to look up on MusicBrainz"
	default:
		var err error
		choice, err = e.lookup(ctx, artist, s.Title)
		if err != nil {
			log.Warn(ctx, "Could not look up a Track on MusicBrainz", "artist", s.Artist, "title", s.Title, err)
			p.Note = "MusicBrainz couldn't be asked: " + err.Error()
		}
	}
	switch {
	case choice != nil:
		p.Album, p.Year, p.AlbumFrom = choice.Album, choice.Year, AlbumFromMusicBrainz
		p.ReleaseType, p.ReleaseGroupID = choice.Type, choice.ReleaseGroupID
		p.AlbumArtist = cmp.Or(choice.AlbumArtist, p.AlbumArtist)
	case s.Album != "":
		p.Album, p.AlbumFrom = s.Album, AlbumFromTags
		if s.From == FromYouTubeMusic {
			p.AlbumFrom = AlbumFromYouTubeMusic
		}
	default:
		p.Album, p.AlbumFrom = s.Title, AlbumFromTitle
	}
	if p.Year == "" && s.Year > 0 {
		p.Year = strconv.Itoa(s.Year)
	}
	return p
}

// lookup searches MusicBrainz for the song's recordings and picks their album (see Pick). Only
// recordings of the same title, scoring minScore or more, count.
func (e *Engine) lookup(ctx context.Context, artist, title string) (*Choice, error) {
	recs, err := e.mb.SearchRecordings(ctx, SearchQuery(artist, title))
	if err != nil {
		return nil, err
	}
	want := normKey(title)
	var releases []musicbrainz.Release
	for _, r := range recs {
		if r.Score >= minScore && normKey(r.Title) == want {
			releases = append(releases, r.Releases...)
		}
	}
	return Pick(releases, artist), nil
}

// SearchQuery is the MusicBrainz recording search for a song by an artist. It asks only for
// recordings on an official album, EP or single: a well-known song has more recordings than one
// search lists (most of them only on compilations), and the one on the studio album would
// otherwise be missed. Each recording comes with the releases that match.
func SearchQuery(artist, title string) string {
	return `recording:"` + luceneEscape(title) + `" AND artist:"` + luceneEscape(artist) + `"` +
		` AND status:official AND (primarytype:album OR primarytype:ep OR primarytype:single)`
}

// luceneEscape escapes a phrase for a quoted Lucene term.
func luceneEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}

// mainArtistSplit is where a credit's main artist ends: a featured artist, a "&", "x" or "×", or
// a comma.
var mainArtistSplit = regexp.MustCompile(`(?i)\s+(?:feat\.?|ft\.?|featuring|&|×|x|,)\s+|,\s*`)

// MainArtist is the first artist of a credit: "Daft Punk" of "Daft Punk, Pharrell Williams" or
// "Daft Punk feat. Pharrell Williams". It is only for comparing and searching: "Simon & Garfunkel"
// becomes "Simon", as the same is done to both sides.
func MainArtist(credit string) string {
	return strings.TrimSpace(mainArtistSplit.Split(strings.TrimSpace(credit), 2)[0])
}

// normKey compares names: case, accents, punctuation, spaces and featuring credits taken out.
func normKey(s string) string {
	return strings.ReplaceAll(match.Key(s), " ", "")
}

// sameArtist reports whether two credits have the same main artist.
func sameArtist(a, b string) bool {
	ka := normKey(MainArtist(a))
	return ka != "" && ka == normKey(MainArtist(b))
}

// boolRank sorts true first.
func boolRank(b bool) int {
	if b {
		return 0
	}
	return 1
}
