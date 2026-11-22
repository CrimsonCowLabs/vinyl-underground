package generatedplaylist

import (
	"context"
	"fmt"
	"strings"

	. "github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/model"
)

// candidates returns up to target Tracks from the pool for the filters, best matches first.
// Every search keeps to the pool, so a Station's DJ's genres hold however loose it gets. It goes
// through looser and looser searches until it has enough:
//  1. the seed artists, then their similar artists in the Library, then the genres, within the
//     years;
//  2. the same, ignoring the years;
//  3. anything within the years;
//  4. anything.
//
// Within each search the Tracks are random, so the same Prompt can give a different Playlist.
// The seed artists take at most a third of the candidates, shared between them and at most
// maxPerArtistSearch each. Their similar artists take at most another third, in the same way. No
// artist gets more than a tenth (at least 5), so a few prolific artists can't crowd out the rest.
// Only a Library too small for that ignores the artist limit.
func (s *service) candidates(ctx context.Context, f filters, target int, p pool) (model.MediaFiles, error) {
	c := &collector{target: target, artistCap: max(5, target/10),
		seen: map[string]bool{}, seenSong: map[string]bool{}, perArtist: map[string]int{}}
	type search struct {
		matching bool // the seed artists, their similar artists and the genres, or anything
		years    Sqlizer
	}
	similar := s.similarArtists(ctx, f.SeedArtists)
	years := yearsFilter(f)
	searches := []search{{matching: true, years: years}}
	if years != nil {
		searches = append(searches, search{matching: true}, search{years: years})
	}
	searches = append(searches, search{})

	for _, search := range searches {
		if c.full() {
			break
		}
		if !search.matching {
			mfs, err := s.search(ctx, p, c.room(), search.years)
			if err != nil {
				return nil, err
			}
			c.add(mfs)
			continue
		}
		for _, artists := range [][]string{f.SeedArtists, similar} {
			if err := s.addArtists(ctx, c, p, artists, search.years); err != nil {
				return nil, err
			}
		}
		if len(f.Genres) > 0 && !c.full() {
			mfs, err := s.search(ctx, p.in(f.Genres), c.room(), search.years)
			if err != nil {
				return nil, err
			}
			c.add(mfs)
		}
	}
	if !c.full() {
		c.artistCap = 0
		mfs, err := s.search(ctx, p, c.room())
		if err != nil {
			return nil, err
		}
		c.add(mfs)
	}
	return c.tracks, nil
}

const maxPerArtistSearch = 6

// addArtists adds Tracks by the artists, sharing a third of the candidates between them, at most
// maxPerArtistSearch each.
func (s *service) addArtists(ctx context.Context, c *collector, p pool, artists []string, years Sqlizer) error {
	if len(artists) == 0 {
		return nil
	}
	perArtist := min(maxPerArtistSearch, max(1, (c.target/3+len(artists)-1)/len(artists)))
	for _, artist := range artists {
		if c.full() {
			return nil
		}
		mfs, err := s.search(ctx, p, perArtist, artistFilter(artist), years)
		if err != nil {
			return err
		}
		c.add(mfs[:min(len(mfs), perArtist)])
	}
	return nil
}

// collector gathers distinct candidates up to target, at most artistCap per artist (0 for no
// limit). Two Tracks with the same artist and title (the same song on an album and a
// compilation) count as one.
type collector struct {
	target    int
	artistCap int
	tracks    model.MediaFiles
	seen      map[string]bool
	seenSong  map[string]bool
	perArtist map[string]int
}

func (c *collector) full() bool { return len(c.tracks) >= c.target }
func (c *collector) room() int  { return c.target - len(c.tracks) }

func (c *collector) add(mfs model.MediaFiles) {
	for _, mf := range mfs {
		if c.full() {
			return
		}
		artist := strings.ToLower(oneLine(mf.Artist))
		song := artist + "\x00" + strings.ToLower(oneLine(mf.Title))
		if c.seen[mf.ID] || c.seenSong[song] || (c.artistCap > 0 && c.perArtist[artist] >= c.artistCap) {
			continue
		}
		c.seen[mf.ID] = true
		c.seenSong[song] = true
		c.perArtist[artist]++
		c.tracks = append(c.tracks, mf)
	}
}

func yearsFilter(f filters) Sqlizer {
	var and And
	if f.YearFrom > 0 {
		and = append(and, GtOrEq{"media_file.year": f.YearFrom})
	}
	if f.YearTo > 0 {
		and = append(and, LtOrEq{"media_file.year": f.YearTo})
	}
	if len(and) == 0 {
		return nil
	}
	return and
}

// pool is the Library Tracks that may be candidates: none with an ID in exclude, and when there
// are genres (the Library's exact names), only Tracks with a genre tag among them.
type pool struct {
	exclude []string
	genres  []string
}

// in is the pool's Tracks in the genres, which must be some of the pool's own when it has any.
func (p pool) in(genres []string) pool {
	return pool{exclude: p.exclude, genres: genres}
}

func artistFilter(name string) Sqlizer {
	name = strings.ToLower(name)
	return Or{
		Expr("lower(media_file.artist) = ?", name),
		Expr("lower(media_file.album_artist) = ?", name),
	}
}

// searchOptions picks random, present Tracks matching every filter given: three times max, so
// that after dropping repeats and artists over their limit there are still enough new ones.
func searchOptions(max int, filters ...Sqlizer) model.QueryOptions {
	and := And{Eq{"media_file.missing": false}}
	for _, f := range filters {
		if f != nil {
			and = append(and, f)
		}
	}
	return model.QueryOptions{Filters: and, Sort: "random", Max: max * 3}
}

// search picks random Tracks from the pool matching every filter given (see searchOptions).
func (s *service) search(ctx context.Context, p pool, max int, filters ...Sqlizer) (model.MediaFiles, error) {
	if len(p.exclude) > 0 {
		filters = append(filters, NotEq{"media_file.id": p.exclude})
	}
	opts := searchOptions(max, filters...)
	repo := s.ds.MediaFile(ctx)
	var mfs model.MediaFiles
	var err error
	if len(p.genres) > 0 {
		mfs, err = repo.GetAllByTags(model.TagGenre, p.genres, opts)
	} else {
		mfs, err = repo.GetAll(opts)
	}
	if err != nil {
		return nil, fmt.Errorf("finding candidates: %w", err)
	}
	return mfs, nil
}
