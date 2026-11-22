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
// their last one failed within retryAfter: a try comes setupRetryEvery after the last one
// ended, and never before the Listener's back-off is over. A selection still running when the
// setup step ran out is waited for first. When the time is up, what is left of the starter is
// dropped, so the Station-start wait takes over. Editing the DJ again starts a new setup in
// place of this one; deleting it stops the setup and drops its plan. Setups live in memory only.

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
// form asking after it finished learns it is (one small entry per DJ, until it is edited
// again or deleted); one that gave up is forgotten, unless another took its place.
func (ss *setups) end(key planKey, st *setup) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	select {
	case <-st.done:
	default:
		close(st.done)
	}
	if !st.ready && ss.byKey[key] == st {
		delete(ss.byKey, key)
	}
	st.cancel()
}

// isReady reports whether the setup got its DJ ready.
func (ss *setups) isReady(st *setup) bool {
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
	if s.setups.isReady(st) {
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
	dj, p, ok := s.trySetup(ctx, userID, djID, nil, began.Add(s.setupTimes.wait))
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
	for {
		// A selection under way, such as the one the setup step stopped waiting for, may
		// still bring the picks; the next try is then due setupRetryEvery after it.
		if done := s.selectionUnderWay(userID); done != nil {
			select {
			case <-done:
			case <-ctx.Done():
				return
			}
		}
		if s.plans.isReady(p) {
			s.setups.endStep(st, true)
			log.Info(ctx, "DJ set up in the background", "dj", djID)
			return
		}
		// Not before the Listener's back-off from a failure ends, whoever's call it was.
		next := time.Now().Add(s.setupTimes.retryEvery)
		if t := s.llmBackAt(userID); t.After(next) {
			next = t
		}
		if !next.Before(until) {
			break
		}
		select {
		case <-time.After(time.Until(next)):
		case <-ctx.Done():
			return
		}
		if dj, p, ok = s.trySetup(ctx, userID, djID, p, until); !ok {
			return
		}
	}
	// The Station-start wait takes over from here, so the starter goes.
	s.plans.dropUnpicked(p)
	log.Info(ctx, "Gave up setting up a DJ for now; its Station start will wait for the LLM", "dj", djID)
}

// trySetup has the LLM select into the DJ's plan, waiting up to until for its picks, and
// returns the DJ and its plan. When the LLM hasn't mapped the DJ's genres yet, it maps them
// first, carrying what the plan from before held into the one for the new genres. It doesn't
// ask the LLM while a selection or a mapping of the Listener's failed within retryAfter. ok is
// false when the DJ is gone or ctx is done.
func (s *service) trySetup(ctx context.Context, userID, djID string, from *plan, until time.Time) (DJ, *plan, bool) {
	dj, err := s.DJ(ctx, djID)
	if err != nil {
		if !errors.Is(err, ErrUnknownDJ) {
			log.Warn(ctx, "Setting up a DJ failed", "dj", djID, err)
		}
		return DJ{}, nil, false
	}
	// Right after a save that mapped them, an answer without the Library's genres is still
	// kept, so this asks nothing again.
	if s.needsMapping(dj) && !s.llmDown(userID) {
		if dj, err = s.mapGenresWhenFree(ctx, userID, dj, genreMapTimeout); err != nil && ctx.Err() == nil {
			log.Warn(ctx, "Mapping a DJ to genres failed", "dj", djID, err)
		}
	}
	in, err := s.stationInput(ctx, dj, "")
	if err != nil {
		log.Warn(ctx, "Setting up a DJ failed", "dj", djID, err)
		return DJ{}, nil, false
	}
	p, ok := s.plans.carryInto(ctx, userID, in, from)
	if !ok || s.llmDown(userID) {
		return dj, p, ok
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

// llmDown reports whether the LLM is down for the Listener: a selection or a genre mapping of
// theirs failed within retryAfter.
func (s *service) llmDown(userID string) bool {
	return s.plans.failedLately(userID) || (s.genreLLM != nil && s.genreLLM.failedLately(userID))
}

// llmBackAt is when the Listener's back-off from their last failed selection or genre mapping
// ends: the zero time when they have none.
func (s *service) llmBackAt(userID string) time.Time {
	at := s.plans.backAt(userID)
	if s.genreLLM != nil {
		if g := s.genreLLM.backAt(userID); g.After(at) {
			at = g
		}
	}
	return at
}

// carryInto returns the Listener's plan for the input, holding what the plan from held too,
// when it was another (the DJ's genres were mapped since). It makes no plan once ctx is done,
// so a setup the DJ's deletion stopped can't bring its plan back.
func (ps *plans) carryInto(ctx context.Context, userID string, in selectionInput, from *plan) (*plan, bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ctx.Err() != nil {
		return nil, false
	}
	p := ps.planFor(userID, in, time.Now())
	if from != nil && from != p {
		p.tracks = appendNew(p.tracks, from.tracks)
		p.picked = p.picked || from.picked
	}
	return p, true
}

// dropUnpicked empties the plan unless the LLM picked for it, dropping a starter nobody played.
func (ps *plans) dropUnpicked(p *plan) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if !p.picked {
		p.tracks = nil
	}
}

// addStarter gives the DJ's empty plan starterLength rule-based Tracks, as a Station of it
// would play by rule, unless ctx is done meanwhile (the DJ was edited again or deleted).
func (s *service) addStarter(ctx context.Context, dj DJ, p *plan) {
	starter, err := s.starter(ctx, dj)
	if err != nil {
		log.Warn(ctx, "Planning a DJ's starter Tracks failed", "dj", dj.ID, err)
		return
	}
	s.plans.mu.Lock()
	defer s.plans.mu.Unlock()
	if ctx.Err() == nil && !p.hasTracks() {
		p.tracks = starter
	}
}

// starter picks starterLength Tracks for the DJ by rule, as a refill of its Station would.
func (s *service) starter(ctx context.Context, dj DJ) (model.MediaFiles, error) {
	now := time.Now()
	history, err := s.history(ctx, now)
	if err != nil {
		return nil, err
	}
	skips, err := s.skips(ctx, dj, now)
	if err != nil {
		return nil, err
	}
	candidates, err := s.candidates(ctx, dj)
	if err != nil {
		return nil, err
	}
	rnd := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	return Pick(dj, candidates, history, now, starterLength, rnd, SkipWeight(skips, now)), nil
}
