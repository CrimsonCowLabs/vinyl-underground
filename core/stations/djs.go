package stations

import (
	"math"
	"strings"
	"time"

	"github.com/navidrome/navidrome/model"
)

// DJ is the persona that gives a Station its character. It is either built-in or one a
// Listener created from a name and a personality description (#17). Its Taste decides which
// Tracks suit it without an LLM; with one, its name and description do.
type DJ struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// BuiltIn DJs come with the app and can't be edited or deleted.
	BuiltIn bool `json:"builtIn"`
	// Genres are a Listener's DJ's genres, by their names in the Library's genre tags, and
	// GenresEdited says the Listener picked them by hand. Built-in DJs have none here: their
	// genres are words in their Taste.
	Genres       []string `json:"genres,omitempty"`
	GenresEdited bool     `json:"genresEdited,omitempty"`
	Taste        Taste    `json:"-"`
}

// Taste weighs a Track's existing metadata for a DJ. Every Track starts at a score of 1 and
// each weight adds to it as far as the Track has that quality, so a Track with none of them
// can still play. A Track the Listener rated 1 or 2 keeps only a quarter of its score.
type Taste struct {
	// Genres are words that suit the DJ, matched within a Track's genres ("jazz" matches
	// "Cool Jazz"). When the Library has enough Tracks in them, rule-based selection plays only
	// those, leaving out Tracks with no genre (see strictGenreTracks).
	Genres      []string
	GenreWeight float64
	// WholeGenres matches Genres as whole genre names instead ("jazz" is only "Jazz"), for a
	// Listener's DJ, whose genres are picked from the Library's.
	WholeGenres bool
	// MinBPM and MaxBPM are the tempo the DJ likes. Only Tracks tagged with a BPM are weighed.
	MinBPM, MaxBPM int
	TempoWeight    float64
	// FavoriteWeight is for Tracks the Listener starred (full weight) or rated 4 (half) or 5.
	FavoriteWeight float64
	// HistoryWeight is for Tracks the Listener plays often, the most played getting it all.
	HistoryWeight float64
	// ArtistWeight is for Tracks by the artists the Listener stars, rates and plays the most.
	ArtistWeight float64
	// UnplayedWeight is for Tracks the Listener has never played.
	UnplayedWeight float64
	// OlderThan and OlderWeight are for Tracks released before that year.
	OlderThan   int
	OlderWeight float64
}

// Served is a Track a Station has already handed out.
type Served struct {
	TrackID string
	Artist  string
	At      time.Time
}

var builtIn = []DJ{
	{
		ID:          "night-owl",
		BuiltIn:     true,
		Name:        "Night Owl",
		Description: "Low-key and late-night: slow, mellow Tracks you love for the small hours.",
		Taste: Taste{
			Genres: []string{"jazz", "ambient", "soul", "blues", "downtempo", "trip", "lo-fi", "lofi",
				"chill", "folk", "acoustic", "classical", "r&b", "rnb", "dream", "slowcore", "quiet",
				"lounge", "singer-songwriter", "piano", "bossa"},
			GenreWeight:    4,
			MinBPM:         0,
			MaxBPM:         105,
			TempoWeight:    2,
			FavoriteWeight: 2,
			HistoryWeight:  1.5,
			ArtistWeight:   1,
		},
	},
	{
		ID:          "crate-digger",
		BuiltIn:     true,
		Name:        "The Crate Digger",
		Description: "Deep cuts: Tracks you rarely play, older records, and the B-sides of artists you love.",
		Taste: Taste{
			FavoriteWeight: 1.5,
			ArtistWeight:   2.5,
			UnplayedWeight: 3,
			OlderThan:      1990,
			OlderWeight:    1.5,
		},
	},
	{
		ID:          "sunny",
		BuiltIn:     true,
		Name:        "Sunny",
		Description: "Upbeat and bright, morning-show style: feel-good Tracks and your favorites.",
		Taste: Taste{
			Genres: []string{"pop", "dance", "funk", "disco", "soul", "reggae", "ska", "house",
				"motown", "latin", "afro", "electro", "synth", "rock", "indie", "swing", "salsa",
				"samba", "calypso", "power pop", "new wave"},
			GenreWeight:    4,
			MinBPM:         108,
			MaxBPM:         140,
			TempoWeight:    2,
			FavoriteWeight: 3,
			HistoryWeight:  2,
			ArtistWeight:   1,
		},
	},
}

// DJs returns the built-in DJs. Service.DJs also lists the Listener's own.
func DJs() []DJ {
	return append([]DJ(nil), builtIn...)
}

// FindDJ returns the built-in DJ with the id. Use Service.DJ to find any DJ a Listener can
// start a Station with, their own included.
func FindDJ(id string) (DJ, bool) {
	for _, dj := range builtIn {
		if dj.ID == id {
			return dj, true
		}
	}
	return DJ{}, false
}

// score is how well mf suits the DJ. history is how much the Listener plays mf compared to the
// most played candidate and artistAffinity how much they like its artist, both from 0 to 1.
func (t Taste) score(mf model.MediaFile, history, artistAffinity float64) float64 {
	s := 1.0
	if t.GenreWeight > 0 && t.likesGenre(mf) {
		s += t.GenreWeight
	}
	if t.TempoWeight > 0 && mf.BPM != nil && *mf.BPM > 0 && *mf.BPM >= t.MinBPM && *mf.BPM <= t.MaxBPM {
		s += t.TempoWeight
	}
	s += t.FavoriteWeight * favorite(mf)
	s += t.HistoryWeight * history
	s += t.ArtistWeight * artistAffinity
	if mf.PlayCount == 0 {
		s += t.UnplayedWeight
	}
	if t.OlderThan > 0 && mf.Year > 0 && mf.Year < t.OlderThan {
		s += t.OlderWeight
	}
	if mf.Rating > 0 && mf.Rating <= 2 {
		s /= 4
	}
	return s
}

func (t Taste) likesGenre(mf model.MediaFile) bool {
	genres := append([]string{mf.Genre}, mf.Tags.Values(model.TagGenre)...)
	for _, g := range genres {
		g = strings.ToLower(g)
		if g == "" {
			continue
		}
		for _, word := range t.Genres {
			if t.namesGenre(word, g) {
				return true
			}
		}
	}
	return false
}

// namesGenre says whether the DJ's genre word names the lower-case genre.
func (t Taste) namesGenre(word, genre string) bool {
	if t.WholeGenres {
		return word == genre
	}
	return strings.Contains(genre, word)
}

// favorite is 1 for a starred or 5-star Track, 0.5 for a 4-star one, and 0 otherwise.
func favorite(mf model.MediaFile) float64 {
	switch {
	case mf.Starred || mf.Rating == 5:
		return 1
	case mf.Rating == 4:
		return 0.5
	}
	return 0
}

// plays turns a play count into a slowly growing number, so one Track played 500 times
// doesn't drown out everything else the Listener plays.
func plays(count int64) float64 {
	return math.Log1p(float64(max(count, 0)))
}
