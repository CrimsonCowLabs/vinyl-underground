package stations_test

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/navidrome/navidrome/core/stations"
	"github.com/navidrome/navidrome/model"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var now = time.Date(2026, 11, 20, 22, 0, 0, 0, time.UTC)

func track(id, artist string) model.MediaFile {
	return model.MediaFile{ID: id, Title: "Song " + id, Artist: artist, Tags: model.Tags{}}
}

func ids(mfs model.MediaFiles) []string {
	res := make([]string, len(mfs))
	for i, mf := range mfs {
		res[i] = mf.ID
	}
	return res
}

func dj(id string) stations.DJ {
	d, ok := stations.FindDJ(id)
	Expect(ok).To(BeTrue(), "no DJ "+id)
	return d
}

// distinctArtists returns n Tracks, each by a different artist.
func distinctArtists(n int) model.MediaFiles {
	var res model.MediaFiles
	for i := range n {
		res = append(res, track(fmt.Sprintf("t%d", i), fmt.Sprintf("Artist %d", i)))
	}
	return res
}

var _ = Describe("Pick", func() {
	var rnd *rand.Rand

	BeforeEach(func() {
		rnd = rand.New(rand.NewPCG(1, 2))
	})

	It("picks the number of Tracks asked for, each once", func() {
		picked := stations.Pick(dj("night-owl"), distinctArtists(20), nil, now, 5, rnd)
		Expect(picked).To(HaveLen(5))
		seen := map[string]bool{}
		for _, id := range ids(picked) {
			Expect(seen).ToNot(HaveKey(id))
			seen[id] = true
		}
	})

	It("skips Tracks the Station served or the Listener played in the last 48h", func() {
		candidates := distinctArtists(12)
		playedYesterday := now.Add(-24 * time.Hour)
		candidates[0].PlayDate = &playedYesterday
		candidates[0].PlayCount = 3
		history := []stations.Served{
			{TrackID: "t1", Artist: "Artist 1", At: now.Add(-47 * time.Hour)},
			{TrackID: "t2", Artist: "Artist 2", At: now.Add(-30 * time.Hour)},
		}
		for seed := range uint64(30) {
			picked := stations.Pick(dj("sunny"), candidates, history, now, 9, rand.New(rand.NewPCG(seed, 1)))
			Expect(picked).To(HaveLen(9))
			Expect(ids(picked)).ToNot(ContainElement("t0"))
			Expect(ids(picked)).ToNot(ContainElement("t1"))
			Expect(ids(picked)).ToNot(ContainElement("t2"))
		}
	})

	It("plays a Track again once 48h have passed", func() {
		candidates := distinctArtists(1)
		playedLongAgo := now.Add(-49 * time.Hour)
		candidates[0].PlayDate = &playedLongAgo
		history := []stations.Served{{TrackID: "t0", Artist: "Artist 0", At: now.Add(-49 * time.Hour)}}
		Expect(ids(stations.Pick(dj("sunny"), candidates, history, now, 1, rnd))).To(Equal([]string{"t0"}))
	})

	It("doesn't repeat an artist within 5 Tracks, counting what the Station already served", func() {
		var candidates model.MediaFiles
		for a := range 7 {
			for t := range 4 {
				candidates = append(candidates, track(fmt.Sprintf("a%d-t%d", a, t), fmt.Sprintf("Artist %d", a)))
			}
		}
		history := []stations.Served{
			{TrackID: "old-1", Artist: "artist 0", At: now.Add(-time.Hour)},
			{TrackID: "old-2", Artist: "ARTIST 1", At: now.Add(-2 * time.Hour)},
		}
		for seed := range uint64(30) {
			picked := stations.Pick(dj("crate-digger"), candidates, history, now, 12, rand.New(rand.NewPCG(seed, 7)))
			Expect(picked).To(HaveLen(12))
			sequence := []string{"artist 1", "artist 0"}
			for _, mf := range picked {
				sequence = append(sequence, strings.ToLower(mf.Artist))
			}
			for i := range sequence {
				for j := i + 1; j < len(sequence) && j <= i+5; j++ {
					Expect(sequence[j]).ToNot(Equal(sequence[i]), "seed %d: %v", seed, sequence)
				}
			}
		}
	})

	It("keeps playing when the Library is too small for the repeat rules", func() {
		candidates := model.MediaFiles{track("t0", "Solo"), track("t1", "Solo")}
		history := []stations.Served{{TrackID: "t0", Artist: "solo", At: now.Add(-time.Minute)}}

		picked := stations.Pick(dj("night-owl"), candidates, history, now, 3, rnd)

		Expect(ids(picked)).To(Equal([]string{"t1", "t0", "t1"}))
	})

	It("returns nothing when there are no candidates", func() {
		Expect(stations.Pick(dj("night-owl"), nil, nil, now, 3, rnd)).To(BeEmpty())
	})
})

// picks counts how often each Track is the first pick over many Stations.
func picks(d stations.DJ, candidates model.MediaFiles) map[string]int {
	res := map[string]int{}
	for seed := range uint64(2000) {
		picked := stations.Pick(d, candidates, nil, now, 1, rand.New(rand.NewPCG(seed, 99)))
		res[picked[0].ID]++
	}
	return res
}

