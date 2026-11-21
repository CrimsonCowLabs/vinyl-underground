package generatedplaylist

import "time"

// SetSimilarWait shortens how long a Preview waits for similar artists, and returns a func that
// restores it.
func SetSimilarWait(d time.Duration) func() {
	old := similarWait
	similarWait = d
	return func() { similarWait = old }
}
