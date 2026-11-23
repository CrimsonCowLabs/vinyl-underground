package corrections

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/navidrome/navidrome/model"
)

// TrackNumber is a Track's disc and track number as the admin corrects them.
type TrackNumber struct {
	ID          string `json:"id"`
	DiscNumber  int    `json:"discNumber"`
	TrackNumber int    `json:"trackNumber"`
}

// The highest numbers accepted (also in ui/src/album/trackNumbers.js)
const (
	maxDiscNumber  = 999
	maxTrackNumber = 9999
)

// trackNumbersLabel lists the renumbered Tracks for the Corrections page, as numbered now:
// "1-1 One, 1-2 Two" (just "2 Two" for a Track with no disc number).
func trackNumbersLabel(fixes model.TrackNumberFixes) string {
	sorted := slices.SortedStableFunc(slices.Values(fixes), func(a, b model.TrackNumberFix) int {
		return cmp.Or(cmp.Compare(a.Disc, b.Disc), cmp.Compare(a.Track, b.Track))
	})
	labels := make([]string, 0, len(fixes))
	for _, f := range sorted {
		n := fmt.Sprint(f.Track)
		if f.Disc > 0 {
			n = fmt.Sprintf("%d-%d", f.Disc, f.Track)
		}
		labels = append(labels, n+" "+f.Title)
	}
	return strings.Join(labels, ", ")
}
