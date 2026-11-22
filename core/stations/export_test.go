package stations

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/navidrome/navidrome/model"
)

// WaitForSelection waits until the Service's background LLM selections are done.
func WaitForSelection(s Service) {
	s.(*service).plans.wg.Wait()
}

// Plans holds the LLM's picks per Listener and DJ, as a Service does.
type Plans = plans

// PlanIdle is how long a plan is kept unused.
const PlanIdle = planIdle

// testInput is the selection input for the Station with the DJ and Theme, in a Library with none of the
// DJ's genres.
func testInput(dj DJ, theme string) selectionInput {
	return selectionInput{dj: dj, theme: theme, request: stationRequest(dj, theme)}
}

// Take serves up to n planned Tracks for the Listener's Station at now, with no history.
func (ps *Plans) Take(userID string, dj DJ, theme string, now time.Time, n int) model.MediaFiles {
	return ps.take(userID, testInput(dj, theme), nil, now, n, rand.New(rand.NewPCG(1, 2)))
}

// SelectAhead runs a background selection for the Listener's Station and waits for it.
func (ps *Plans) SelectAhead(ctx context.Context, sel Selector, userID string, dj DJ, theme string) {
	ps.selectAhead(ctx, sel, userID, testInput(dj, theme), nil)
	ps.wg.Wait()
}

// PickRanked takes the next Tracks from the LLM's ranked picks, as a refill does.
var PickRanked = pickRanked

// WaitForBlurbs waits until the Service has written the Blurbs it queued.
func WaitForBlurbs(s Service) {
	s.(*service).blurbs.wg.Wait()
}

// WaitForGenres waits until the Service's background genre mappings are done.
func WaitForGenres(s Service) {
	if m := s.(*service).genreLLM; m != nil {
		m.wg.Wait()
	}
}

// ForgetGenreFailures has the Service ask the LLM for genres again at once after it failed.
func ForgetGenreFailures(s Service) {
	m := s.(*service).genreLLM
	m.mu.Lock()
	defer m.mu.Unlock()
	clear(m.failedAt)
}

// SetStartWait sets how long a Station start waits for the LLM's picks.
func SetStartWait(s Service, d time.Duration) {
	s.(*service).startWait = d
}
