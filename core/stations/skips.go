package stations

import (
	"time"

	"github.com/navidrome/navidrome/model"
)

const (
	// SkipFade is how long a skip shapes a Station. Its effect fades evenly to nothing over it.
	SkipFade = 30 * 24 * time.Hour
	// TrackSkipPenalty is how much of its chance a Track loses when it was just skipped.
	TrackSkipPenalty = 0.4
	// ArtistSkipPenalty is how much of their chance an artist's Tracks lose when one of them
	// was just skipped. It is milder: skipping one song isn't turning against its artist.
	ArtistSkipPenalty = 0.2
	// MinSkipWeight is the least of its chance skips leave a Track, however many there were:
	// a skip is a mild "less like this", never a ban.
	MinSkipWeight = 0.2
)

// Skip is a Track the Listener skipped on a Station.
type Skip struct {
	TrackID string
	Artist  string
	At      time.Time
}

// Weight scales a Track's chance on a Station: 1 leaves it alone, and less makes it rarer.
type Weight func(mf model.MediaFile) float64

// SkipWeight makes a Station a little less likely to play the Tracks skipped on it, and their
// artists' other Tracks. Each skip counts in full when it happens and fades to nothing over
// SkipFade, and skips compound, so a Track skipped again gets rarer still, down to
// MinSkipWeight. Tracks with no artist share no artist penalty.
func SkipWeight(skips []Skip, now time.Time) Weight {
	return func(mf model.MediaFile) float64 {
		w := 1.0
		artist := artistKey(mf.Artist)
		for _, s := range skips {
			fresh := 1 - float64(now.Sub(s.At))/float64(SkipFade)
			if fresh <= 0 {
				continue
			}
			fresh = min(fresh, 1)
			if s.TrackID == mf.ID {
				w *= 1 - TrackSkipPenalty*fresh
			}
			if artist != "" && artistKey(s.Artist) == artist {
				w *= 1 - ArtistSkipPenalty*fresh
			}
		}
		return max(w, MinSkipWeight)
	}
}
