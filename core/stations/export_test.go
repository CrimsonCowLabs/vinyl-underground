package stations

// WaitForSelection waits until the Service's background LLM selections are done.
func WaitForSelection(s Service) {
	s.(*service).plans.wg.Wait()
}
