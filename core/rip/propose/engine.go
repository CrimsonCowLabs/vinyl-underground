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

// Propose works out the tags for one Track: the artist and title from StartsFrom, then the album
// and year from MusicBrainz (see Pick). Each reading of the title is looked up in turn (one
// MusicBrainz request each, usually one in all), and the first MusicBrainz knows wins. When
// MusicBrainz finds nothing, it is the first reading, and the album is YouTube Music's or the
// File's own, if any, else the Track's own title, as for a single. It always returns a ready
// proposal: a failed lookup only adds a Note, and sets LookupFailed.
func (e *Engine) Propose(ctx context.Context, in Input) model.RipProposal {
	starts := StartsFrom(in)
	s := starts[0]
	var choice *Choice
	var note string
	failed := false
	switch {
	case e.mb == nil:
		note = "MusicBrainz lookups are off"
	case s.Title == "" || s.AlbumArtist == "":
		note = "no artist and title to look up on MusicBrainz"
	default:
		for _, cand := range starts {
			c, err := e.lookup(ctx, cand.AlbumArtist, cand.Title)
			if err != nil {
				log.Warn(ctx, "Could not look up a Track on MusicBrainz", "artist", cand.Artist, "title", cand.Title, err)
				note, failed = "MusicBrainz couldn't be asked: "+err.Error(), true
				break
			}
			if c != nil {
				s, choice = cand, c
				break
			}
		}
	}
	p := model.RipProposal{
		Status: model.RipProposalReady, Title: s.Title, Artist: s.Artist, From: string(s.From),
		AlbumArtist: s.AlbumArtist, Note: note, LookupFailed: failed, UpdatedAt: time.Now(),
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

// lookup searches MusicBrainz for the song's recordings by artist (the lead artist, as credited)
// and picks their album (see Pick). Only recordings of the same title, credited to the main
// artist, count, both titles read without featuring credits or a remaster note. The search score
// decides nothing: an album recording credited with its featured artists ("Daft Punk feat.
// Pharrell Williams and Nile Rodgers") can score below covers and remixes.
func (e *Engine) lookup(ctx context.Context, artist, title string) (*Choice, error) {
	recs, err := e.mb.SearchRecordings(ctx, SearchQuery(artist, title))
	if err != nil {
		return nil, err
	}
	want := normKey(songTitle(title))
	var releases []musicbrainz.Release
	for _, r := range recs {
		if normKey(songTitle(r.Title)) == want && sameArtist(r.ArtistCredit.String(), artist) {
			releases = append(releases, r.Releases...)
		}
	}
	return Pick(releases, artist), nil
}

// SearchQuery is the MusicBrainz recording search for a song by an artist. It asks only for
// recordings on an official album, EP or single: a well-known song has more recordings than one
// search lists (most of them only on compilations), and the one on the studio album would
// otherwise be missed. Each recording comes with the releases that match. The title is searched
// for without its featuring credits or remaster note (see songTitle).
func SearchQuery(artist, title string) string {
	return `recording:"` + luceneEscape(songTitle(title)) + `" AND artist:"` + luceneEscape(artist) + `"` +
		` AND status:official AND (primarytype:album OR primarytype:ep OR primarytype:single)`
}

// remasterNote is a remaster note at the end of a title: "(Remastered)", "[2011 Remaster]",
// " - Remastered 2009", "(Remastered 2011 Version)".
var remasterNote = regexp.MustCompile(`(?i)\s*(?:[(\[][^()\[\]]*\bremaster(?:ed)?\b[^()\[\]]*[)\]]|\s[-–—]\s[^-–—]*\bremaster(?:ed)?\b[^-–—]*)$`)

// songTitle is the song's own title, as MusicBrainz names its recordings: without featuring
// credits (MusicBrainz puts them in the artist credit) or a remaster note (a remaster is the same
// recording, on the same album). "Live", "Remix" and the like are kept: they are other recordings.
func songTitle(title string) string {
	t := match.WithoutFeaturing(title)
	if s := strings.TrimSpace(remasterNote.ReplaceAllString(t, "")); s != "" {
		t = s
	}
	return t
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
