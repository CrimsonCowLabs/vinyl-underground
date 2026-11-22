package stations

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
)

// With an LLM, creating or editing a Listener's DJ sets it up (#110), so its Station sounds
// like it from the first Track. Saving maps its genres (deriveGenres, up to genreMapTimeout);
// then, in the background, the DJ's first plan of the LLM's picks is selected from them into
// the Listener's plan for the DJ with no Theme, the one its Station starts from. The whole step
// gets SetupWait from the moment the save began, so the selection gets what the mapping left.
// The form waits for it with AwaitSetup.
//
// When the plan isn't ready by then, or the LLM is down (a selection or a mapping of the
// Listener's failed within retryAfter), the plan gets a starter of starterLength rule-based
// Tracks, on the DJ's genres as rule-based selection plays them, and the setup goes on in the
// background: every setupRetryEvery, for setupRetryFor after the save, it maps the DJ's genres
// again if the LLM hasn't, and has the LLM select into the plan, its picks going after the
// starter. Each try goes through the same selection as a Station's (awaitPicks), so a Listener
// still has one selection at a time, a try waits for one under way, and none is made while
// their last one failed within retryAfter. Editing the DJ again starts a new setup in place of
// this one; deleting it stops the setup and drops its plan. Setups live in memory only.

const (
	// SetupWait is the longest a DJ's setup step takes, from saving it to a plan of the LLM's
	// picks; past it, the DJ plays a starter while the setup goes on in the background.
	SetupWait = 60 * time.Second
	// setupRetryEvery is how often a setup still going asks the LLM again, and setupRetryFor
	// for how long after the save it keeps trying. Past that, the Station-start wait takes
	// over.
	setupRetryEvery = retryAfter
	setupRetryFor   = 30 * time.Minute
	// starterLength is how many rule-based Tracks a DJ whose setup goes on starts with.
	starterLength = 5
)

// SetupStatus is how far a Listener's DJ's setup got.
type SetupStatus string

const (
	// SetupReady means the DJ's plan holds the LLM's picks: its Station starts with them.
	SetupReady SetupStatus = "ready"
	// SetupSettingUp means the DJ plays a starter while the setup goes on in the background.
	SetupSettingUp SetupStatus = "settingUp"
	// SetupNone means the DJ has no setup: there is no LLM, or it ended or was forgotten.
	SetupNone SetupStatus = "none"
)

// setupTimes are a setup's durations: SetupWait, setupRetryEvery and setupRetryFor, but
// shorter in tests.
type setupTimes struct {
	wait, retryEvery, retryFor time.Duration
}

// setups holds each Listener's DJ setups going on.
type setups struct {
	mu    sync.Mutex
	byKey map[planKey]*setup
	wg    sync.WaitGroup // setups going on, for tests
}

// setup is one DJ's setup.
type setup struct {
	done   chan struct{} // closed once the setup step ended: the plan is ready, or has a starter
	ready  bool          // the plan is ready; setups.mu must be held
	cancel context.CancelFunc
}

// begin starts a new setup for the key, stopping any other one for it.
func (ss *setups) begin(key planKey, cancel context.CancelFunc) *setup {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if ss.byKey == nil {
		ss.byKey = map[planKey]*setup{}
	}
	if old := ss.byKey[key]; old != nil {
		old.cancel()
	}
	st := &setup{done: make(chan struct{}), cancel: cancel}
	ss.byKey[key] = st
	return st
}

// stop stops the setup for the key, if any, and forgets it.
func (ss *setups) stop(key planKey) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if st := ss.byKey[key]; st != nil {
		st.cancel()
		delete(ss.byKey, key)
	}
}

// endStep ends the setup step, saying whether the plan is ready.
func (ss *setups) endStep(st *setup, ready bool) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	st.ready = ready
	select {
	case <-st.done:
	default:
		close(st.done)
	}
}

// end ends the setup once it stopped trying. A setup that got the DJ ready is kept, so the
// form asking after it finished learns it is; one that gave up is forgotten, unless another
// took its place.
func (ss *setups) end(key planKey, st *setup) {
	ready := st.isReady(ss)
	ss.endStep(st, ready)
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if !ready && ss.byKey[key] == st {
		delete(ss.byKey, key)
	}
	st.cancel()
}

// isReady reads the setup's ready.
func (st *setup) isReady(ss *setups) bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return st.ready
}

// get returns the setup for the key, if one is going on.
func (ss *setups) get(key planKey) *setup {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.byKey[key]
}

