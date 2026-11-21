package generatedplaylist

import (
	"context"
	"fmt"
	"strings"

	. "github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/model"
)

// candidates returns up to target Tracks from the Listener's Library for the filters, best
// matches first. It goes through looser and looser searches until it has enough:
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
func (s *service) candidates(ctx context.Context, f filters, target int, exclude Sqlizer) (model.MediaFiles, error) {
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
			mfs, err := s.search(ctx, c.room(), exclude, search.years)
			if err != nil {
				return nil, err
			}
			c.add(mfs)
			continue
		}
		for _, artists := range [][]string{f.SeedArtists, similar} {
			if err := s.addArtists(ctx, c, artists, exclude, search.years); err != nil {
				return nil, err
			}
		}
		if len(f.Genres) > 0 && !c.full() {
			mfs, err := s.searchGenres(ctx, f.Genres, c.room(), exclude, search.years)
			if err != nil {
				return nil, err
			}
			c.add(mfs)
		}
	}
	if !c.full() {
		c.artistCap = 0
		mfs, err := s.search(ctx, c.room(), exclude)
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
func (s *service) addArtists(ctx context.Context, c *collector, artists []string, exclude, years Sqlizer) error {
	if len(artists) == 0 {
		return nil
	}
	perArtist := min(maxPerArtistSearch, max(1, (c.target/3+len(artists)-1)/len(artists)))
	for _, artist := range artists {
		if c.full() {
			return nil
		}
		mfs, err := s.search(ctx, perArtist, artistFilter(artist), exclude, years)
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

// excludeFilter leaves out the Tracks with the ids, or is nil for none.
func excludeFilter(ids []string) Sqlizer {
	if len(ids) == 0 {
		return nil
	}
	return NotEq{"media_file.id": ids}
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

func (s *service) search(ctx context.Context, max int, filters ...Sqlizer) (model.MediaFiles, error) {
	mfs, err := s.ds.MediaFile(ctx).GetAll(searchOptions(max, filters...))
	if err != nil {
		return nil, fmt.Errorf("finding candidates: %w", err)
	}
	return mfs, nil
}

func (s *service) searchGenres(ctx context.Context, genres []string, max int, filters ...Sqlizer) (model.MediaFiles, error) {
	mfs, err := s.ds.MediaFile(ctx).GetAllByTags(model.TagGenre, genres, searchOptions(max, filters...))
	if err != nil {
		return nil, fmt.Errorf("finding candidates by genre: %w", err)
	}
	return mfs, nil
}
