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
//  1. the seed artists, then the genres, within the years;
//  2. the same, ignoring the years;
//  3. anything within the years;
//  4. anything.
//
// Within each search the Tracks are random, so the same Prompt can give a different Playlist.
// The seed artists take at most half the candidates, shared between them, so one prolific artist
// can't crowd out the rest.
func (s *service) candidates(ctx context.Context, f filters, target int) (model.MediaFiles, error) {
	c := &collector{target: target, seen: map[string]bool{}, seenSong: map[string]bool{}}
	type search struct {
		matching bool // the seed artists and genres, or anything
		years    Sqlizer
	}
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
			mfs, err := s.search(ctx, nil, search.years, c.room())
			if err != nil {
				return nil, err
			}
			c.add(mfs)
			continue
		}
		if len(f.SeedArtists) > 0 {
			perArtist := max(1, (target/2+len(f.SeedArtists)-1)/len(f.SeedArtists))
			for _, artist := range f.SeedArtists {
				if c.full() {
					break
				}
				mfs, err := s.search(ctx, artistFilter(artist), search.years, perArtist)
				if err != nil {
					return nil, err
				}
				c.add(mfs[:min(len(mfs), perArtist)])
			}
		}
		if len(f.Genres) > 0 && !c.full() {
			mfs, err := s.searchGenres(ctx, f.Genres, search.years, c.room())
			if err != nil {
				return nil, err
			}
			c.add(mfs)
		}
	}
	return c.tracks, nil
}

// collector gathers distinct candidates up to target. Two Tracks with the same artist and
// title (the same song on an album and a compilation) count as one.
type collector struct {
	target   int
	tracks   model.MediaFiles
	seen     map[string]bool
	seenSong map[string]bool
}

func (c *collector) full() bool { return len(c.tracks) >= c.target }
func (c *collector) room() int  { return c.target - len(c.tracks) }

func (c *collector) add(mfs model.MediaFiles) {
	for _, mf := range mfs {
		if c.full() {
			return
		}
		song := strings.ToLower(oneLine(mf.Artist)) + "\x00" + strings.ToLower(oneLine(mf.Title))
		if c.seen[mf.ID] || c.seenSong[song] {
			continue
		}
		c.seen[mf.ID] = true
		c.seenSong[song] = true
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

func artistFilter(name string) Sqlizer {
	name = strings.ToLower(name)
	return Or{
		Expr("lower(media_file.artist) = ?", name),
		Expr("lower(media_file.album_artist) = ?", name),
	}
}

// searchOptions picks max random, present Tracks matching every filter given. Extra rows are
// fetched so that repeats of Tracks already collected still leave enough new ones.
func searchOptions(max int, filters ...Sqlizer) model.QueryOptions {
	and := And{Eq{"media_file.missing": false}}
	for _, f := range filters {
		if f != nil {
			and = append(and, f)
		}
	}
	return model.QueryOptions{Filters: and, Sort: "random", Max: max * 2}
}

func (s *service) search(ctx context.Context, filter, years Sqlizer, max int) (model.MediaFiles, error) {
	mfs, err := s.ds.MediaFile(ctx).GetAll(searchOptions(max, filter, years))
	if err != nil {
		return nil, fmt.Errorf("finding candidates: %w", err)
	}
	return mfs, nil
}

func (s *service) searchGenres(ctx context.Context, genres []string, years Sqlizer, max int) (model.MediaFiles, error) {
	mfs, err := s.ds.MediaFile(ctx).GetAllByTags(model.TagGenre, genres, searchOptions(max, years))
	if err != nil {
		return nil, fmt.Errorf("finding candidates by genre: %w", err)
	}
	return mfs, nil
}
