package retag

import (
	"fmt"

	"github.com/navidrome/navidrome/core/musicbrainz"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// release makes a release of one medium per disc, track i of a disc titled titles[i] and
// lasting 200+i seconds.
func release(discs ...[]string) *musicbrainz.Release {
	rel := &musicbrainz.Release{ID: "rel", Title: "Record"}
	for d, titles := range discs {
		m := musicbrainz.Media{Position: d + 1, TrackCount: len(titles)}
		for i, t := range titles {
			m.Tracks = append(m.Tracks, musicbrainz.Track{
				ID: fmt.Sprintf("t%d-%d", d+1, i+1), Position: i + 1, Title: t, Length: (200 + i) * 1000,
				Recording: musicbrainz.Recording{ID: fmt.Sprintf("rec%d-%d", d+1, i+1)},
			})
		}
		rel.Media = append(rel.Media, m)
	}
	return rel
}

func file(id, title string, track int, duration float64) File {
	return File{ID: id, Cleaned: Clean(Local{Title: title, Artist: "Band", Album: "Record", Track: track}), Duration: duration}
}

var _ = Describe("MatchRelease", func() {
	titles := []string{"Airbag", "Paranoid Android", "Subterranean Homesick Alien", "Exit Music (For a Film)"}

	It("places Files by title", func() {
		files := []File{file("a", "Airbag", 1, 200), file("b", "Paranoid Android", 0, 201.5), file("c", "Exit Music (for a film)", 4, 203)}
		placed, ok := MatchRelease(files, release(titles))
		Expect(ok).To(BeTrue())
		Expect(placed).To(HaveLen(3))
		Expect(placed["b"].Track.ID).To(Equal("t1-2"))
		Expect(placed["c"].Track.Recording.ID).To(Equal("rec1-4"))
		Expect(placed["c"].Disc).To(Equal(1))
	})

	It("places a File with no real title by its position and length, among Files placed by title", func() {
		files := []File{file("a", "Airbag", 1, 200), file("b", "Track 02", 0, 201), file("c", "Subterranean Homesick Alien", 3, 202)}
		placed, ok := MatchRelease(files, release(titles))
		Expect(ok).To(BeTrue())
		Expect(placed["b"].Track.Title).To(Equal("Paranoid Android"))
		Expect(placed["b"].ByTitle).To(BeFalse())
	})

	It("doesn't place a File with no real title whose length is wrong", func() {
		files := []File{file("a", "Airbag", 1, 200), file("b", "Track 02", 0, 260), file("c", "Subterranean Homesick Alien", 3, 202)}
		placed, ok := MatchRelease(files, release(titles))
		Expect(ok).To(BeTrue())
		Expect(placed).NotTo(HaveKey("b"))
	})

	It("takes a whole album of Files with no real titles when every position and length fits", func() {
		files := []File{file("a", "Track 01", 0, 200), file("b", "Track 02", 0, 201), file("c", "Track 03", 0, 202), file("d", "Track 04", 0, 203)}
		placed, ok := MatchRelease(files, release(titles))
		Expect(ok).To(BeTrue())
		Expect(placed).To(HaveLen(4))
	})

	It("doesn't take two Files with no real titles on lengths alone", func() {
		files := []File{file("a", "Track 01", 0, 200), file("b", "Track 02", 0, 201)}
		_, ok := MatchRelease(files, release(titles))
		Expect(ok).To(BeFalse())
	})

	It("doesn't take a release whose lengths are all wrong", func() {
		files := []File{file("a", "Airbag", 1, 230), file("b", "Paranoid Android", 2, 240)}
		_, ok := MatchRelease(files, release(titles))
		Expect(ok).To(BeFalse())
	})

	It("doesn't take a release with fewer tracks than the folder has Files", func() {
		files := []File{file("a", "Airbag", 1, 200), file("b", "Paranoid Android", 2, 201), file("c", "Bonus", 3, 300)}
		_, ok := MatchRelease(files, release(titles[:2]))
		Expect(ok).To(BeFalse())
	})

	It("doesn't take a release most of the folder's titles aren't on", func() {
		files := []File{file("a", "Airbag", 1, 200), file("b", "Creep", 2, 201), file("c", "Karma Police", 3, 202), file("d", "No Surprises", 4, 203)}
		_, ok := MatchRelease(files, release(titles))
		Expect(ok).To(BeFalse())
	})

	It("matches a title kept with its number by the title without it", func() {
		files := []File{file("a", "1 Airbag", 1, 200), file("b", "2 Paranoid Android", 2, 201)}
		placed, ok := MatchRelease(files, release(titles))
		Expect(ok).To(BeTrue())
		Expect(placed["a"].Track.Title).To(Equal("Airbag"))
	})

	It("matches a title that really starts with a number", func() {
		files := []File{file("a", "15 Step", 1, 200), file("b", "Bodysnatchers", 2, 201)}
		placed, ok := MatchRelease(files, release([]string{"15 Step", "Bodysnatchers"}))
		Expect(ok).To(BeTrue())
		Expect(placed["a"].Track.Title).To(Equal("15 Step"))
	})

	It("places Files on the right disc", func() {
		f := file("a", "Track 01", 0, 200)
		f.Disc = 2
		files := []File{file("x", "Airbag", 1, 200), f}
		placed, ok := MatchRelease(files, release(titles, []string{"Disc Two Opener"}))
		Expect(ok).To(BeTrue())
		Expect(placed["a"].Disc).To(Equal(2))
		Expect(placed["a"].Track.Title).To(Equal("Disc Two Opener"))
	})

	It("doesn't place two Files on one track", func() {
		files := []File{file("a", "Airbag", 1, 200), file("b", "Airbag", 0, 200), file("c", "Paranoid Android", 2, 201)}
		placed, _ := MatchRelease(files, release(titles))
		Expect(placed).To(HaveKey("a"))
		Expect(placed).NotTo(HaveKey("b"))
	})
})
