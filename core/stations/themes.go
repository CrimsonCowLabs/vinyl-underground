package stations

import (
	"cmp"
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// With an LLM, the DJ's persona and the Theme shape a Station the way a Prompt shapes a
// Generated Playlist (ADR-0004): they become the request to a Selector, which turns it into
// filters, builds candidates from the Library and has the LLM rank them. A Station plays those
// picks in the LLM's order, keeping Pick's rules about repeats. When the DJ has genres, only
// Tracks in them are candidates: the Theme can narrow them but never widen them (#108).
//
// The LLM is slow (tens of seconds), so its picks are made ahead, in the background, into a
// plan per Listener and DJ. A refill only ever takes from the plan and never waits for the LLM:
// when the plan is short, rule-based selection tops it up. Only a Station's start waits, up to
// StartWait, when its plan is empty, so a Station sounds like its DJ from the first Track.
// Creating or editing a Listener's DJ plans its first picks up front (see dj_setup.go), so its
// first start needn't wait. Plans live in memory only, so a restart loses them.

const (
	// MaxThemeLength is the longest Theme accepted, in characters.
	MaxThemeLength = 200

	// planLength is how many Tracks one selection asks the LLM for: about an hour of music.
	planLength = 15
	// planLowWater is how few planned Tracks start the next selection, so a new plan is ready
	// before this one runs out.
	planLowWater = 8
	// selectionTimeout is a backstop for one background selection (two LLM calls, each
	// already bounded by the LLM's own timeout), so a stuck one never blocks the next forever.
	selectionTimeout = 10 * time.Minute
	// retryAfter is how long a Listener's Stations play by rule after a selection failed (say,
	// the LLM's host is asleep) before asking the LLM again. A Station start doesn't wait for
	// the LLM meanwhile either, so a sleeping LLM host doesn't cost StartWait on every start.
	retryAfter = 2 * time.Minute
	// StartWait is the longest a Station start waits for the LLM's picks when none are
	// planned. Past it, the Station starts by rule and the picks join later refills.
	StartWait = 60 * time.Second
	// planIdle is how long a plan is kept unused before it is dropped, so memory stays bounded
	// while a Station left for the evening still has its picks the next day.
	planIdle = 24 * time.Hour
)

// Selector picks up to length Tracks from the Listener's Library for a free-text request, best
// first, never one with an ID in exclude and, when genres (the Library's exact genre names) are
// given, only ones tagged with one of them. generatedplaylist.Selector implements it.
type Selector interface {
	Select(ctx context.Context, request string, genres []string, length int, exclude []string) (model.MediaFiles, error)
}

// cleanTheme trims the Theme and checks its length. Empty means no Theme.
func cleanTheme(theme string) (string, error) {
	theme = strings.Join(strings.Fields(theme), " ")
	if utf8.RuneCountInString(theme) > MaxThemeLength {
		return "", ErrInvalidTheme
	}
	return theme, nil
}

// stationRequest is what the LLM is asked for: music suiting the DJ's persona and the Theme.
func stationRequest(dj DJ, theme string) string {
	req := fmt.Sprintf("Music for a Station hosted by the DJ %s. %s", dj.Name, dj.Description)
	if theme != "" {
		req += "\nTheme: " + theme
	}
	return req
}

// selectionInput is what a Station's selection asks the Selector for: the request, from the
// DJ's persona and the Theme, within the Library genres the DJ's genres name (none for a DJ
// without genres).
type selectionInput struct {
	dj      DJ
	theme   string
	request string
	genres  []string
}

// stationInput is the selection input for the Listener's Station with the DJ and Theme. A DJ
// whose genres name none of the Library's selects from the whole Library, as one without
// genres does.
func (s *service) stationInput(ctx context.Context, dj DJ, theme string) (selectionInput, error) {
	in := selectionInput{dj: dj, theme: theme, request: stationRequest(dj, theme)}
	if len(dj.Taste.Genres) > 0 {
		genres, err := s.matchingGenres(ctx, dj.Taste)
		if err != nil {
			return selectionInput{}, err
		}
		in.genres = genres
	}
	return in, nil
}

// plan is the LLM's picks for one Listener's Station with one DJ, not yet served.
type plan struct {
	// input is what the Selector was asked, so editing the DJ's persona or genres, or changing
	// the Theme, starts a new plan too.
	input  selectionInput
	tracks model.MediaFiles
	usedAt time.Time
	// picked is set once a selection added the LLM's picks to it, so a DJ's setup can tell
	// them from the rule-based starter Tracks it may hold (see dj_setup.go).
	picked bool
}

// hasTracks reports whether the plan has Tracks to serve, picked by the LLM or not.
func (p *plan) hasTracks() bool {
	return len(p.tracks) > 0
}

// ready reports whether the plan has Tracks to serve and the LLM picked for it: a DJ set up.
func (p *plan) ready() bool {
	return p.picked && len(p.tracks) > 0
}

// isFor reports whether the plan was made for the input: the DJ's current persona and genres,
// and the Theme.
func (p *plan) isFor(in selectionInput) bool {
	return p.input.request == in.request && slices.Equal(p.input.genres, in.genres)
}

// planKey names a plan: each Listener has one per DJ, so switching Station keeps the picks.
type planKey struct {
	userID, djID string
}

// plans holds each Listener's plan per DJ.
type plans struct {
	mu    sync.Mutex
	byKey map[planKey]*plan
	// selecting holds the Listeners with a selection under way. It is per Listener, not per
	// plan, so switching Station never queues a second slow LLM call behind the first.
	selecting map[string]*selection
	// failedAt is when each Listener's last selection failed, if it did and none succeeded
	// since. It is per Listener: when the LLM is down, it is down for all their DJs.
	failedAt map[string]time.Time
	// backoff is how long after a failure a Listener's selections wait: retryAfter, but
	// shorter in tests.
	backoff time.Duration
	wg      sync.WaitGroup // background selections, for tests
}

// selection is a background selection under way into a plan.
type selection struct {
	plan *plan
	done chan struct{} // closed when it ends
}

// planFor returns the Listener's plan for the input's DJ, starting a new, empty one when there
// is none or it was made for another input (the DJ's persona or genres, or the Theme, changed
// since), and marks it used at now. ps.mu must be held.
func (ps *plans) planFor(userID string, in selectionInput, now time.Time) *plan {
	if ps.byKey == nil {
		ps.byKey = map[planKey]*plan{}
		ps.selecting = map[string]*selection{}
		ps.failedAt = map[string]time.Time{}
	}
	key := planKey{userID, in.dj.ID}
	p := ps.byKey[key]
	if p == nil || !p.isFor(in) {
		p = &plan{input: in}
		ps.byKey[key] = p
	}
	p.usedAt = now
	return p
}

// drop forgets the Listener's plan for the DJ, as when they delete it. A selection under way
// for it still ends, its picks going with the plan.
func (ps *plans) drop(userID, djID string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	delete(ps.byKey, planKey{userID, djID})
}

// isReady reports whether the plan is ready (see plan.ready).
func (ps *plans) isReady(p *plan) bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return p.ready()
}

