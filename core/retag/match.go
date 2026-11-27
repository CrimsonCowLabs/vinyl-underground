package retag

import (
	"cmp"
	"math"
	"slices"

	"github.com/navidrome/navidrome/core/musicbrainz"
	"github.com/navidrome/navidrome/utils/str"
	"github.com/xrash/smetrics"
)

// File is one File of a folder, as retag sees it: its Track's ID and path, its values after the
// local fixes, and its length.
type File struct {
	ID, Path string
	Cleaned
	// Duration is in seconds.
	Duration float64
	// MbzAlbumID and MbzRecordingID are the MusicBrainz IDs the File already has, if any.
	MbzAlbumID, MbzRecordingID string
}

// Placed is the release track a File is.
type Placed struct {
	// Disc is the track's medium's position.
	Disc  int
	Track musicbrainz.Track
	// ByTitle is a File placed by its title, not just its position and length.
	ByTitle bool
}

const (
	// titleThreshold is how alike (Jaro-Winkler, sanitized) a File's title and a track's must be.
	titleThreshold = 0.9
	// byTitleShare is the share of a folder's titled Files a release must place by title.
	byTitleShare = 0.7
	// minUntitled is the fewest Files with no real title a release is taken for on positions and
	// lengths alone.
	minUntitled = 3
)

// LengthFits reports whether a File lasting seconds can be a track of ms milliseconds: within 4
// seconds, or 3% of a long track. A track of unknown length fits, unknown.
func LengthFits(seconds float64, ms int) (fits, known bool) {
	if ms <= 0 || seconds <= 0 {
		return true, false
	}
	d := math.Abs(seconds - float64(ms)/1000)
	return d <= max(4, seconds*0.03), true
}

func similarity(a, b string) float64 {
	a, b = str.SanitizeFieldForSorting(a), str.SanitizeFieldForSorting(b)
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	return smetrics.JaroWinkler(a, b, 0.7, 4)
}

type pairing struct {
	file, disc int
	track      musicbrainz.Track
	byTitle    bool
	score      float64
}

// MatchRelease places a folder's Files on the release's tracks, and reports whether the release
// is the folder's: it has room for every File, and places most of the titled Files by title (or,
// with no titled Files, at least minUntitled Files, every one by position and length). A File
// with a real title is placed by it, and only where its length fits; a File with no real title
// only where its position is, and its length fits. No track takes two Files.
func MatchRelease(files []File, rel *musicbrainz.Release) (map[string]Placed, bool) {
	if len(files) == 0 || len(files) > rel.TrackCount() {
		return nil, false
	}
	var pairs []pairing
	for fi, f := range files {
		for _, m := range rel.Media {
			for _, t := range m.Tracks {
				fits, known := LengthFits(f.Duration, t.Length)
				if !fits {
					continue
				}
				atPosition := f.Track == t.Position && (f.Disc == m.Position || (f.Disc == 0 && m.Position == 1))
				if f.Has(FlagNoTitle) {
					if atPosition && known {
						pairs = append(pairs, pairing{file: fi, disc: m.Position, track: t, score: 1})
					}
					continue
				}
				sim := similarity(f.Title, t.Title)
				if f.Alt != "" {
					sim = max(sim, similarity(f.Alt, t.Title))
				}
				if sim < titleThreshold {
					continue
				}
				score := 2 + sim
				if atPosition {
					score += 0.5
				}
				pairs = append(pairs, pairing{file: fi, disc: m.Position, track: t, byTitle: true, score: score})
			}
		}
	}
	slices.SortStableFunc(pairs, func(a, b pairing) int { return cmp.Compare(b.score, a.score) })

	placed := map[string]Placed{}
	taken := map[string]bool{}
	byTitle := 0
	for _, p := range pairs {
		id := files[p.file].ID
		if _, done := placed[id]; done || taken[p.track.ID] {
			continue
		}
		placed[id] = Placed{Disc: p.disc, Track: p.track, ByTitle: p.byTitle}
		taken[p.track.ID] = true
		if p.byTitle {
			byTitle++
		}
	}

	titled := 0
	for _, f := range files {
		if !f.Has(FlagNoTitle) {
			titled++
		}
	}
	if titled == 0 {
		return placed, len(files) >= minUntitled && len(placed) == len(files)
	}
	return placed, float64(byTitle) >= math.Ceil(byTitleShare*float64(titled))
}
