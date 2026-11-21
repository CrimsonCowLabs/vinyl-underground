// Package generatedplaylist makes Generated Playlists: a Listener's Prompt becomes a preview of
// real Tracks from the Library, which they can Save as an ordinary Playlist that remembers its
// Prompt. See docs/adr/0004-llm-ranks-library-candidates-never-invents-tracks.md.
//
// The LLM never names Tracks itself. It makes two calls:
//  1. filters: the Prompt (plus the Library's genres) becomes a title, genres, seed artists and
//     a year range.
//  2. ranking: the server builds up to MaxCandidates Tracks from the Library matching those
//     filters and the seed artists' similar artists (from Last.fm), one compact line each, and
//     the LLM picks and orders them by number.
//
// Every Track returned is one of the candidates, so it exists in the Library and is visible to
// the Listener.
package generatedplaylist

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	. "github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/core/llm"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
)

const (
	DefaultLength = 25
	MinLength     = 10
	MaxLength     = 100
	// MaxCandidates caps the Tracks sent to the LLM, so prompts stay short enough for a ~4B
	// model on small hardware (~33 s to rank 150 on an 8 GB Apple Silicon Mac).
	MaxCandidates = 150
	// MaxPromptLength is the longest Prompt accepted, in characters.
	MaxPromptLength = 500

	maxNameLength    = 100
	maxSeedArtists   = 10
	maxPromptGenres  = 100
	minCandidates    = 60
	candidatesFactor = 3
)

var (
	ErrInvalidPrompt = errors.New("the Prompt must be 1 to 500 characters")
	ErrInvalidLength = fmt.Errorf("the length must be from %d to %d Tracks", MinLength, MaxLength)
	ErrNoTracks      = errors.New("no Tracks in the Library to choose from")
)

// Preview is a Generated Playlist before it is saved.
type Preview struct {
	Prompt string           `json:"prompt"`
	Name   string           `json:"name"`
	Tracks model.MediaFiles `json:"tracks"`
}

type Service interface {
	// Enabled reports whether an LLM is configured. Without one, Generated Playlists are hidden.
	Enabled() bool
	// Preview picks length Tracks (DefaultLength when 0) from the Library for the Prompt.
	Preview(ctx context.Context, prompt string, length int) (*Preview, error)
	// Save creates the Listener's Playlist holding trackIDs in order, remembering its Prompt.
	// IDs that aren't Tracks the Listener can see are dropped.
	Save(ctx context.Context, name, prompt string, trackIDs []string) (string, error)
}

// New returns the Service. A nil client means no LLM is configured. A nil similar means
// candidates come from local tags alone.
func New(ds model.DataStore, client llm.Client, similar SimilarArtists) Service {
	return &service{ds: ds, llm: client, similar: similar}
}

type service struct {
	ds      model.DataStore
	llm     llm.Client
	similar SimilarArtists
}

func (s *service) Enabled() bool {
	return s.llm != nil
}

func cleanPrompt(prompt string) (string, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" || utf8.RuneCountInString(prompt) > MaxPromptLength {
		return "", ErrInvalidPrompt
	}
	return prompt, nil
}

func (s *service) Preview(ctx context.Context, prompt string, length int) (*Preview, error) {
	prompt, err := cleanPrompt(prompt)
	if err != nil {
		return nil, err
	}
	if length == 0 {
		length = DefaultLength
	}
	if length < MinLength || length > MaxLength {
		return nil, ErrInvalidLength
	}

	genres, err := s.libraryGenres(ctx)
	if err != nil {
		return nil, err
	}
	f, err := s.askFilters(ctx, prompt, genres)
	if err != nil {
		return nil, err
	}
	log.Debug(ctx, "Generated Playlist filters", "prompt", prompt, "filters", f)

	candidates, err := s.candidates(ctx, f, candidateTarget(length))
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, ErrNoTracks
	}

	picks, err := s.askRanking(ctx, prompt, length, candidates)
	if err != nil {
		return nil, err
	}
	name := f.Name
	if name == "" {
		name = truncate(prompt, maxNameLength)
	}
	tracks, picked := pickTracks(candidates, picks, length)
	log.Info(ctx, "Generated Playlist preview", "length", length, "candidates", len(candidates),
		"llmPicks", len(picks), "validPicks", picked, "tracks", len(tracks))
	return &Preview{Prompt: prompt, Name: name, Tracks: tracks}, nil
}

