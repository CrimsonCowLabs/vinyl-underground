// Package stations runs Stations: a Listener picks a DJ, built-in or their own, and gets an endless,
// skippable queue of Tracks from their Library, refilled from the server a few Tracks at a
// time. See docs/adr/0005-stations-are-personal-not-live-broadcast.md.
//
// Without an LLM, selection is rule-based. Each refill gathers candidates from the Library (a
// random sample, the Listener's favorites and most played, and Tracks that suit the DJ), and
// Pick chooses among them by the DJ's Taste while keeping Tracks from repeating within
// RepeatWindow and artists within ArtistGap Tracks. With an LLM, the DJ's persona and an
// optional Theme choose the Tracks instead, selected ahead so refills never wait on it (see
// themes.go); rule-based selection fills in until those picks are ready. Only a Station's start
// waits for them, up to StartWait. What each
// Listener's Station served is kept in the station_track table, so Stations are independent
// and survive a restart.
//
// A Station is a Listener's with one DJ. Skipping a Track on it is a mild "less like this":
// Skip records it in the station_skip table, and that Station alone then plays the Track and
// its artist a little less, fading over SkipFade. Skips never touch ratings or favorites.
//
// With an LLM, the DJ also shows a Blurb at each Track change, written ahead in the background
// (see blurbs.go). Without one there are no Blurbs, and the Station plays the same.
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
	"github.com/navidrome/navidrome/log"
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

	// strictGenreTracks is how many Library Tracks must be in a DJ's genres for rule-based
	// selection to play only those. Below it the genres are just a boost, so a DJ never runs
	// dry in a small Library. It counts within the tasteCandidates sample, so it can't exceed it.
	strictGenreTracks = 50
)

var (
	ErrUnknownDJ    = errors.New("no such DJ")
	ErrInvalidCount = fmt.Errorf("the count must be from 1 to %d Tracks", MaxCount)
	ErrNoTracks     = errors.New("no Tracks in the Library to play")
	ErrNotServed    = errors.New("the Station didn't serve that Track lately")
	ErrInvalidTheme = fmt.Errorf("the Theme must be at most %d characters", MaxThemeLength)
)

type Service interface {
	// DJs returns the DJs the logged-in Listener can start a Station with: the built-in ones,
	// then their own by name.
	DJs(ctx context.Context) ([]DJ, error)
	// DJ returns the DJ with the id, built-in or the logged-in Listener's own, or ErrUnknownDJ.
	DJ(ctx context.Context, id string) (DJ, error)
	// CreateDJ creates a DJ for the logged-in Listener from a name and a personality
	// description (ErrInvalidDJName, ErrInvalidDJDescription), with the Library genres the
	// Listener picked by hand, or else the ones its name and description name.
	CreateDJ(ctx context.Context, edit DJEdit) (DJ, error)
	// UpdateDJ changes the name and description of the logged-in Listener's DJ. Its genres
	// become the ones the Listener picked, if they picked any now, or the ones worked out from
	// the name and description when they ask for a new suggestion; otherwise genres picked by
	// hand before stay, and any others are worked out again from the name and description.
	// Built-in DJs are read-only (ErrBuiltInDJ).
	UpdateDJ(ctx context.Context, id string, edit DJEdit) (DJ, error)
	// OpenDJ returns the logged-in Listener's own DJ for the DJ form (ErrBuiltInDJ for a
	// built-in one). With an LLM, genres it hasn't mapped yet and nobody picked by hand are
	// mapped first, waiting a little for it, and stored.
	OpenDJ(ctx context.Context, id string) (DJ, error)
	// Genres returns the genres in the logged-in Listener's Library, by name: the ones their
	// DJs can have.
	Genres(ctx context.Context) ([]string, error)
	// MatchGenres returns the genres saving a DJ with the name and description would give it,
	// unless the Listener picks them by hand, so the DJ form can show them before it is saved.
	MatchGenres(ctx context.Context, name, description string) ([]string, error)
	// AwaitSetup waits for the setup step of the logged-in Listener's DJ, which creating or
	// editing it starts with an LLM (ErrBuiltInDJ for a built-in one), and says how it ended:
	// SetupReady when its plan holds the LLM's picks, SetupSettingUp when it plays a starter
	// while the setup goes on, or SetupNone when no setup is going on (as without an LLM). It
	// waits at most SetupWait from the save.
	AwaitSetup(ctx context.Context, id string) (SetupStatus, error)
	// DeleteDJ deletes the logged-in Listener's DJ (ErrBuiltInDJ for a built-in one), stops its
	// setup and forgets its plan. What its Stations served and played stays in the Listener's
	// history.
	DeleteDJ(ctx context.Context, id string) error
	// Next serves the next count Tracks (DefaultCount when 0) of the logged-in Listener's
	// Station with the DJ and the optional Theme, and remembers them so later refills don't
	// repeat them. The Theme only counts with an LLM.
	Next(ctx context.Context, djID, theme string, count int) (model.MediaFiles, error)
	// Start serves a Station's first Tracks as Next does, but with an LLM and none of its picks
	// planned for the Station, it first waits for them, up to StartWait. Past that, the first
	// Tracks are rule-based and the picks join later refills. It doesn't wait when the
	// Listener's last selection failed within the last couple of minutes.
	Start(ctx context.Context, djID, theme string, count int) (model.MediaFiles, error)
	// Skip records that the logged-in Listener skipped a Track their Station served lately, so
	// their Station with the DJ plays it, and its artist, a little less for a while.
	Skip(ctx context.Context, djID, trackID string) error
	// Blurb returns the Blurb for the change from afterID into trackID on the logged-in
	// Listener's Station with the DJ, if it is written. It never waits for one.
	Blurb(ctx context.Context, djID, trackID, afterID string) (Blurb, bool)
}

