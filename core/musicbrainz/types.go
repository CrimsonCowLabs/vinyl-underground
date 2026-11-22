package musicbrainz

import (
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
}

// Media is one disc, tape, file set... of a release.
type Media struct {
	// Format is "CD", `12" Vinyl`, "Digital Media"... or empty.
	Format string `json:"format"`
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
	for _, s := range []string{r.Country, r.Formats(), r.Date[:min(len(r.Date), 4)]} {
		if s != "" {
			detail = append(detail, s)
		}
	}
	return strings.Join(detail, ", ")
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
	return len(c) > 0 && (c[0].Artist.ID == VariousArtistsID || strings.EqualFold(c[0].Name, "Various Artists"))
}
