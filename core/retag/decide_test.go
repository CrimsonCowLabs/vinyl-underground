package retag

import (
	"github.com/navidrome/navidrome/core/acoustid"
	"github.com/navidrome/navidrome/core/musicbrainz"
	"github.com/navidrome/navidrome/core/tagedit"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func credit(name, id string) musicbrainz.ArtistCredits {
	c := musicbrainz.ArtistCredit{Name: name}
	c.Artist.ID, c.Artist.Name = id, name
	return musicbrainz.ArtistCredits{c}
}

var _ = Describe("deciding", func() {
	var rel *musicbrainz.Release

	BeforeEach(func() {
		rel = release([]string{"Airbag", "Paranoid Android"})
		rel.ArtistCredit = credit("Radiohead", "a-rh")
		rel.ReleaseGroup = musicbrainz.ReleaseGroup{ID: "rg", FirstReleaseDate: "1997-05-21"}
		rel.Date = "2009"
		for i := range rel.Media[0].Tracks {
			rel.Media[0].Tracks[i].ArtistCredit = credit("Radiohead", "a-rh")
		}
	})

	at := func(i int) Placed { return Placed{Disc: 1, Track: rel.Media[0].Tracks[i], ByTitle: true} }

	It("gives a File with no real title the track's title, and fills what is empty", func() {
		f := File{Cleaned: Clean(Local{Title: "Track 02", Artist: "Radiohead", Album: "OK Computer"})}
		w := Want{Local: f.Local}
		notes := w.fromRelease(f, at(1), rel)
		Expect(notes).To(BeEmpty())
		Expect(w.Title).To(Equal("Paranoid Android"))
		Expect(w.Track).To(Equal(2))
		Expect(w.Year).To(Equal(1997))
		Expect(w.IDs).To(HaveKeyWithValue("musicbrainz_trackid", []string{"rec1-2"}))
		Expect(w.IDs).To(HaveKeyWithValue("musicbrainz_releasetrackid", []string{"t1-2"}))
		Expect(w.IDs).NotTo(HaveKey("musicbrainz_albumid"))
	})

	It("adds only the track's IDs, never the album's, which would give the album a new ID", func() {
		f := File{Cleaned: Clean(Local{Title: "Airbag", Artist: "Radiohead", Album: "OK Computer", Track: 1, Year: 1997})}
		w := Want{Local: f.Local}
		w.fromRelease(f, at(0), rel)
		Expect(w.IDs).To(HaveKey("musicbrainz_trackid"))
		Expect(w.IDs).NotTo(HaveKey("musicbrainz_albumid"))
		Expect(w.IDs).NotTo(HaveKey("musicbrainz_releasegroupid"))
	})

	It("takes the track number and year from the release over ones guessed from the File's name", func() {
		f := File{Cleaned: Clean(Local{Title: "Paranoid Android", Artist: "Radiohead", Album: "OK Computer", Stem: "07 Paranoid Android", Folder: "OK Computer (2009)"})}
		Expect(f.Track).To(Equal(7))
		w := Want{Local: f.Local}
		w.fromRelease(f, at(1), rel)
		Expect(w.Track).To(Equal(2))
		Expect(w.Year).To(Equal(1997))
	})

	It("adds no IDs to a File that has some already", func() {
		f := File{Cleaned: Clean(Local{Title: "Airbag", Artist: "Radiohead", Album: "OK Computer", Track: 1, Year: 1997}), MbzRecordingID: "old"}
		w := Want{Local: f.Local}
		w.fromRelease(f, at(0), rel)
		Expect(w.IDs).To(BeEmpty())
	})

	It("keeps values that are set, and notes where MusicBrainz differs", func() {
		f := File{Cleaned: Clean(Local{Title: "Airbag", Artist: "The Smiths", Album: "OK Computer", Track: 5, Year: 2001})}
		w := Want{Local: f.Local}
		notes := w.fromRelease(f, at(0), rel)
		Expect(w.Year).To(Equal(2001))
		Expect(w.Track).To(Equal(5))
		Expect(w.Artist).To(Equal("The Smiths"))
		Expect(notes).To(ConsistOf(
			ContainSubstring("year 2001"), ContainSubstring("track number 5"), ContainSubstring("artist"),
		))
	})

	It("takes a kept number off the title when MusicBrainz's title has none", func() {
		f := File{Cleaned: Clean(Local{Title: "1 Airbag", Artist: "Radiohead", Album: "OK Computer", Track: 1})}
		w := Want{Local: f.Local}
		w.fromRelease(f, at(0), rel)
		Expect(w.Title).To(Equal("Airbag"))
	})

	It("sets the disc number only where the track number agrees with the release's", func() {
		two := release([]string{"Airbag"}, []string{"Lucky", "Karma Police"})
		f := File{Cleaned: Clean(Local{Title: "Karma Police", Artist: "Radiohead", Album: "OK Computer", Track: 3})}
		w := Want{Local: f.Local}
		w.fromRelease(f, Placed{Disc: 2, Track: two.Media[1].Tracks[1], ByTitle: true}, two)
		Expect(w.Disc).To(Equal(0))

		f = File{Cleaned: Clean(Local{Title: "Karma Police", Artist: "Radiohead", Album: "OK Computer", Track: 2})}
		w = Want{Local: f.Local}
		w.fromRelease(f, Placed{Disc: 2, Track: two.Media[1].Tracks[1], ByTitle: true}, two)
		Expect(w.Disc).To(Equal(2))
	})

	It("fills an unknown artist and album from the release", func() {
		f := File{Cleaned: Clean(Local{Title: "Airbag", Artist: "[Unknown Artist]", Track: 1})}
		w := Want{Local: f.Local}
		w.fromRelease(f, at(0), rel)
		Expect(w.Artist).To(Equal("Radiohead"))
		Expect(w.Album).To(Equal("Record"))
		Expect(w.AlbumArtist).To(Equal("Radiohead"))
	})

	It("keeps an album artist that is set", func() {
		f := File{Cleaned: Clean(Local{Title: "Airbag", Artist: "Radiohead", AlbumArtist: "Various Artists", Track: 1})}
		w := Want{Local: f.Local}
		w.fromRelease(f, at(0), rel)
		Expect(w.Album).To(Equal("Record"))
		Expect(w.AlbumArtist).To(Equal("Various Artists"))
	})

	Describe("from AcoustID", func() {
		rec := func(title, artist string) acoustid.Recording {
			r := acoustid.Recording{ID: "rec-" + title, Title: title}
			r.Artists = append(r.Artists, struct {
				ID         string `json:"id"`
				Name       string `json:"name"`
				JoinPhrase string `json:"joinphrase"`
			}{ID: "a-" + artist, Name: artist})
			return r
		}
		result := func(score float64, recs ...acoustid.Recording) []acoustid.Result {
			return []acoustid.Result{{Score: score, Recordings: recs}}
		}

		It("names a File with no real title", func() {
			f := File{Cleaned: Clean(Local{Title: "13", Artist: "[Unknown Artist]", Album: "Mix"})}
			w := Want{Local: f.Local}
			_, ok := w.fromAcoustID(f, result(0.95, rec("Mickey", "Toni Basil")))
			Expect(ok).To(BeTrue())
			Expect(w.Title).To(Equal("Mickey"))
			Expect(w.Artist).To(Equal("Toni Basil"))
			Expect(w.IDs).To(HaveKeyWithValue("musicbrainz_trackid", []string{"rec-Mickey"}))
		})

		It("takes nothing from a weak match", func() {
			f := File{Cleaned: Clean(Local{Title: "13", Artist: "Band", Album: "Mix"})}
			w := Want{Local: f.Local}
			_, ok := w.fromAcoustID(f, result(0.6, rec("Mickey", "Toni Basil")))
			Expect(ok).To(BeFalse())
			Expect(w.Title).To(Equal("13"))
		})

		It("takes no title when the recordings disagree on it", func() {
			f := File{Cleaned: Clean(Local{Title: "13", Artist: "Band", Album: "Mix"})}
			w := Want{Local: f.Local}
			notes, ok := w.fromAcoustID(f, result(0.95, rec("Mickey", "Toni Basil"), rec("Hey Mickey", "Toni Basil")))
			Expect(ok).To(BeFalse())
			Expect(w.Title).To(Equal("13"))
			Expect(notes).NotTo(BeEmpty())
		})

		It("takes nothing for a title that may start with its number when no recording has it", func() {
			f := File{Cleaned: Clean(Local{Title: "12 Fake Plastic Trees", Artist: "[Unknown Artist]", Album: "Mix", Track: 12})}
			w := Want{Local: f.Local}
			_, ok := w.fromAcoustID(f, result(0.95, rec("Creep", "Radiohead")))
			Expect(ok).To(BeFalse())
			Expect(w.Artist).To(Equal("[Unknown Artist]"))
		})

		It("keeps a real title, and picks the recording that has it", func() {
			f := File{Cleaned: Clean(Local{Title: "Mickey", Artist: "[Unknown Artist]", Album: "Mix"})}
			w := Want{Local: f.Local}
			_, ok := w.fromAcoustID(f, result(0.95, rec("Other", "Someone"), rec("Mickey", "Toni Basil")))
			Expect(ok).To(BeTrue())
			Expect(w.Title).To(Equal("Mickey"))
			Expect(w.Artist).To(Equal("Toni Basil"))
		})
	})

	Describe("the values to write", func() {
		It("are only the fields that change", func() {
			orig := Local{Title: "10 - Bones", Artist: "Radiohead", Album: "The Bends", Year: 1995}
			w := Want{Local: Clean(orig).Local}
			Expect(w.values(orig)).To(Equal(tagedit.Values{"title": {"Bones"}, "tracknumber": {"10"}}))
		})
		It("write a year as the date, and IDs", func() {
			orig := Local{Title: "Bones"}
			w := Want{Local: Local{Title: "Bones", Year: 1995, Disc: 1}, IDs: map[string][]string{"musicbrainz_trackid": {"rec"}}}
			Expect(w.values(orig)).To(Equal(tagedit.Values{"date": {"1995"}, "discnumber": {"1"}, "musicbrainz_trackid": {"rec"}}))
		})
	})
})
