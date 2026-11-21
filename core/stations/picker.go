package stations

import (
	"math/rand/v2"
	"strings"
	"time"

	"github.com/navidrome/navidrome/model"
)

const (
	// RepeatWindow is how long a Track stays off a Station after it was served or played.
	RepeatWindow = 48 * time.Hour
	// ArtistGap is how many Tracks must pass before an artist comes back on a Station.
	ArtistGap = 5
)

// Pick chooses the next n Tracks of a Station from candidates, in play order. history is what
// the Station already served, newest first.
//
// Tracks the Station served, or the Listener played, within RepeatWindow are left out, and no
// artist comes back within ArtistGap Tracks. Among the rest, each Track's chance is its score
// for the DJ (see Taste). When a Library is too small for those rules, the artist rule goes
// first, and then the Track heard longest ago plays, so a Station never runs dry while it has
// any candidates at all. Each of weights, if any, scales every Track's chance (see SkipWeight).
func Pick(dj DJ, candidates model.MediaFiles, history []Served, now time.Time, n int, rnd *rand.Rand, weights ...Weight) model.MediaFiles {
	p := newPicker(dj, candidates, history)
	p.weights = weights
	var picked model.MediaFiles
	for len(picked) < n && len(p.pool) > 0 {
		mf := p.next(now, rnd)
		picked = append(picked, mf)
		// Later picks in this batch count as served after this one.
		p.served(mf, now.Add(time.Duration(len(picked))*time.Nanosecond))
	}
	return picked
}

type picker struct {
	dj         DJ
	pool       model.MediaFiles
	lastHeard  map[string]time.Time
	artists    []string           // the artists of the last served Tracks, oldest first
	affinity   map[string]float64 // per artist, the sum of liking over their candidate Tracks
	bestArtist float64            // the highest affinity
	maxPlays   float64            // plays of the most played candidate
	weights    []Weight           // scale each Track's score
}

func newPicker(dj DJ, candidates model.MediaFiles, history []Served) *picker {
	p := &picker{dj: dj, lastHeard: map[string]time.Time{}}
	seen := map[string]bool{}
	for _, c := range candidates {
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		p.pool = append(p.pool, c)
		if c.PlayDate != nil {
			p.lastHeard[c.ID] = *c.PlayDate
		}
	}
	for i := len(history) - 1; i >= 0; i-- {
		s := history[i]
		if s.At.After(p.lastHeard[s.TrackID]) {
			p.lastHeard[s.TrackID] = s.At
		}
		p.artists = append(p.artists, artistKey(s.Artist))
	}
	p.affinity = map[string]float64{}
	for _, mf := range p.pool {
		p.maxPlays = max(p.maxPlays, plays(mf.PlayCount))
		a := artistKey(mf.Artist)
		p.affinity[a] += liking(mf)
		p.bestArtist = max(p.bestArtist, p.affinity[a])
	}
	return p
}

func (p *picker) served(mf model.MediaFile, at time.Time) {
	p.lastHeard[mf.ID] = at
	p.artists = append(p.artists, artistKey(mf.Artist))
}

func (p *picker) next(now time.Time, rnd *rand.Rand) model.MediaFile {
	recentArtists := map[string]bool{}
	for _, a := range p.artists[max(0, len(p.artists)-ArtistGap):] {
		recentArtists[a] = true
	}
	fresh := func(mf model.MediaFile) bool {
		heard, ok := p.lastHeard[mf.ID]
		return !ok || now.Sub(heard) >= RepeatWindow
	}
	if mf, ok := p.weighted(rnd, func(mf model.MediaFile) bool {
		return fresh(mf) && !recentArtists[artistKey(mf.Artist)]
	}); ok {
		return mf
	}
	if mf, ok := p.weighted(rnd, fresh); ok {
		return mf
	}
	oldest := p.pool[0]
	for _, mf := range p.pool[1:] {
		if p.lastHeard[mf.ID].Before(p.lastHeard[oldest.ID]) {
			oldest = mf
		}
	}
	return oldest
}

// weighted picks one of the Tracks that pass ok, each with a chance proportional to its score.
func (p *picker) weighted(rnd *rand.Rand, ok func(model.MediaFile) bool) (model.MediaFile, bool) {
	var eligible model.MediaFiles
	var scores []float64
	total := 0.0
	for _, mf := range p.pool {
		if !ok(mf) {
			continue
		}
		history := 0.0
		if p.maxPlays > 0 {
			history = plays(mf.PlayCount) / p.maxPlays
		}
		s := p.dj.Taste.score(mf, history, p.artistAffinity(mf))
		for _, w := range p.weights {
			s *= w(mf)
		}
		eligible = append(eligible, mf)
		scores = append(scores, s)
		total += s
	}
	if len(eligible) == 0 {
		return model.MediaFile{}, false
	}
	r := rnd.Float64() * total
	for i, s := range scores {
		if r < s {
			return eligible[i], true
		}
		r -= s
	}
	return eligible[len(eligible)-1], true
}

func artistKey(artist string) string {
	return strings.ToLower(strings.TrimSpace(artist))
}

// artistAffinity says how much the Listener likes mf's artist, from 0 to 1: the favorites and
// plays of the artist's other candidate Tracks, compared to the best-liked artist's. Leaving mf
// out means a much-played hit doesn't count as a reason to play itself.
func (p *picker) artistAffinity(mf model.MediaFile) float64 {
	if p.bestArtist == 0 {
		return 0
	}
	return (p.affinity[artistKey(mf.Artist)] - liking(mf)) / p.bestArtist
}

// liking is how much the Listener shows they like mf by starring, rating and playing it.
func liking(mf model.MediaFile) float64 {
	return 2*favorite(mf) + plays(mf.PlayCount)
}