// dropIdle forgets every plan unused for planIdle, so memory stays bounded. A household has
// a few Listeners and DJs, so a sweep is cheap. ps.mu must be held.
func (ps *plans) dropIdle(now time.Time) {
	for k, p := range ps.byKey {
		if now.Sub(p.usedAt) > planIdle {
			delete(ps.byKey, k)
		}
	}
}

// take serves up to n planned Tracks for the Listener's Station with the input, keeping Pick's
// rules against history.
func (ps *plans) take(userID string, in selectionInput, history []Served, now time.Time, n int, rnd *rand.Rand, weights ...Weight) model.MediaFiles {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.dropIdle(now)
	p := ps.planFor(userID, in, now)
	picked, rest := pickRanked(p.tracks, history, now, n, rnd, weights...)
	p.tracks = rest
	return picked
}

// selectAhead starts a background selection for the Listener's Station when its plan runs low
// and none is under way. exclude is what the Station served lately, which the LLM shouldn't
// be offered again. It returns at once.
func (ps *plans) selectAhead(ctx context.Context, sel Selector, userID string, in selectionInput, exclude []string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	p := ps.byKey[planKey{userID, in.dj.ID}]
	if p == nil || !p.isFor(in) || len(p.tracks) >= planLowWater {
		return
	}
	ps.startSelection(ctx, sel, userID, p, exclude)
}