func with(mf model.MediaFile, changes ...func(*model.MediaFile)) model.MediaFile {
	for _, change := range changes {
		change(&mf)
	}
	return mf
}

func genre(g string) func(*model.MediaFile) {
	return func(mf *model.MediaFile) { mf.Genre = g; mf.Tags = model.Tags{model.TagGenre: {g}} }
}

func bpm(v int) func(*model.MediaFile) {
	return func(mf *model.MediaFile) { mf.BPM = &v }
}

func year(y int) func(*model.MediaFile) {
	return func(mf *model.MediaFile) { mf.Year = y }
}

func rating(r int) func(*model.MediaFile) {
	return func(mf *model.MediaFile) { mf.Rating = r }
}

func starred(mf *model.MediaFile) { mf.Starred = true }

func played(times int64) func(*model.MediaFile) {
	return func(mf *model.MediaFile) {
		mf.PlayCount = times
		at := now.Add(-30 * 24 * time.Hour)
		mf.PlayDate = &at
	}
}

var _ = Describe("DJ taste", func() {
	It("has the three built-in DJs", func() {
		var names []string
		for _, d := range stations.DJs() {
			names = append(names, d.Name)
			Expect(d.Description).ToNot(BeEmpty())
		}
		Expect(names).To(Equal([]string{"Night Owl", "The Crate Digger", "Sunny"}))
	})

	DescribeTable("every DJ leans toward the Listener's favorites",
		func(id string) {
			n := picks(dj(id), model.MediaFiles{
				with(track("starred", "A"), starred),
				with(track("rated", "B"), rating(5)),
				track("plain", "C"),
			})
			Expect(n["starred"]).To(BeNumerically(">", n["plain"]*5/4))
			Expect(n["rated"]).To(BeNumerically(">", n["plain"]*5/4))
		},
		Entry("Night Owl", "night-owl"),
		Entry("The Crate Digger", "crate-digger"),
		Entry("Sunny", "sunny"),
	)

	DescribeTable("every DJ stays away from Tracks the Listener rated low",
		func(id string) {
			n := picks(dj(id), model.MediaFiles{with(track("disliked", "A"), rating(1)), track("plain", "B")})
			Expect(n["plain"]).To(BeNumerically(">", n["disliked"]*3))
		},
		Entry("Night Owl", "night-owl"),
		Entry("The Crate Digger", "crate-digger"),
		Entry("Sunny", "sunny"),
	)

	DescribeTable("Night Owl and Sunny lean toward what the Listener plays most",
		func(id string) {
			n := picks(dj(id), model.MediaFiles{with(track("often", "A"), played(40)), track("never", "B")})
			Expect(n["often"]).To(BeNumerically(">", n["never"]*5/4))
		},
		Entry("Night Owl", "night-owl"),
		Entry("Sunny", "sunny"),
	)

	It("Night Owl likes it slow and low-key", func() {
		n := picks(dj("night-owl"), model.MediaFiles{
			with(track("jazz", "A"), genre("Cool Jazz")),
			with(track("metal", "B"), genre("Thrash Metal")),
			with(track("slow", "C"), genre("Electronic"), bpm(80)),
			with(track("fast", "D"), genre("Electronic"), bpm(150)),
		})
		Expect(n["jazz"]).To(BeNumerically(">", n["metal"]*2))
		Expect(n["slow"]).To(BeNumerically(">", n["fast"]*5/4))
	})

	It("Sunny likes it upbeat", func() {
		n := picks(dj("sunny"), model.MediaFiles{
			with(track("disco", "A"), genre("Disco")),
			with(track("ambient", "B"), genre("Dark Ambient")),
			with(track("fast", "C"), genre("Electronic"), bpm(124)),
			with(track("slow", "D"), genre("Electronic"), bpm(70)),
		})
		Expect(n["disco"]).To(BeNumerically(">", n["ambient"]*2))
		Expect(n["fast"]).To(BeNumerically(">", n["slow"]*5/4))
	})

	It("The Crate Digger digs out rarely played Tracks and older records", func() {
		n := picks(dj("crate-digger"), model.MediaFiles{
			with(track("hit", "A"), played(40)),
			track("deep-cut", "B"),
			with(track("old", "C"), year(1971)),
			with(track("new", "D"), year(2021)),
		})
		Expect(n["deep-cut"]).To(BeNumerically(">", n["hit"]*2))
		Expect(n["old"]).To(BeNumerically(">", n["new"]*5/4))
	})

	It("The Crate Digger finds deep cuts by the artists the Listener loves", func() {
		n := picks(dj("crate-digger"), model.MediaFiles{
			with(track("their-hit", "Loved Band"), played(40), starred),
			track("their-deep-cut", "Loved Band"),
			track("stranger", "Unknown Band"),
		})
		Expect(n["their-deep-cut"]).To(BeNumerically(">", n["stranger"]*3/2))
	})
})
