package generatedplaylist

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/navidrome/navidrome/core/llm"
	"github.com/navidrome/navidrome/model"
)

// filters is what the LLM makes of a Prompt, after cleanup: genres are the Library's own genre
// names, and a zero year means no limit on that side.
type filters struct {
	Name        string
	Genres      []string
	SeedArtists []string
	YearFrom    int
	YearTo      int
}

type filtersReply struct {
	Name        string   `json:"name"`
	Genres      []string `json:"genres"`
	SeedArtists []string `json:"seed_artists"`
	YearFrom    int      `json:"year_from"`
	YearTo      int      `json:"year_to"`
}

var filtersSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"name":         map[string]any{"type": "string"},
		"genres":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"seed_artists": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"year_from":    map[string]any{"type": "integer"},
		"year_to":      map[string]any{"type": "integer"},
	},
	"required": []string{"name", "genres", "seed_artists", "year_from", "year_to"},
}

const filtersSystem = `You help build a music playlist from a listener's request, using only music in their library.
Turn the request into search filters:
- name: a short playlist title (2 to 5 words) for the request.
- genres: genres from the library's genre list that fit the request. Use the list's exact names. Empty if none fit.
- seed_artists: up to 10 well-known artists whose music fits the request.
- year_from, year_to: the release years the request asks for, e.g. 1990 and 1999 for "90s". Use 0 when the request doesn't limit the years.`

func (s *service) askFilters(ctx context.Context, prompt string, libraryGenres []string) (filters, error) {
	user := fmt.Sprintf("Request: %s\n\nGenres in the library: %s", prompt, strings.Join(libraryGenres, ", "))
	var reply filtersReply
	err := s.llm.Complete(ctx, llm.Request{System: filtersSystem, User: user, SchemaName: "filters", Schema: filtersSchema}, &reply)
	if err != nil {
		return filters{}, fmt.Errorf("turning the Prompt into filters: %w", err)
	}
	return cleanFilters(reply, libraryGenres), nil
}

// cleanFilters keeps only what can be looked up in the Library: genres it has (matched without
// case, or one name containing the other), a few distinct seed artists, and plausible years.
func cleanFilters(r filtersReply, libraryGenres []string) filters {
	f := filters{Name: truncate(strings.TrimSpace(r.Name), maxNameLength)}

	seen := map[string]bool{}
	for _, want := range r.Genres {
		want = strings.ToLower(strings.TrimSpace(want))
		if want == "" {
			continue
		}
		for _, g := range libraryGenres {
			lg := strings.ToLower(g)
			if !seen[g] && (lg == want || strings.Contains(lg, want) || strings.Contains(want, lg)) {
				seen[g] = true
				f.Genres = append(f.Genres, g)
			}
		}
	}

	seenArtist := map[string]bool{}
	for _, a := range r.SeedArtists {
		a = strings.TrimSpace(a)
		key := strings.ToLower(a)
		if a == "" || seenArtist[key] || len(f.SeedArtists) == maxSeedArtists {
			continue
		}
		seenArtist[key] = true
		f.SeedArtists = append(f.SeedArtists, a)
	}

	validYear := func(y int) int {
		if y < 1000 || y > 2999 {
			return 0
		}
		return y
	}
	f.YearFrom, f.YearTo = validYear(r.YearFrom), validYear(r.YearTo)
	if f.YearFrom != 0 && f.YearTo != 0 && f.YearFrom > f.YearTo {
		f.YearFrom, f.YearTo = f.YearTo, f.YearFrom
	}
	return f
}

type rankingReply struct {
	Tracks []int `json:"tracks"`
}

const rankingSystem = `You pick and order Tracks for a music playlist from a numbered list of candidates.
Choose the Tracks that best fit the listener's request, in a good listening order.
Answer only with candidate numbers from the list. Never repeat a number.`

func rankingSchema(length int) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tracks": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "maxItems": length},
		},
		"required": []string{"tracks"},
	}
}

func (s *service) askRanking(ctx context.Context, prompt string, length int, candidates model.MediaFiles) ([]int, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "Request: %s\nPick %d Tracks.\n\nCandidates:\n", prompt, min(length, len(candidates)))
	for i, mf := range candidates {
		b.WriteString(candidateLine(i+1, mf))
		b.WriteByte('\n')
	}
	var reply rankingReply
	err := s.llm.Complete(ctx, llm.Request{System: rankingSystem, User: b.String(), SchemaName: "ranking", Schema: rankingSchema(length)}, &reply)
	if err != nil {
		return nil, fmt.Errorf("ranking candidates: %w", err)
	}
	return reply.Tracks, nil
}

// candidateLine is one compact candidate line: "12. Artist - Title (1994, Indie Rock)".
func candidateLine(n int, mf model.MediaFile) string {
	line := strconv.Itoa(n) + ". " + oneLine(mf.Artist) + " - " + oneLine(mf.Title)
	var extra []string
	if mf.Year > 0 {
		extra = append(extra, strconv.Itoa(mf.Year))
	}
	if mf.Genre != "" {
		extra = append(extra, oneLine(mf.Genre))
	}
	if len(extra) > 0 {
		line += " (" + strings.Join(extra, ", ") + ")"
	}
	return line
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
