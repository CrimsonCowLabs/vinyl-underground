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
		// Artist and title first; then the title alone, for an artist the index can't find by
		// its words (a channel's "TaylorSwiftVEVO").
		queries := []string{g.Title}
		if g.Artist != "" {
			queries = []string{g.Artist + " " + g.Title, g.Title}
		}
		for _, q := range queries {
			found, err := m.ds.MediaFile(ctx).Search(q, model.QueryOptions{Max: searchMax})
			if err != nil {
				return nil, err
			}
			n := len(res)
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
			if len(res) > n {
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

// sameArtist compares two artist names without their spaces, so a channel's "TaylorSwift"
// matches "Taylor Swift", and accepts one inside the other for a list of artists.
func sameArtist(a, b string) bool {
	a = strings.ReplaceAll(Key(a), " ", "")
	b = strings.ReplaceAll(Key(b), " ", "")
	if a == "" || b == "" {
		return false
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	return a == b || (len(a) >= minContained && strings.Contains(b, a))
}
