package musicbrainz

import "strings"

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
	// Status is "Official", "Promotion", "Bootleg", "Pseudo-Release"... or empty.
	Status string `json:"status"`
	// Date is "YYYY", "YYYY-MM" or "YYYY-MM-DD", or empty.
	Date         string        `json:"date"`
	ArtistCredit ArtistCredits `json:"artist-credit"`
	ReleaseGroup ReleaseGroup  `json:"release-group"`
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
