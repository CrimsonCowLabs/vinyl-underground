package musicbrainz

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
)

// VariousArtistsID is MusicBrainz's "Various Artists", the credit of most compilations.
const VariousArtistsID = "89ad4ac3-39f7-470e-963a-56509c546377"

// Recording is one recorded performance of a song.
type Recording struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Score is how well the recording matches a search, out of 100.
	Score int `json:"score"`
	// Length is in milliseconds, 0 when unknown.
	Length       int           `json:"length"`
	ArtistCredit ArtistCredits `json:"artist-credit"`
	Releases     []Release     `json:"releases"`
}

// Release is one issue of a release group (an edition of an album, single...).
type Release struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Score is how well the release matches a search, out of 100 (only from SearchReleases).
	Score int `json:"score"`
	// Status is "Official", "Promotion", "Bootleg", "Pseudo-Release"... or empty.
	Status string `json:"status"`
	// Date is "YYYY", "YYYY-MM" or "YYYY-MM-DD", or empty.
	Date         string        `json:"date"`
	ArtistCredit ArtistCredits `json:"artist-credit"`
	ReleaseGroup ReleaseGroup  `json:"release-group"`
	// Country is where it was issued ("GB", "XE" for Europe...), or empty. Media is filled in by
	// ReleaseGroupReleases and SearchReleases, CoverArtArchive only by ReleaseGroupReleases.
	Country         string  `json:"country"`
	Media           []Media `json:"media"`
	CoverArtArchive struct {
		// Front is set when the Cover Art Archive holds a front cover for the release.
		Front bool `json:"front"`
	} `json:"cover-art-archive"`
	// Genres are the genres MusicBrainz's users voted for this release (filled in by Release).
	Genres []Genre `json:"genres"`
}

// Genre is one of MusicBrainz's genres, with how many users voted for it.
type Genre struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// maxAlbumGenres is how many genres AlbumGenres keeps at most.
const maxAlbumGenres = 5

// AlbumGenres are the album's genres as written into its Files: its release group's (the votes
// for every edition of the album), else the release's own, keeping those with at least a third
// of the top genre's votes (one-vote noise goes when others are well backed), most votes first,
// at most five, in Title Case as the Library's genres are ("alternative rock" ->
// "Alternative Rock").
func (r Release) AlbumGenres() []string {
	genres := r.ReleaseGroup.Genres
	if len(genres) == 0 {
		genres = r.Genres
	}
	genres = slices.Clone(genres)
	slices.SortStableFunc(genres, func(a, b Genre) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Name, b.Name))
	})
	res := []string{}
	for _, g := range genres {
		if len(res) == maxAlbumGenres || g.Count*3 < genres[0].Count {
			break
		}
		if name := genreTitle(g.Name); name != "" && !slices.Contains(res, name) {
			res = append(res, name)
		}
	}
	return res
}

// genreAcronyms are written in capitals, genreSmallWords in lower case unless first.
var (
	genreAcronyms   = map[string]bool{"r&b": true, "uk": true, "us": true, "edm": true, "idm": true, "ebm": true, "dj": true}
	genreSmallWords = map[string]bool{"and": true, "of": true, "the": true, "n": true}
)

// genreTitle is a MusicBrainz genre name (lower case) in Title Case: each word, and each part of
// a hyphenated word, capitalised.
func genreTitle(name string) string {
	words := strings.Fields(name)
	for i, w := range words {
		switch {
		case genreAcronyms[w]:
			words[i] = strings.ToUpper(w)
		case i > 0 && genreSmallWords[w]:
		default:
			parts := strings.Split(w, "-")
			for j, p := range parts {
				if r := []rune(p); len(r) > 0 {
					parts[j] = strings.ToUpper(string(r[0])) + string(r[1:])
				}
			}
			words[i] = strings.Join(parts, "-")
		}
	}
	return strings.Join(words, " ")
}

// Media is one disc, tape, file set... of a release.
type Media struct {
	// Format is "CD", `12" Vinyl`, "Digital Media"... or empty.
	Format string `json:"format"`
	// Position is the medium's number in the release (the disc number), from 1.
	Position   int `json:"position"`
	TrackCount int `json:"track-count"`
	// Tracks are filled in by Release only.
	Tracks []Track `json:"tracks"`
}

// Track is one track of a medium: a recording at a position.
type Track struct {
	// ID is the release track's ID (MUSICBRAINZ_RELEASETRACKID).
	ID       string `json:"id"`
	Position int    `json:"position"`
	Title    string `json:"title"`
	// Length is in milliseconds, 0 when unknown.
	Length       int           `json:"length"`
	ArtistCredit ArtistCredits `json:"artist-credit"`
	Recording    Recording     `json:"recording"`
}

// TrackCount is the number of tracks on all the release's media.
func (r Release) TrackCount() int {
	n := 0
	for _, m := range r.Media {
		n += max(m.TrackCount, len(m.Tracks))
	}
	return n
}

// Formats is the release's media as printed: "2×CD", "CD + DVD", leaving out media of unknown format.
func (r Release) Formats() string {
	var parts []string
	var counts []int
	for _, m := range r.Media {
		f := m.Format
		if f == "" {
			continue
		}
		if n := len(parts); n > 0 && parts[n-1] == f {
			counts[n-1]++
			continue
		}
		parts, counts = append(parts, f), append(counts, 1)
	}
	for i, n := range counts {
		if n > 1 {
			parts[i] = strconv.Itoa(n) + "×" + parts[i]
		}
	}
	return strings.Join(parts, " + ")
}

// Detail tells the release apart from the other releases of its album: its country, formats
// and year, "GB, 2×CD, 1999", leaving out what is unknown.
func (r Release) Detail() string {
	var detail []string
	for _, s := range []string{r.Country, r.Formats(), r.Year()} {
		if s != "" {
			detail = append(detail, s)
		}
	}
	return strings.Join(detail, ", ")
}

// Year is the year of the release's date, "" when unknown.
func (r Release) Year() string {
	return r.Date[:min(len(r.Date), 4)]
}

// Description names the release among the others of its album: "Record (GB, 2×CD, 1999)".
func (r Release) Description() string {
	if detail := r.Detail(); detail != "" {
		return r.Title + " (" + detail + ")"
	}
	return r.Title
}

// ReleaseGroup is an album, EP or single, whatever its editions.
type ReleaseGroup struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// PrimaryType is "Album", "Single", "EP", "Broadcast" or "Other".
	PrimaryType string `json:"primary-type"`
	// SecondaryTypes are "Compilation", "Live", "Remix", "Soundtrack", "DJ-mix"...
	SecondaryTypes   []string `json:"secondary-types"`
	FirstReleaseDate string   `json:"first-release-date"`
	// Genres are the genres voted for the album as a whole (filled in by Release).
	Genres []Genre `json:"genres"`
}

// ArtistCredit is one artist of a credit, with the phrase joining it to the next.
type ArtistCredit struct {
	Name       string `json:"name"`
	JoinPhrase string `json:"joinphrase"`
	Artist     struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"artist"`
}

type ArtistCredits []ArtistCredit

// String is the credit as printed: "Band & Friend".
func (c ArtistCredits) String() string {
	var b strings.Builder
	for _, a := range c {
		b.WriteString(a.Name + a.JoinPhrase)
	}
	return strings.TrimSpace(b.String())
}

// IsVariousArtists reports whether the credit is MusicBrainz's "Various Artists".
func (c ArtistCredits) IsVariousArtists() bool {
	return len(c) > 0 && (c[0].Artist.ID == VariousArtistsID || strings.EqualFold(c[0].Name, VariousArtistsName))
}
