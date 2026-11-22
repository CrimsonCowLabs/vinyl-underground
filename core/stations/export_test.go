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

// Take serves up to n planned Tracks for the Listener's Station at now, with no history.
func (ps *Plans) Take(userID string, dj DJ, theme string, now time.Time, n int) model.MediaFiles {
	return ps.take(userID, dj, theme, nil, now, n, rand.New(rand.NewPCG(1, 2)))
}

// SelectAhead runs a background selection for the Listener's Station and waits for it.
func (ps *Plans) SelectAhead(ctx context.Context, sel Selector, userID string, dj DJ, theme string) {
	ps.selectAhead(ctx, sel, userID, dj, theme, nil)
	ps.wg.Wait()
}

// PickRanked takes the next Tracks from the LLM's ranked picks, as a refill does.
var PickRanked = pickRanked

// WaitForBlurbs waits until the Service has written the Blurbs it queued.
func WaitForBlurbs(s Service) {
	s.(*service).blurbs.wg.Wait()
}
