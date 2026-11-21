package stations

// WaitForSelection waits until the Service's background LLM selections are done.
func WaitForSelection(s Service) {
	s.(*service).plans.wg.Wait()
}

// PickRanked takes the next Tracks from the LLM's ranked picks, as a refill does.
var PickRanked = pickRanked

// WaitForBlurbs waits until the Service has written the Blurbs it queued.
func WaitForBlurbs(s Service) {
	s.(*service).blurbs.wg.Wait()
}
