// Package stations runs Stations: a Listener picks a built-in DJ and gets an endless,
// skippable queue of Tracks from their Library, refilled from the server a few Tracks at a
// time. See docs/adr/0005-stations-are-personal-not-live-broadcast.md.
//
// Without an LLM, selection is rule-based. Each refill gathers candidates from the Library (a
// random sample, the Listener's favorites and most played, and Tracks that suit the DJ), and
// Pick chooses among them by the DJ's Taste while keeping Tracks from repeating within
// RepeatWindow and artists within ArtistGap Tracks. With an LLM, the DJ's persona and an
// optional Theme choose the Tracks instead, selected ahead so playback never waits on it (see
// themes.go); rule-based selection fills in until those picks are ready. What each
// Listener's Station served is kept in the station_track table, so Stations are independent
// and survive a restart.
package stations

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	. "github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
)

const (
	// DefaultCount is how many Tracks a refill serves when no count is given.
	DefaultCount = 3
	// MaxCount is the most Tracks one refill serves.
	MaxCount = 10

	randomCandidates   = 300
	favoriteCandidates = 100
	tasteCandidates    = 200
)

var (
	ErrUnknownDJ    = errors.New("no such DJ")
	ErrInvalidCount = fmt.Errorf("the count must be from 1 to %d Tracks", MaxCount)
	ErrNoTracks     = errors.New("no Tracks in the Library to play")
	ErrInvalidTheme = fmt.Errorf("the Theme must be at most %d characters", MaxThemeLength)
)

type Service interface {
	// DJs returns the DJs a Listener can start a Station with.
	DJs() []DJ
	// Next serves the next count Tracks (DefaultCount when 0) of the logged-in Listener's
	// Station with the DJ and the optional Theme, and remembers them so later refills don't
	// repeat them. The Theme only counts with an LLM.
	Next(ctx context.Context, djID, theme string, count int) (model.MediaFiles, error)
}

// New returns the Service. A nil selector means no LLM is configured: selection is rule-based
// and the Theme is ignored.
func New(ds model.DataStore, selector Selector) Service {
	return &service{ds: ds, selector: selector}
}

type service struct {
	ds       model.DataStore
	selector Selector
	plans    plans
	locks    sync.Map // Listener ID -> *sync.Mutex, so one Listener's refills don't race; a household has few
}

func (s *service) DJs() []DJ {
	return DJs()
}

func (s *service) Next(ctx context.Context, djID, theme string, count int) (model.MediaFiles, error) {
	dj, ok := FindDJ(djID)
	if !ok {
		return nil, ErrUnknownDJ
	}
	// Without an LLM the Theme has no say, so it is ignored rather than checked.
	if s.selector == nil {
		theme = ""
	}
	theme, err := cleanTheme(theme)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		count = DefaultCount
	}
	if count < 1 || count > MaxCount {
		return nil, ErrInvalidCount
	}
	usr, ok := request.UserFrom(ctx)
	if !ok {
		return nil, errors.New("a Station needs a logged-in Listener")
	}
	lock, _ := s.locks.LoadOrStore(usr.ID, &sync.Mutex{})
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()

	now := time.Now()
	repo := s.ds.StationTrack(ctx)
	if err := repo.DeleteBefore(now.Add(-RepeatWindow)); err != nil {
		return nil, fmt.Errorf("forgetting old Station Tracks: %w", err)
	}
	served, err := repo.Since(now.Add(-RepeatWindow))
	if err != nil {
		return nil, fmt.Errorf("loading the Station's Tracks: %w", err)
	}
	history := make([]Served, len(served))
	for i, t := range served {
		history[i] = Served{TrackID: t.MediaFileID, Artist: t.Artist, At: t.ServedAt}
	}

	var picked model.MediaFiles
	if s.selector != nil {
		picked = s.plans.take(usr.ID, dj, theme, history, now, count)
		history = append(servedAt(picked, now), history...)
	}
	if len(picked) < count {
		candidates, err := s.candidates(ctx, dj)
		if err != nil {
			return nil, err
		}
		picked = append(picked, Pick(dj, candidates, history, now, count-len(picked), rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())))...)
	}
	if len(picked) == 0 {
		return nil, ErrNoTracks
	}

	records := make([]model.StationTrack, len(picked))
	for i, mf := range picked {
		// Later Tracks count as served later, so the Station's order survives.
		records[i] = model.StationTrack{MediaFileID: mf.ID, Artist: mf.Artist, ServedAt: now.Add(time.Duration(i) * time.Millisecond)}
	}
	if err := repo.Add(records...); err != nil {
		return nil, fmt.Errorf("remembering the Station's Tracks: %w", err)
	}
	if s.selector != nil {
		exclude := make([]string, 0, len(history)+len(picked))
		for _, h := range history {
			exclude = append(exclude, h.TrackID)
		}
		for _, mf := range picked {
			exclude = append(exclude, mf.ID)
		}
		s.plans.selectAhead(ctx, s.selector, usr.ID, dj, theme, exclude)
	}
	return picked, nil
}