// New returns the Service. A nil selector means no LLM is configured: selection is rule-based
// and the Theme is ignored. A nil writer means there are no Blurbs. Without WithGenreLLM, a
// Listener's DJs get the genres their words name.
func New(ds model.DataStore, selector Selector, writer BlurbWriter, opts ...Option) Service {
	s := &service{ds: ds, selector: selector, blurbs: blurbs{writer: writer}, startWait: StartWait,
		setupTimes: setupTimes{wait: SetupWait, retryEvery: setupRetryEvery, retryFor: setupRetryFor}}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

type service struct {
	ds       model.DataStore
	selector Selector
	plans    plans
	blurbs   blurbs
	locks    sync.Map // Listener ID -> *sync.Mutex, so one Listener's refills don't race; a household has few
	// startWait is how long a Station start waits for the LLM's picks: StartWait, but
	// shorter in tests.
	startWait time.Duration
	// genreLLM maps a Listener's DJs to the Library's genres; nil without an LLM.
	genreLLM *genreMapper
	// setups are the Listener's DJs being set up, and setupTimes how long that takes.
	setups     setups
	setupTimes setupTimes
}

// tracksRequest is a checked request for a Station's Tracks, to start it or refill it.
type tracksRequest struct {
	usr   model.User
	dj    DJ
	theme string
	count int
	// input is what the LLM is asked for the Station, when there is one.
	input selectionInput
}

// checkRequest checks a request for count Tracks of the logged-in Listener's Station with the
// DJ and Theme. With an LLM, it also works out what the LLM is asked, which looks up the
// Library genres the DJ's genres name.
func (s *service) checkRequest(ctx context.Context, djID, theme string, count int) (tracksRequest, error) {
	dj, err := s.DJ(ctx, djID)
	if err != nil {
		return tracksRequest{}, err
	}
	if dj, err = s.fillUnsetGenres(ctx, dj); err != nil {
		return tracksRequest{}, err
	}
	// Without an LLM the Theme has no say, so it is ignored rather than checked.
	if s.selector == nil {
		theme = ""
	}
	theme, err = cleanTheme(theme)
	if err != nil {
		return tracksRequest{}, err
	}
	if count == 0 {
		count = DefaultCount
	}
	if count < 1 || count > MaxCount {
		return tracksRequest{}, ErrInvalidCount
	}
	usr, ok := request.UserFrom(ctx)
	if !ok {
		return tracksRequest{}, errors.New("a Station needs a logged-in Listener")
	}
	r := tracksRequest{usr: usr, dj: dj, theme: theme, count: count}
	if s.selector != nil {
		if r.input, err = s.stationInput(ctx, dj, theme); err != nil {
			return tracksRequest{}, err
		}
	}
	return r, nil
}

func (s *service) Start(ctx context.Context, djID, theme string, count int) (model.MediaFiles, error) {
	r, err := s.checkRequest(ctx, djID, theme, count)
	if err != nil {
		return nil, err
	}
	if s.selector != nil {
		// What the Listener's Stations served lately is for the LLM to leave out. The wait
		// holds no Listener lock, so their other requests go on meanwhile.
		history, err := s.history(ctx, time.Now())
		if err != nil {
			return nil, err
		}
		if err := s.plans.awaitPicks(ctx, s.selector, r.usr.ID, r.input, trackIDs(history), s.startWait, (*plan).hasTracks); err != nil {
			return nil, err
		}
	}
	return s.next(ctx, r)
}

func (s *service) Next(ctx context.Context, djID, theme string, count int) (model.MediaFiles, error) {
	r, err := s.checkRequest(ctx, djID, theme, count)
	if err != nil {
		return nil, err
	}
	return s.next(ctx, r)
}

// history returns what the Listener's Stations served since RepeatWindow before now, newest
// first.
func (s *service) history(ctx context.Context, now time.Time) ([]Served, error) {
	served, err := s.ds.StationTrack(ctx).Since(now.Add(-RepeatWindow))
	if err != nil {
		return nil, fmt.Errorf("loading the Station's Tracks: %w", err)
	}
	history := make([]Served, len(served))
	for i, t := range served {
		history[i] = Served{TrackID: t.MediaFileID, Artist: t.Artist, At: t.ServedAt}
	}
	return history, nil
}

// trackIDs returns the IDs of the served Tracks.
func trackIDs(history []Served) []string {
	ids := make([]string, len(history))
	for i, h := range history {
		ids[i] = h.TrackID
	}
	return ids
}

// next serves the checked request.
func (s *service) next(ctx context.Context, r tracksRequest) (model.MediaFiles, error) {
	usr, dj, theme, count := r.usr, r.dj, r.theme, r.count
	defer s.lock(usr.ID)()

	now := time.Now()
	repo := s.ds.StationTrack(ctx)
	if err := repo.DeleteBefore(now.Add(-RepeatWindow)); err != nil {
		return nil, fmt.Errorf("forgetting old Station Tracks: %w", err)
	}
	history, err := s.history(ctx, now)
	if err != nil {
		return nil, err
	}

	skips, err := s.skips(ctx, dj, now)
	if err != nil {
		return nil, err
	}
	skipWeight := SkipWeight(skips, now)
	rnd := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))

	var picked model.MediaFiles
	if s.selector != nil {
		picked = s.plans.take(usr.ID, r.input, history, now, count, rnd, skipWeight)
		history = append(servedAt(picked, now), history...)
	}
	fromPlan := len(picked)
	if len(picked) < count {
		candidates, err := s.candidates(ctx, dj)
		if err != nil {
			return nil, err
		}
		picked = append(picked, Pick(dj, candidates, history, now, count-len(picked), rnd, skipWeight)...)
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
	logRefill(ctx, usr.UserName, dj, picked, fromPlan)
	if s.selector != nil {
		exclude := trackIDs(history)
		for _, mf := range picked {
			exclude = append(exclude, mf.ID)
		}
		s.plans.selectAhead(ctx, s.selector, usr.ID, r.input, exclude)
	}
	s.blurbs.queueAhead(ctx, usr.ID, dj, theme, picked)
	s.mapGenresAhead(ctx, usr.ID, dj)
	return picked, nil
}