// candidateTarget is how many candidates to offer the LLM for a Playlist of length Tracks.
func candidateTarget(length int) int {
	return min(max(length*candidatesFactor, minCandidates), MaxCandidates)
}

// maxPerArtist is how many Tracks one artist may have in a Playlist of length Tracks. A small
// model tends to pick a whole album in a row when it is among the candidates.
func maxPerArtist(length int) int {
	return max(3, length/8)
}

// pickTracks returns the candidates the LLM picked (1-based numbers), in its order. Numbers that
// aren't candidates, repeats, and picks beyond an artist's share (maxPerArtist) are dropped. If
// that leaves fewer than length, the rest come from the remaining candidates, best matches first,
// keeping to the artists' shares unless there is nothing else. It also returns how many of the
// LLM's picks it kept.
func pickTracks(candidates model.MediaFiles, picks []int, length int) (model.MediaFiles, int) {
	res := make(model.MediaFiles, 0, min(length, len(candidates)))
	used := make([]bool, len(candidates))
	perArtist := map[string]int{}
	limit := maxPerArtist(length)
	take := func(i int, capped bool) {
		artist := strings.ToLower(oneLine(candidates[i].Artist))
		if len(res) == length || used[i] || (capped && perArtist[artist] >= limit) {
			return
		}
		used[i] = true
		perArtist[artist]++
		res = append(res, candidates[i])
	}
	for _, n := range picks {
		if n >= 1 && n <= len(candidates) {
			take(n-1, true)
		}
	}
	picked := len(res)
	for _, capped := range []bool{true, false} {
		for i := range candidates {
			take(i, capped)
		}
	}
	return res, picked
}

// libraryGenres returns the names of the Listener's Library's most common genres.
func (s *service) libraryGenres(ctx context.Context) ([]string, error) {
	genres, err := s.ds.Genre(ctx).GetAll(model.QueryOptions{Sort: "song_count", Order: "desc", Max: maxPromptGenres})
	if err != nil {
		return nil, fmt.Errorf("reading genres: %w", err)
	}
	names := make([]string, 0, len(genres))
	for _, g := range genres {
		if g.Name != "" {
			names = append(names, g.Name)
		}
	}
	return names, nil
}

func (s *service) Save(ctx context.Context, name, prompt string, trackIDs []string) (string, error) {
	prompt, err := cleanPrompt(prompt)
	if err != nil {
		return "", err
	}
	if len(trackIDs) > MaxLength {
		return "", ErrInvalidLength
	}
	ids, err := s.existingTracks(ctx, trackIDs)
	if err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return "", ErrNoTracks
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = truncate(prompt, maxNameLength)
	}

	usr, _ := request.UserFrom(ctx)
	pls := &model.Playlist{Name: name, OwnerID: usr.ID, Prompt: prompt}
	pls.AddMediaFilesByID(ids)
	if err := s.ds.Playlist(ctx).Put(pls); err != nil {
		return "", fmt.Errorf("saving Generated Playlist: %w", err)
	}
	return pls.ID, nil
}

// existingTracks returns the ids, in order and without repeats, that are Tracks the Listener can
// see. Anything else, such as an id the LLM made up, is dropped.
func (s *service) existingTracks(ctx context.Context, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	found, err := s.ds.MediaFile(ctx).GetAll(model.QueryOptions{
		Filters: And{Eq{"media_file.id": ids}, Eq{"media_file.missing": false}},
	})
	if err != nil {
		return nil, fmt.Errorf("reading Tracks: %w", err)
	}
	exists := make(map[string]bool, len(found))
	for _, mf := range found {
		exists[mf.ID] = true
	}
	res := make([]string, 0, len(found))
	for _, id := range ids {
		if exists[id] {
			res = append(res, id)
			delete(exists, id)
		}
	}
	return res, nil
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:n]))
}
