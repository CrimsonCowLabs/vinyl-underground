package stations

// WaitForSelection waits until the Service's background LLM selections are done.
func WaitForSelection(s Service) {
	s.(*service).plans.wg.Wait()
}

// PickRanked takes the next Tracks from the LLM's ranked picks, as a refill does.
var PickRanked = pickRanked