// servedAt returns the Tracks as served at now, in order, newest first like a Station's history.
func servedAt(mfs model.MediaFiles, now time.Time) []Served {
	res := make([]Served, len(mfs))
	for i, mf := range mfs {
		res[len(mfs)-1-i] = Served{TrackID: mf.ID, Artist: mf.Artist, At: now.Add(time.Duration(i) * time.Nanosecond)}
	}
	return res
}

// candidates gathers Tracks from the Listener's Library for Pick to choose from: a random
// sample, so the whole Library gets a chance, plus the Listener's favorites and most played
// Tracks, plus Tracks that suit the DJ's Taste. Repeats among them are fine; Pick drops them.
func (s *service) candidates(ctx context.Context, dj DJ) (model.MediaFiles, error) {
	repo := s.ds.MediaFile(ctx)
	present := Eq{"media_file.missing": false}
	var all model.MediaFiles
	add := func(what string, mfs model.MediaFiles, err error) error {
		if err != nil {
			return fmt.Errorf("finding %s for the Station: %w", what, err)
		}
		all = append(all, mfs...)
		return nil
	}

	mfs, err := repo.GetRandom(model.QueryOptions{Filters: present, Max: randomCandidates})
	if err := add("random Tracks", mfs, err); err != nil {
		return nil, err
	}
	mfs, err = repo.GetAll(model.QueryOptions{Sort: "random", Max: favoriteCandidates,
		Filters: And{present, Or{Expr("annotation.starred = true"), GtOrEq{"annotation.rating": 4}}}})
	if err := add("favorites", mfs, err); err != nil {
		return nil, err
	}
	mfs, err = repo.GetAll(model.QueryOptions{Sort: "play_count", Order: "desc", Max: favoriteCandidates,
		Filters: And{present, Gt{"annotation.play_count": 0}}})
	if err := add("most played Tracks", mfs, err); err != nil {
		return nil, err
	}

	t := dj.Taste
	if t.GenreWeight > 0 {
		genres, err := s.matchingGenres(ctx, t.Genres)
		if err != nil {
			return nil, err
		}
		if len(genres) > 0 {
			mfs, err = repo.GetAllByTags(model.TagGenre, genres, model.QueryOptions{Sort: "random", Max: tasteCandidates, Filters: present})
			if err := add("Tracks in the DJ's genres", mfs, err); err != nil {
				return nil, err
			}
		}
	}
	if t.UnplayedWeight > 0 {
		mfs, err = repo.GetAll(model.QueryOptions{Sort: "random", Max: tasteCandidates,
			Filters: And{present, Expr("coalesce(annotation.play_count, 0) = 0")}})
		if err := add("unplayed Tracks", mfs, err); err != nil {
			return nil, err
		}
	}
	if t.OlderThan > 0 {
		mfs, err = repo.GetAll(model.QueryOptions{Sort: "random", Max: tasteCandidates,
			Filters: And{present, Gt{"media_file.year": 0}, Lt{"media_file.year": t.OlderThan}}})
		if err := add("older Tracks", mfs, err); err != nil {
			return nil, err
		}
	}
	return all, nil
}

// matchingGenres returns the Library's genres that contain one of the DJ's genre words.
func (s *service) matchingGenres(ctx context.Context, words []string) ([]string, error) {
	genres, err := s.ds.Genre(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("listing genres for the Station: %w", err)
	}
	var res []string
	for _, g := range genres {
		name := strings.ToLower(g.Name)
		for _, w := range words {
			if strings.Contains(name, w) {
				res = append(res, g.Name)
				break
			}
		}
	}
	return res, nil
}
