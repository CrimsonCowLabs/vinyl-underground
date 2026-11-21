package stations_test

import (
	"math/rand/v2"

	"github.com/navidrome/navidrome/core/stations"
	"github.com/navidrome/navidrome/model"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("The LLM's ranked picks", func() {
	ranked := model.MediaFiles{
		{ID: "a", Artist: "Artist A"},
		{ID: "b", Artist: "Artist B"},
		{ID: "c", Artist: "Artist C"},
		{ID: "d", Artist: "Artist D"},
	}

	It("drops a pick its weight rules out, keeping the rest in the LLM's order", func() {
		noB := func(mf model.MediaFile) float64 {
			if mf.ID == "b" {
				return 0
			}
			return 1
		}

		picked, rest := stations.PickRanked(ranked, nil, now, 2, rand.New(rand.NewPCG(1, 2)), noB)

		Expect(ids(picked)).To(Equal([]string{"a", "c"}))
		Expect(ids(rest)).To(Equal([]string{"d"}))
	})

	It("plays a skipped pick, and its artist's, less often on that Station", func() {
		weight := stations.SkipWeight([]stations.Skip{{TrackID: "a", Artist: "Artist A", At: now}}, now)
		rnd := rand.New(rand.NewPCG(1, 2))

		kept := 0
		const runs = 2000
		for range runs {
			picked, _ := stations.PickRanked(ranked, nil, now, 1, rnd, weight)
			if picked[0].ID == "a" {
				kept++
			}
		}

		share := float64(kept) / runs
		Expect(share).To(BeNumerically("<", 0.9))
		Expect(share).To(BeNumerically(">", 0.2))
	})
})
