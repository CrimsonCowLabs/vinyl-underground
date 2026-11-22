package stations

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/navidrome/navidrome/model"
)

// A Listener's own DJ has genres (#106): names of the Library's genre tags, which rule-based
// selection plays the way a built-in DJ's genres are played, strict rule included. They are
// worked out from the DJ's name and description when it is saved (deriveGenres), until the
// Listener picks them by hand; from then on editing the description leaves them alone.

func (s *service) Genres(ctx context.Context) ([]string, error) {
	return s.libraryGenres(ctx)
}

func (s *service) MatchGenres(ctx context.Context, name, description string) ([]string, error) {
	genres, _, err := s.deriveGenres(ctx, name, description)
	return []string(genres), err
}

// libraryGenres returns the names of the genres in the Listener's Library, by name.
func (s *service) libraryGenres(ctx context.Context) ([]string, error) {
	genres, err := s.ds.Genre(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("listing the Library's genres: %w", err)
	}
	res := make([]string, 0, len(genres))
	for _, g := range genres {
		if g.Name != "" {
			res = append(res, g.Name)
		}
	}
	slices.SortFunc(res, func(a, b string) int { return cmp.Compare(strings.ToLower(a), strings.ToLower(b)) })
	return slices.CompactFunc(res, strings.EqualFold), nil
}

// deriveGenres works out the genres for a DJ with the name and description, from the
// Library's genres, and says where they came from. It is the one place a DJ's genres are
// worked out rather than picked by the Listener.
func (s *service) deriveGenres(ctx context.Context, name, description string) (model.DJGenres, model.DJGenreSource, error) {
	library, err := s.libraryGenres(ctx)
	if err != nil {
		return nil, "", err
	}
	return matchGenreWords(name+"\n"+description, library), model.DJGenresFromWords, nil
}

// libraryOnly keeps the genres the Library has, in the Library's own spelling, once each.
func libraryOnly(genres, library []string) model.DJGenres {
	res := model.DJGenres{}
	for _, g := range genres {
		i := slices.IndexFunc(library, func(l string) bool { return strings.EqualFold(l, strings.TrimSpace(g)) })
		if i >= 0 && !slices.Contains(res, library[i]) {
			res = append(res, library[i])
		}
	}
	return res
}

// genericWords say nothing about a genre on their own: "music" mustn't match "World Music".
// A genre made of them still matches when the text names it whole ("new wave").
var genericWords = map[string]bool{
	"a": true, "an": true, "and": true, "the": true, "of": true, "n": true, "music": true,
	"song": true, "songs": true, "track": true, "tracks": true, "tune": true, "tunes": true,
	"sound": true, "sounds": true, "style": true, "new": true, "old": true, "other": true,
	"misc": true, "various": true, "general": true, "all": true, "my": true, "your": true,
	"best": true, "good": true, "great": true, "love": true, "night": true, "day": true,
	"time": true, "late": true, "early": true, "only": true, "with": true, "for": true,
}

// matchGenreWords returns the Library genres the text names, case-insensitively: a genre
// whose words all appear together in the text ("deep house" names "Deep House"), or one with
// a telling word the text has ("techno" names "Techno", "Hardcore Techno" and "Euro-Techno").
func matchGenreWords(text string, library []string) model.DJGenres {
	words := genreWords(text)
	has := make(map[string]bool, len(words))
	for _, w := range words {
		has[w] = true
	}
	res := model.DJGenres{}
	for _, g := range library {
		gw := genreWords(g)
		if len(gw) == 0 {
			continue
		}
		named := containsRun(words, gw)
		for _, w := range gw {
			named = named || (len(w) > 1 && !genericWords[w] && has[w])
		}
		if named {
			res = append(res, g)
		}
	}
	return res
}

// genreWords splits text into lower-case words of letters and digits ("Club-House" is "club",
// "house"; "R&B" is "r", "b").
func genreWords(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// containsRun says whether run appears in words, in order and side by side.
func containsRun(words, run []string) bool {
	for i := 0; i+len(run) <= len(words); i++ {
		if slices.Equal(words[i:i+len(run)], run) {
			return true
		}
	}
	return false
}