func (s *service) Blurb(ctx context.Context, djID, trackID, afterID string) (Blurb, bool) {
	usr, ok := request.UserFrom(ctx)
	if !ok {
		return Blurb{}, false
	}
	return s.blurbs.get(usr.ID, djID, trackID, afterID)
}

func (s *service) Skip(ctx context.Context, djID, trackID string) error {
	if _, err := s.DJ(ctx, djID); err != nil {
		return err
	}
	usr, ok := request.UserFrom(ctx)
	if !ok {
		return errors.New("a Station needs a logged-in Listener")
	}
	defer s.lock(usr.ID)()

	now := time.Now()
	served, err := s.ds.StationTrack(ctx).Since(now.Add(-RepeatWindow))
	if err != nil {
		return fmt.Errorf("loading the Station's Tracks: %w", err)
	}
	for _, t := range served {
		if t.MediaFileID == trackID {
			skip := model.StationSkip{DJID: djID, MediaFileID: trackID, Artist: t.Artist, SkippedAt: now}
			if err := s.ds.StationSkip(ctx).Add(skip); err != nil {
				return fmt.Errorf("remembering the skip: %w", err)
			}
			return nil
		}
	}
	return ErrNotServed
}

// lock keeps one Listener's refills and skips from racing, and returns the unlock.
func (s *service) lock(userID string) func() {
	lock, _ := s.locks.LoadOrStore(userID, &sync.Mutex{})
	lock.(*sync.Mutex).Lock()
	return lock.(*sync.Mutex).Unlock
}