// awaitPicks waits, up to wait, until the Listener's plan for the input is done waiting for:
// until it has Tracks (plan.hasTracks), as a Station start does, or the LLM's (plan.ready), as
// a DJ's setup does. It starts the selection itself if none is under way, and when one is
// under way for another of the Listener's Stations, it starts its own once that one ends. It
// returns at once when the Listener's last selection failed within retryAfter, and early
// when a selection fails meanwhile or ctx is done (then with ctx's error).
func (ps *plans) awaitPicks(ctx context.Context, sel Selector, userID string, in selectionInput, exclude []string, wait time.Duration, done func(*plan) bool) error {
	timeout := time.NewTimer(wait)
	defer timeout.Stop()
	for {
		ps.mu.Lock()
		p := ps.planFor(userID, in, time.Now())
		if done(p) || ps.failedRecently(userID) {
			ps.mu.Unlock()
			return nil
		}
		ps.startSelection(ctx, sel, userID, p, exclude)
		running := ps.selecting[userID]
		ps.mu.Unlock()

		select {
		case <-running.done:
			// This plan's own selection ended, so its picks, if any, are in. When another
			// plan's ended, this one's starts next time round.
			if running.plan == p {
				return nil
			}
		case <-timeout.C:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// failedRecently reports whether the Listener's last selection failed within retryAfter.
// ps.mu must be held.
func (ps *plans) failedRecently(userID string) bool {
	at, ok := ps.failedAt[userID]
	return ok && time.Since(at) < cmp.Or(ps.backoff, retryAfter)
}

// failedLately reports whether the Listener's last selection failed within retryAfter.
func (ps *plans) failedLately(userID string) bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.failedRecently(userID)
}

// backAt is when the Listener's back-off from their last failed selection ends: the zero time
// when none failed since one succeeded.
func (ps *plans) backAt(userID string) time.Time {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	at, ok := ps.failedAt[userID]
	if !ok {
		return time.Time{}
	}
	return at.Add(cmp.Or(ps.backoff, retryAfter))
}

// startSelection starts a background selection into the plan, unless one is under way for
// the Listener or their last one failed recently. ps.mu must be held.
func (ps *plans) startSelection(ctx context.Context, sel Selector, userID string, p *plan, exclude []string) {
	if ps.selecting[userID] != nil || ps.failedRecently(userID) {
		return
	}
	for _, mf := range p.tracks {
		exclude = append(exclude, mf.ID)
	}
	running := &selection{plan: p, done: make(chan struct{})}
	ps.selecting[userID] = running
	ps.wg.Add(1)
	// The selection outlives the refill's request, keeping its Listener for the Library.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), selectionTimeout)
	go func() {
		defer ps.wg.Done()
		defer cancel()
		in := p.input
		tracks, err := sel.Select(ctx, in.request, in.genres, planLength, exclude)

		ps.mu.Lock()
		defer ps.mu.Unlock()
		delete(ps.selecting, userID)
		defer close(running.done)
		if err != nil {
			ps.failedAt[userID] = time.Now()
			log.Warn(ctx, "Station selection failed; playing by rule for now", "dj", in.dj.ID, "theme", in.theme, "genres", in.genres, err)
			return
		}
		delete(ps.failedAt, userID)
		log.Debug(ctx, "Station selection ready", "dj", in.dj.ID, "theme", in.theme, "tracks", len(tracks))
		// The plan may have been replaced (an edited DJ or a new Theme) or dropped meanwhile;
		// then these picks go with it.
		p.tracks = appendNew(p.tracks, tracks)
		p.picked = p.picked || len(tracks) > 0
	}()
}

// appendNew appends the Tracks of more that aren't in tracks already.
func appendNew(tracks, more model.MediaFiles) model.MediaFiles {
	have := make(map[string]bool, len(tracks))
	for _, mf := range tracks {
		have[mf.ID] = true
	}
	for _, mf := range more {
		if !have[mf.ID] {
			have[mf.ID] = true
			tracks = append(tracks, mf)
		}
	}
	return tracks
}
