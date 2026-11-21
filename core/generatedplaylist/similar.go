package generatedplaylist

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// SimilarArtists finds artists similar to a named one (from Last.fm and the other metadata
// agents), keeping only those in the Library. external.Provider implements it.
type SimilarArtists interface {
	SimilarArtists(ctx context.Context, artistName string, count int) (model.Artists, error)
}

// similarWait is how long a Preview waits for similar-artist lookups before carrying on with
// what it has. A slow or unreachable Last.fm costs at most this.
var similarWait = 4 * time.Second

const (
	// similarLookupTimeout lets a lookup that outlives similarWait finish in the background.
	// For a seed artist in the Library, its answer is then cached with the artist's info for
	// the next Preview.
	similarLookupTimeout = 15 * time.Second
	// similarPerSeed is how many similar artists to take from each seed artist's lookup.
	similarPerSeed = 5
	// maxSimilarToSeeds caps the similar artists searched for, across all seed artists.
	maxSimilarToSeeds = 20
)

// similarArtists returns the names of Library artists similar to the seed artists, without the
// seeds themselves: the first similar artist of each seed, then the second, and so on. Seeds
// whose lookup fails or is too slow are skipped, so with Last.fm unreachable it returns nothing
// and the candidates come from local tags alone.
func (s *service) similarArtists(ctx context.Context, seeds []string) []string {
	if s.similar == nil || len(seeds) == 0 {
		return nil
	}
	lookupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), similarLookupTimeout)
	results := make([][]string, len(seeds))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i, seed := range seeds {
		wg.Go(func() {
			artists, err := s.similar.SimilarArtists(lookupCtx, seed, similarPerSeed)
			if err != nil {
				log.Debug(ctx, "No similar artists for Generated Playlist seed", "artist", seed, err)
				return
			}
			names := make([]string, 0, len(artists))
			for _, a := range artists {
				names = append(names, a.Name)
			}
			mu.Lock()
			results[i] = names
			mu.Unlock()
		})
	}
	// Slow lookups outlive this function: they stop at similarLookupTimeout, not when the
	// Preview's ctx is done, and results is only read under mu.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		cancel()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(similarWait):
		log.Warn(ctx, "Similar-artist lookups are slow; building Generated Playlist candidates without the rest",
			"wait", similarWait)
	case <-ctx.Done():
	}

	mu.Lock()
	defer mu.Unlock()
	seen := map[string]bool{}
	for _, seed := range seeds {
		seen[strings.ToLower(seed)] = true
	}
	var res []string
	for n := 0; n < similarPerSeed; n++ {
		for _, names := range results {
			if n >= len(names) || len(res) == maxSimilarToSeeds {
				continue
			}
			name := strings.TrimSpace(names[n])
			if key := strings.ToLower(name); name != "" && !seen[key] {
				seen[key] = true
				res = append(res, name)
			}
		}
	}
	return res
}