// skips forgets the Listener's skips older than SkipFade, and returns the rest on their Station
// with the DJ.
func (s *service) skips(ctx context.Context, dj DJ, now time.Time) ([]Skip, error) {
	repo := s.ds.StationSkip(ctx)
	if err := repo.DeleteBefore(now.Add(-SkipFade)); err != nil {
		return nil, fmt.Errorf("forgetting old skips: %w", err)
	}
	skipped, err := repo.Since(dj.ID, now.Add(-SkipFade))
	if err != nil {
		return nil, fmt.Errorf("loading the Station's skips: %w", err)
	}
	res := make([]Skip, len(skipped))
	for i, sk := range skipped {
		res[i] = Skip{TrackID: sk.MediaFileID, Artist: sk.Artist, At: sk.SkippedAt}
	}
	return res, nil
}

// logRefill logs what a refill served and where each Track came from: the first fromPlan from
// the LLM's plan, the rest from rule-based selection. It is how to tell, from the logs, why a
// DJ played what it did.
func logRefill(ctx context.Context, listener string, dj DJ, picked model.MediaFiles, fromPlan int) {
	tracks := make([]string, len(picked))
	for i, mf := range picked {
		source := "plan"
		if i >= fromPlan {
			source = "rule"
		}
		tracks[i] = fmt.Sprintf("%s: %s - %s", source, mf.Artist, mf.Title)
	}
	log.Info(ctx, "Station refill", "listener", listener, "dj", dj.ID,
		"planned", fromPlan, "byRule", len(picked)-fromPlan, "tracks", tracks)
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
// When strictGenreTracks or more Library Tracks are in the DJ's genres, only those are left.
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
	strict := false
	if t.GenreWeight > 0 {
		genres, err := s.matchingGenres(ctx, t)
		if err != nil {
			return nil, err
		}
		if len(genres) > 0 {
			mfs, err = repo.GetAllByTags(model.TagGenre, genres, model.QueryOptions{Sort: "random", Max: tasteCandidates, Filters: present})
			if err := add("Tracks in the DJ's genres", mfs, err); err != nil {
				return nil, err
			}
			// The sample holds every Track in the genres up to tasteCandidates, so it counts them.
			strict = len(mfs) >= strictGenreTracks
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
	if strict {
		inGenres := all[:0]
		for _, mf := range all {
			if t.likesGenre(mf) {
				inGenres = append(inGenres, mf)
			}
		}
		all = inGenres
	}
	return all, nil
}

// matchingGenres returns the Library's genres that the DJ's genre words name.
func (s *service) matchingGenres(ctx context.Context, t Taste) ([]string, error) {
	genres, err := s.libraryGenres(ctx)
	if err != nil {
		return nil, err
	}
	var res []string
	for _, g := range genres {
		name := strings.ToLower(g)
		for _, w := range t.Genres {
			if t.namesGenre(w, name) {
				res = append(res, g)
				break
			}
		}
	}
	return res, nil
}
