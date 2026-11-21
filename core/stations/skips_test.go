package stations_test

import (
	"math/rand/v2"
	"time"

	"github.com/navidrome/navidrome/core/stations"
	"github.com/navidrome/navidrome/model"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const day = 24 * time.Hour

var _ = Describe("Skips", func() {
	skipped := func(id, artist string, ago time.Duration) stations.Skip {
		return stations.Skip{TrackID: id, Artist: artist, At: now.Add(-ago)}
	}

	It("make the Station pick a skipped Track, and its artist, less often", func() {
		candidates := model.MediaFiles{track("skipped", "A"), track("same-artist", "A"), track("other", "B"), track("other-2", "C")}
		weight := stations.SkipWeight([]stations.Skip{skipped("skipped", "A", time.Hour)}, now)
		n := map[string]int{}
		for seed := range uint64(4000) {
			picked := stations.Pick(dj("sunny"), candidates, nil, now, 1, rand.New(rand.NewPCG(seed, 5)), weight)
			n[picked[0].ID]++
		}
		Expect(n["skipped"]).To(BeNumerically("<", n["same-artist"]*4/5))
		Expect(n["same-artist"]).To(BeNumerically("<", n["other"]*9/10))
		// Mild: the skipped Track still plays.
		Expect(n["skipped"]).To(BeNumerically(">", n["other"]/4))
	})

	It("weigh less on a Track the more recent the skip, and not at all after ~30 days", func() {
		weightAfter := func(ago time.Duration) float64 {
			return stations.SkipWeight([]stations.Skip{skipped("t", "A", ago)}, now)(track("t", "A"))
		}
		Expect(weightAfter(time.Minute)).To(BeNumerically("<", weightAfter(10*day)))
		Expect(weightAfter(10 * day)).To(BeNumerically("<", weightAfter(25*day)))
		Expect(weightAfter(25 * day)).To(BeNumerically("<", 1))
		Expect(weightAfter(30 * day)).To(Equal(1.0))
		Expect(weightAfter(90 * day)).To(Equal(1.0))
	})

	It("weigh less on the skipped Track than on its artist's other Tracks", func() {
		weight := stations.SkipWeight([]stations.Skip{skipped("t", "Miles Davis", day)}, now)
		Expect(weight(track("t", "Miles Davis"))).To(BeNumerically("<", weight(track("u", "miles davis "))))
		Expect(weight(track("u", "miles davis "))).To(BeNumerically("<", 1))
		Expect(weight(track("v", "John Coltrane"))).To(Equal(1.0))
	})

	It("add up when a Track is skipped again", func() {
		once := stations.SkipWeight([]stations.Skip{skipped("t", "A", day)}, now)
		twice := stations.SkipWeight([]stations.Skip{skipped("t", "A", day), skipped("t", "A", 2*day)}, now)
		Expect(twice(track("t", "A"))).To(BeNumerically("<", once(track("t", "A"))))
		Expect(twice(track("t", "A"))).To(BeNumerically(">", 0))
	})
})
