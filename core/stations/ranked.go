package stations

import (
	"time"

	"github.com/navidrome/navidrome/model"
)

// pickRanked takes the next n Tracks from ranked (the LLM's picks, best first), in its order.
// It keeps Pick's rules: a Track the Station served or the Listener played within RepeatWindow
// is dropped, and one whose artist played within the last ArtistGap Tracks waits for a later
// refill. It returns the Tracks picked and those left for later, in order. It may pick fewer
// than n: the caller tops them up by rule.
func pickRanked(ranked model.MediaFiles, history []Served, now time.Time, n int) (picked, rest model.MediaFiles) {
	p := newPicker(DJ{}, ranked, history)
	for _, mf := range p.pool {
		if heard, ok := p.lastHeard[mf.ID]; ok && now.Sub(heard) < RepeatWindow {
			continue
		}
		if len(picked) == n || p.recentArtist(mf.Artist) {
			rest = append(rest, mf)
			continue
		}
		picked = append(picked, mf)
		p.served(mf, now.Add(time.Duration(len(picked))*time.Nanosecond))
	}
	return picked, rest
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
