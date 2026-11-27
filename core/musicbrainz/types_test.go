package musicbrainz_test

import (
	"github.com/navidrome/navidrome/core/musicbrainz"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Release.AlbumGenres", func() {
	genres := func(pairs ...any) []musicbrainz.Genre {
		var g []musicbrainz.Genre
		for i := 0; i < len(pairs); i += 2 {
			g = append(g, musicbrainz.Genre{Name: pairs[i].(string), Count: pairs[i+1].(int)})
		}
		return g
	}

	It("takes the release group's strongest genres, most votes first, in Title Case", func() {
		// Mer de Noms, as MusicBrainz had it: the one-vote genres are left out.
		r := musicbrainz.Release{
			Genres: genres("alternative rock", 3),
			ReleaseGroup: musicbrainz.ReleaseGroup{Genres: genres("alternative metal", 6, "alternative rock", 11,
				"heavy metal", 1, "metal", 1, "post-grunge", 1, "progressive rock", 1, "rock", 5)},
		}
		Expect(r.AlbumGenres()).To(Equal([]string{"Alternative Rock", "Alternative Metal", "Rock"}))
	})

	It("falls back to the release's own genres when its release group has none", func() {
		r := musicbrainz.Release{Genres: genres("hip hop", 2)}
		Expect(r.AlbumGenres()).To(Equal([]string{"Hip Hop"}))
	})

	It("is empty when neither has a genre", func() {
		Expect(musicbrainz.Release{}.AlbumGenres()).To(BeEmpty())
	})

	It("keeps at most five", func() {
		r := musicbrainz.Release{ReleaseGroup: musicbrainz.ReleaseGroup{Genres: genres(
			"a", 3, "b", 3, "c", 3, "d", 3, "e", 3, "f", 3)}}
		Expect(r.AlbumGenres()).To(HaveLen(5))
	})

	It("writes acronyms in capitals and keeps small joining words small", func() {
		r := musicbrainz.Release{ReleaseGroup: musicbrainz.ReleaseGroup{Genres: genres(
			"r&b", 4, "drum and bass", 4, "uk garage", 4, "synth-pop", 4)}}
		Expect(r.AlbumGenres()).To(Equal([]string{"Drum and Bass", "R&B", "Synth-Pop", "UK Garage"}))
	})
})
