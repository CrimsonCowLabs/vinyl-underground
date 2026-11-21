package stations

import (
	"math/rand/v2"
	"time"

	"github.com/navidrome/navidrome/model"
)

// pickRanked takes the next n Tracks from ranked (the LLM's picks, best first), in its order.
// It keeps Pick's rules: a Track the Station served or the Listener played within RepeatWindow
// is dropped, and one whose artist played within the last ArtistGap Tracks waits for a later
// refill. Weights, such as the Station's skips, make a pick less likely: one is kept with
// a chance of its weight when its turn comes, and otherwise dropped, so the LLM's order still
// holds for the rest. It returns the Tracks picked and those left for later, in order. It may
// pick fewer than n: the caller tops them up by rule.
func pickRanked(ranked model.MediaFiles, history []Served, now time.Time, n int, rnd *rand.Rand, weights ...Weight) (picked, rest model.MediaFiles) {
	p := newPicker(DJ{}, ranked, history)
	for _, mf := range p.pool {
		if heard, ok := p.lastHeard[mf.ID]; ok && now.Sub(heard) < RepeatWindow {
			continue
		}
		if len(picked) == n || p.recentArtist(mf.Artist) {
			rest = append(rest, mf)
			continue
		}
		if !keep(mf, rnd, weights) {
			continue
		}
		picked = append(picked, mf)
		p.served(mf, now.Add(time.Duration(len(picked))*time.Nanosecond))
	}
	return picked, rest
}

// keep decides by chance whether a pick with the weights plays.
func keep(mf model.MediaFile, rnd *rand.Rand, weights []Weight) bool {
	w := 1.0
	for _, weight := range weights {
		w *= weight(mf)
	}
	return w >= 1 || rnd.Float64() < w
}

// recentArtist reports whether the artist played within the last ArtistGap Tracks.
func (p *picker) recentArtist(artist string) bool {
	key := artistKey(artist)
	for _, a := range p.artists[max(0, len(p.artists)-ArtistGap):] {
		if a == key {
			return true
		}
	}
	return false
}