// AwaitSetup waits for the logged-in Listener's DJ's setup step to end, and says how far it
// got.
func (s *service) AwaitSetup(ctx context.Context, id string) (SetupStatus, error) {
	if err := editable(id); err != nil {
		return "", err
	}
	if _, err := s.DJ(ctx, id); err != nil {
		return "", err
	}
	usr, ok := request.UserFrom(ctx)
	if !ok {
		return "", errors.New("a DJ's setup needs a logged-in Listener")
	}
	st := s.setups.get(planKey{usr.ID, id})
	if st == nil {
		return SetupNone, nil
	}
	select {
	case <-st.done:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if st.isReady(&s.setups) {
		return SetupReady, nil
	}
	return SetupSettingUp, nil
}

// startSetup starts setting up the Listener's DJ, saved in a save that began at began. It
// returns at once. Without an LLM there is nothing to set up.
func (s *service) startSetup(ctx context.Context, dj DJ, began time.Time) {
	usr, ok := request.UserFrom(ctx)
	if s.selector == nil || !ok {
		return
	}
	key := planKey{usr.ID, dj.ID}
	// The setup outlives the save's request, keeping its Listener for the Library.
	ctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	st := s.setups.begin(key, cancel)
	s.setups.wg.Add(1)
	go func() {
		defer s.setups.wg.Done()
		defer s.setups.end(key, st)
		s.runSetup(ctx, usr.ID, dj.ID, st, began)
	}()
}

// runSetup runs a DJ's setup: the setup step, then tries in the background until the plan is
// ready, the time is up or ctx is done.
func (s *service) runSetup(ctx context.Context, userID, djID string, st *setup, began time.Time) {
	until := began.Add(s.setupTimes.retryFor)
	dj, p, ok := s.trySetup(ctx, userID, djID, nil, began.Add(s.setupTimes.wait), false)
	if !ok {
		return
	}
	if !s.plans.isReady(p) {
		s.addStarter(ctx, dj, p)
	}
	ready := s.plans.isReady(p)
	s.setups.endStep(st, ready)
	if ready {
		log.Info(ctx, "DJ set up", "dj", djID)
		return
	}
	log.Info(ctx, "DJ still being set up; playing a starter meanwhile", "dj", djID)
	for time.Now().Add(s.setupTimes.retryEvery).Before(until) {
		select {
		case <-time.After(s.setupTimes.retryEvery):
		case <-ctx.Done():
			return
		}
		if dj, p, ok = s.trySetup(ctx, userID, djID, p, until, true); !ok {
			return
		}
		if s.plans.isReady(p) {
			s.setups.endStep(st, true)
			log.Info(ctx, "DJ set up in the background", "dj", djID)
			return
		}
	}
	log.Info(ctx, "Gave up setting up a DJ for now; its Station start will wait for the LLM", "dj", djID)
}

// trySetup has the LLM select into the DJ's plan, waiting up to until for its picks, and
// returns the DJ and its plan. With remap, it first maps the DJ's genres with the LLM when it
// hasn't yet, and carries what the plan from before held into the one for the new genres. It
// doesn't ask the LLM while a selection or a mapping of the Listener's failed within
// retryAfter. ok is false when the DJ is gone or ctx is done.
func (s *service) trySetup(ctx context.Context, userID, djID string, from *plan, until time.Time, remap bool) (DJ, *plan, bool) {
	dj, err := s.DJ(ctx, djID)
	if err != nil {
		if !errors.Is(err, ErrUnknownDJ) {
			log.Warn(ctx, "Setting up a DJ failed", "dj", djID, err)
		}
		return DJ{}, nil, false
	}
	if remap && s.needsMapping(dj) && !s.genresFailedRecently(userID) {
		if dj, err = s.remapGenres(ctx, userID, dj); err != nil {
			log.Warn(ctx, "Mapping a DJ to genres failed", "dj", djID, err)
		}
	}
	if ctx.Err() != nil {
		return DJ{}, nil, false
	}
	in, err := s.stationInput(ctx, dj, "")
	if err != nil {
		log.Warn(ctx, "Setting up a DJ failed", "dj", djID, err)
		return DJ{}, nil, false
	}
	p := s.plans.carryInto(userID, in, from)
	if s.genresFailedRecently(userID) {
		return dj, p, true
	}
	history, err := s.history(ctx, time.Now())
	if err != nil {
		log.Warn(ctx, "Setting up a DJ failed", "dj", djID, err)
		return dj, p, true
	}
	if wait := time.Until(until); wait > 0 {
		if err := s.plans.awaitPicks(ctx, s.selector, userID, in, trackIDs(history), wait, (*plan).ready); err != nil {
			return DJ{}, nil, false
		}
	}
	return dj, p, true
}

// remapGenres maps the DJ's genres with the LLM, once the Listener's selection under way, if
// any, has ended, and stores them unless the DJ was edited meanwhile.
func (s *service) remapGenres(ctx context.Context, userID string, dj DJ) (DJ, error) {
	if done := s.selectionUnderWay(userID); done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return dj, ctx.Err()
		}
	}
	library, err := s.libraryGenres(ctx)
	if err != nil {
		return dj, err
	}
	mapCtx, cancel := context.WithTimeout(ctx, genreMapTimeout)
	defer cancel()
	genres, ok := s.llmGenres(mapCtx, dj.Name, dj.Description, library)
	if !ok {
		return dj, nil
	}
	return s.storeGenres(ctx, dj, genres, model.DJGenresFromLLM)
}

// genresFailedRecently reports whether the Listener's last genre mapping failed within
// retryAfter: the LLM is down for them.
func (s *service) genresFailedRecently(userID string) bool {
	m := s.genreLLM
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.failedRecently(userID, time.Now())
}

// carryInto returns the Listener's plan for the input, holding what the plan from held too,
// when it was another (the DJ's genres were mapped since).
func (ps *plans) carryInto(userID string, in selectionInput, from *plan) *plan {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	p := ps.planFor(userID, in, time.Now())
	if from != nil && from != p {
		p.tracks = appendNew(p.tracks, from.tracks)
		p.picked = p.picked || from.picked
	}
	return p
}

// addStarter gives the DJ's empty plan starterLength rule-based Tracks, as a Station of it
// would play by rule.
func (s *service) addStarter(ctx context.Context, dj DJ, p *plan) {
	now := time.Now()
	history, err := s.history(ctx, now)
	if err == nil {
		var skips []Skip
		if skips, err = s.skips(ctx, dj, now); err == nil {
			var candidates model.MediaFiles
			if candidates, err = s.candidates(ctx, dj); err == nil {
				rnd := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
				starter := Pick(dj, candidates, history, now, starterLength, rnd, SkipWeight(skips, now))
				s.plans.mu.Lock()
				if !p.hasTracks() {
					p.tracks = starter
				}
				s.plans.mu.Unlock()
			}
		}
	}
	if err != nil {
		log.Warn(ctx, "Planning a DJ's starter Tracks failed", "dj", dj.ID, err)
	}
}
