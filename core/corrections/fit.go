package corrections

import (
	"cmp"
	"context"
	"math"

	"github.com/navidrome/navidrome/core/musicbrainz"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// Find on MusicBrainz lists the releases of a search, and several editions of an album often
// look the same there (a GB CD, another GB CD...). FitReleases tells the admin which of them
// fits the album's Files best: how many of the album's Tracks pair with each (as the dialog
// pairs them, see matchRelease), and how close the paired Tracks' durations are to the release
// tracks' lengths.

// ReleaseFit is how well a release fits the album's Files.
type ReleaseFit struct {
	ID string `json:"id"`
	// Paired is how many of the album's Tracks pair with a track of the release.
	Paired int `json:"paired"`
	// TrackCount is the release's number of tracks, AlbumTracks the album's number of Tracks.
	TrackCount  int `json:"trackCount"`
	AlbumTracks int `json:"albumTracks"`
	// LengthDiff is the mean difference, in seconds, between the paired Tracks' durations and
	// their release tracks' lengths, over those whose both are known; nil when none is.
	LengthDiff *float64 `json:"lengthDiff"`
	// Best marks the release that fits best (see MarkBestFit).
	Best bool `json:"best"`
	// Official says the release's status is Official, which breaks a tie.
	Official bool `json:"-"`
}

// FitReleases scores each release against the album's Tracks, in the order given (the search's),
// and marks the best fit (MarkBestFit). A release that can't be paired (it lacks its MusicBrainz
// IDs) is left out; an album that can't be had is an error.
func (c *corrections) FitReleases(ctx context.Context, albumID string, rels []*musicbrainz.Release) ([]ReleaseFit, error) {
	fits := make([]ReleaseFit, 0, len(rels))
	for _, rel := range rels {
		if !isMbzID(rel.ID) || !isMbzID(rel.ReleaseGroup.ID) {
			log.Debug(ctx, "Find on MusicBrainz: a release without its MusicBrainz IDs is left unscored", "release", rel.ID)
			continue
		}
		m, e, err := c.matchRelease(ctx, albumID, rel)
		if err != nil {
			return nil, err
		}
		fits = append(fits, fitOf(m, e.tracks, rel))
	}
	MarkBestFit(fits)
	return fits, nil
}

// fitOf scores the pairing m of the album's Tracks with rel.
func fitOf(m *ReleaseMatch, tracks model.MediaFiles, rel *musicbrainz.Release) ReleaseFit {
	durations := make(map[string]float64, len(tracks))
	for _, mf := range tracks {
		durations[mf.ID] = float64(mf.Duration)
	}
	f := ReleaseFit{ID: rel.ID, TrackCount: rel.TrackCount(), AlbumTracks: len(m.Tracks), Official: rel.Status == "Official"}
	sum, known := 0.0, 0
	for _, t := range m.Tracks {
		if t.Paired == nil {
			continue
		}
		f.Paired++
		if d := durations[t.ID]; d > 0 && t.Paired.Length > 0 {
			sum += math.Abs(d - float64(t.Paired.Length)/1000)
			known++
		}
	}
	if known > 0 {
		// To the hundredth: closer than that is the same fit, and the tie is broken otherwise.
		diff := math.Round(sum/float64(known)*100) / 100
		f.LengthDiff = &diff
	}
	return f
}

// MarkBestFit marks the release that fits the album's Files best, of fits in search order: the
// one most Tracks pair with, then whose lengths are closest, then an Official one, then the
// first. None is marked unless one pairs most (over half) of the album's Tracks: a poor fit
// isn't offered as the best.
func MarkBestFit(fits []ReleaseFit) {
	best := -1
	for i := range fits {
		fits[i].Best = false
		if fits[i].Paired*2 <= fits[i].AlbumTracks {
			continue
		}
		if best < 0 || compareFit(fits[i], fits[best]) < 0 {
			best = i
		}
	}
	if best >= 0 {
		fits[best].Best = true
	}
}

// compareFit is negative when a fits better than b; zero when neither does, so the earlier one
// in search order stays the best.
func compareFit(a, b ReleaseFit) int {
	return cmp.Or(
		cmp.Compare(b.Paired, a.Paired),
		compareLengthDiff(a.LengthDiff, b.LengthDiff),
		compareBool(a.Official, b.Official),
	)
}

// compareLengthDiff puts the smaller difference first, and an unknown one last.
func compareLengthDiff(a, b *float64) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return 1
	case b == nil:
		return -1
	}
	return cmp.Compare(*a, *b)
}

// compareBool puts true first.
func compareBool(a, b bool) int {
	return -cmp.Compare(boolInt(a), boolInt(b))
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
