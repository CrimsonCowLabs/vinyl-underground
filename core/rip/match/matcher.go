package match

import (
	"context"
	"strings"

	"github.com/navidrome/navidrome/model"
)

const (
	// searchMax bounds each full-text search. A title-only search of a common title ("Intro")
	// can miss a Track beyond it, which only costs a warning.
	searchMax = 50
	// maxMatches is the most Tracks listed for one video.
	maxMatches = 5
	// minContained is the shortest artist name found inside a longer one ("Daft Punk" in
	// "Daft Punk, Pharrell Williams"), so a short name doesn't match by chance.
	minContained = 4
)

// Matcher finds the Tracks the Library and the Inbox already seem to have for a video, among
// those the user in the context can see. It asks the Track repository's full-text search for
// candidates and never reads the whole Library.
type Matcher struct {
	ds model.DataStore
}

func New(ds model.DataStore) *Matcher {
	return &Matcher{ds: ds}
}

// AlreadyHave returns the Tracks that seem to be the video's recording: same title and artist,
// once the YouTube noise, accents, punctuation and featuring credits are taken out. artist may
// be the video's uploader or channel when the site gives no artist.
func (m *Matcher) AlreadyHave(ctx context.Context, artist, title string) (model.RipMatches, error) {
	res := model.RipMatches{}
	seen := map[string]bool{}
	for _, g := range Guesses(artist, title) {
		// The title alone finds the artist however the index spells it (a channel's
		// "TaylorSwiftVEVO"). Only when it lists too many Tracks to be sure (a common title, like
		// "Intro") is it narrowed down by the artist. One search is the usual cost.
		queries := []string{g.Title}
		if g.Artist != "" {
			queries = append(queries, g.Artist+" "+g.Title)
		}
		for _, q := range queries {
			found, err := m.ds.MediaFile(ctx).Search(q, model.QueryOptions{Max: searchMax})
			if err != nil {
				return nil, err
			}
			for _, mf := range found {
				if seen[mf.ID] || !sameRecording(g, mf) {
					continue
				}
				seen[mf.ID] = true
				res = append(res, model.RipMatch{
					ID: mf.ID, Title: mf.Title, Artist: mf.Artist, Album: mf.Album, Library: mf.LibraryName,
					Source: mf.Source, SourceURL: mf.SourceURL,
				})
				if len(res) == maxMatches {
					return res, nil
				}
			}
			if len(found) < searchMax {
				break
			}
		}
	}
	return res, nil
}

// sameRecording reports whether the Track reads as the guessed artist and title. The Track's
// own tags are read like a video's, as an earlier Rip in the Inbox still has its YouTube title.
func sameRecording(g Guess, mf model.MediaFile) bool {
	title := Key(g.Title)
	for _, artist := range []string{mf.Artist, mf.AlbumArtist} {
		for _, t := range Guesses(artist, mf.Title) {
			if Key(t.Title) == title && sameArtist(g.Artist, t.Artist) {
				return true
			}
		}
	}
	return false
}

// sameArtist compares two artist names. Without their spaces, so a channel's "TaylorSwift"
// matches "Taylor Swift"; or word for word, one inside the other, so "Daft Punk" matches
// "Daft Punk, Julian Casablancas" (but "Queen" doesn't match "Queens of the Stone Age").
func sameArtist(a, b string) bool {
	a, b = Key(a), Key(b)
	if a == "" || b == "" {
		return false
	}
	if strings.ReplaceAll(a, " ", "") == strings.ReplaceAll(b, " ", "") {
		return true
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	return len(a) >= minContained && strings.Contains(" "+b+" ", " "+a+" ")
}
