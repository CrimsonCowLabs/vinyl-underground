package corrections_test

import (
	"github.com/navidrome/navidrome/core/corrections"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Which release of a MusicBrainz search fits the album's Files best.
var _ = Describe("MarkBestFit", func() {
	diff := func(s float64) *float64 { return &s }
	best := func(fits []corrections.ReleaseFit) []string {
		corrections.MarkBestFit(fits)
		var ids []string
		for _, f := range fits {
			if f.Best {
				ids = append(ids, f.ID)
			}
		}
		return ids
	}

	It("picks, of editions every Track pairs with, the one whose lengths are closest", func() {
		Expect(best([]corrections.ReleaseFit{
			{ID: "remaster", Paired: 12, AlbumTracks: 12, TrackCount: 12, LengthDiff: diff(2.4), Official: true},
			{ID: "original", Paired: 12, AlbumTracks: 12, TrackCount: 12, LengthDiff: diff(0.3), Official: true},
			{ID: "unknown", Paired: 12, AlbumTracks: 12, TrackCount: 12},
		})).To(Equal([]string{"original"}))
	})

	It("picks the one most Tracks pair with first, however close the others' lengths", func() {
		Expect(best([]corrections.ReleaseFit{
			{ID: "close", Paired: 10, AlbumTracks: 12, LengthDiff: diff(0.1)},
			{ID: "whole", Paired: 12, AlbumTracks: 12, LengthDiff: diff(1.5)},
		})).To(Equal([]string{"whole"}))
	})

	It("prefers a known length difference to an unknown one", func() {
		Expect(best([]corrections.ReleaseFit{
			{ID: "unknown", Paired: 12, AlbumTracks: 12, Official: true},
			{ID: "known", Paired: 12, AlbumTracks: 12, LengthDiff: diff(3)},
		})).To(Equal([]string{"known"}))
	})

	It("marks none unless one pairs most of the album's Tracks", func() {
		Expect(best([]corrections.ReleaseFit{
			{ID: "half", Paired: 6, AlbumTracks: 12, LengthDiff: diff(0)},
			{ID: "few", Paired: 2, AlbumTracks: 12, LengthDiff: diff(0)},
		})).To(BeEmpty())
		Expect(best([]corrections.ReleaseFit{
			{ID: "most", Paired: 7, AlbumTracks: 12, LengthDiff: diff(0)},
		})).To(Equal([]string{"most"}))
		Expect(best(nil)).To(BeEmpty())
	})

	It("breaks a tie by an Official release, then by search order", func() {
		Expect(best([]corrections.ReleaseFit{
			{ID: "promo", Paired: 12, AlbumTracks: 12, LengthDiff: diff(0.5)},
			{ID: "official", Paired: 12, AlbumTracks: 12, LengthDiff: diff(0.5), Official: true},
			{ID: "official-later", Paired: 12, AlbumTracks: 12, LengthDiff: diff(0.5), Official: true},
		})).To(Equal([]string{"official"}))
		Expect(best([]corrections.ReleaseFit{
			{ID: "first", Paired: 12, AlbumTracks: 12, LengthDiff: diff(0.5)},
			{ID: "second", Paired: 12, AlbumTracks: 12, LengthDiff: diff(0.5)},
		})).To(Equal([]string{"first"}))
	})

	It("marks one only, however it was marked before", func() {
		fits := []corrections.ReleaseFit{
			{ID: "a", Paired: 1, AlbumTracks: 12, Best: true},
			{ID: "b", Paired: 12, AlbumTracks: 12},
		}
		Expect(best(fits)).To(Equal([]string{"b"}))
	})
})
