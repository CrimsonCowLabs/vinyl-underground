package stations

import (
	"context"
	"fmt"
	"math/rand/v2"
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
// picks in the LLM's order, keeping Pick's rules about repeats.
//
// The LLM is slow (tens of seconds), so its picks are made ahead, in the background, into a
// plan per Listener. A refill only ever takes from the plan and never waits for the LLM: when
// the plan is short, as when a Station starts, rule-based selection tops it up.

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
	// retryAfter is how long a Station plays by rule after its selection failed (say, the LLM's
	// host is asleep) before asking the LLM again.
	retryAfter = 2 * time.Minute
)

// Selector picks up to length Tracks from the Listener's Library for a free-text request, best
// first, never one with an ID in exclude. generatedplaylist.Selector implements it.
type Selector interface {
	Select(ctx context.Context, request string, length int, exclude []string) (model.MediaFiles, error)
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

// plan is the LLM's picks for one Listener's Station, not yet served.
type plan struct {
	// request is what the LLM was asked, so editing a DJ's persona starts a new plan too.
	request  string
	tracks   model.MediaFiles
	failedAt time.Time
}

// plans holds each Listener's plan. Only the Station they last listened to has one.
type plans struct {
	mu         sync.Mutex
	byListener map[string]*plan
	// selecting holds the Listeners with a selection under way. It is per Listener, not per
	// plan, so switching Station never queues a second slow LLM call behind the first.
	selecting map[string]bool
	wg        sync.WaitGroup // background selections, for tests
}

// take serves up to n planned Tracks for the Listener's Station with the DJ and Theme, keeping
// Pick's rules against history. A different Station from last time, or the same DJ since edited,
// starts a new, empty plan.
func (ps *plans) take(userID string, dj DJ, theme string, history []Served, now time.Time, n int, rnd *rand.Rand, weights ...Weight) model.MediaFiles {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.byListener == nil {
		ps.byListener = map[string]*plan{}
		ps.selecting = map[string]bool{}
	}
	p := ps.byListener[userID]
	if req := stationRequest(dj, theme); p == nil || p.request != req {
		p = &plan{request: req}
		ps.byListener[userID] = p
	}
	picked, rest := pickRanked(p.tracks, history, now, n, rnd, weights...)
	p.tracks = rest
	return picked
}

// selectAhead starts a background selection for the Listener's Station when its plan runs low
// and none is under way. exclude is what the Station served lately, which the LLM shouldn't
// be offered again. It returns at once.
func (ps *plans) selectAhead(ctx context.Context, sel Selector, userID string, dj DJ, theme string, exclude []string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	p := ps.byListener[userID]
	if p == nil || ps.selecting[userID] || len(p.tracks) >= planLowWater || time.Since(p.failedAt) < retryAfter {
		return
	}
	for _, mf := range p.tracks {
		exclude = append(exclude, mf.ID)
	}
	ps.selecting[userID] = true
	ps.wg.Add(1)
	// The selection outlives the refill's request, keeping its Listener for the Library.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), selectionTimeout)
	go func() {
		defer ps.wg.Done()
		defer cancel()
		tracks, err := sel.Select(ctx, p.request, planLength, exclude)

		ps.mu.Lock()
		defer ps.mu.Unlock()
		delete(ps.selecting, userID)
		if err != nil {
			p.failedAt = time.Now()
			log.Warn(ctx, "Station selection failed; playing by rule for now", "dj", dj.ID, "theme", theme, err)
			return
		}
		log.Debug(ctx, "Station selection ready", "dj", dj.ID, "theme", theme, "tracks", len(tracks))
		// The plan may have been replaced meanwhile (another Station); then this one is dropped.
		p.tracks = appendNew(p.tracks, tracks)
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
