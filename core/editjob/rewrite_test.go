package editjob

import (
	"github.com/navidrome/navidrome/core/tagedit"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Rewrite", func() {
	It("renames an Artist among a File's other artists, leaving them alone", func() {
		edit, err := Rewrite{Rename: &Rename{From: []string{"Ann"}, To: "Anne"}}.Apply(tagedit.Values{
			"artist":               {"Ann & Bob"},
			"artists":              {"Ann", "Bob"},
			"musicbrainz_artistid": {"id-ann", "id-bob"},
			"albumartist":          {"Joanne"},
			"albumartists":         {},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(edit).To(Equal(tagedit.Values{"artist": {"Anne & Bob"}, "artists": {"Anne", "Bob"}}))
	})

	It("matches names whatever their case and spaces in the multi-valued fields", func() {
		edit, err := Rewrite{Rename: &Rename{From: []string{"The  Band"}, To: "The Band"}}.Apply(tagedit.Values{
			"albumartist": {"the band"}, "albumartists": {"the band"},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(edit).To(Equal(tagedit.Values{"albumartist": {"The Band"}, "albumartists": {"The Band"}}))
	})

	It("credits a merged Artist once, with its MusicBrainz ID once", func() {
		edit, err := Rewrite{Rename: &Rename{From: []string{"Ann"}, To: "Bob"}}.Apply(tagedit.Values{
			"artists":              {"Ann", "Bob"},
			"musicbrainz_artistid": {"id-ann", "id-bob"},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(edit).To(Equal(tagedit.Values{"artists": {"Bob"}, "musicbrainz_artistid": {"id-bob"}}))
	})

	It("renames each of several values a File holds in a single-valued field", func() {
		edit, err := Rewrite{Rename: &Rename{From: []string{"Ann"}, To: "Bob"}}.Apply(tagedit.Values{
			"artist": {"Ann", "Bob"}, "albumartist": {"Ann", "Cat"},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(edit).To(Equal(tagedit.Values{"artist": {"Bob"}, "albumartist": {"Bob", "Cat"}}))
	})

	It("changes nothing in a File that doesn't name the Artist", func() {
		edit, err := Rewrite{Rename: &Rename{From: []string{"Ann"}, To: "Anne"}}.Apply(tagedit.Values{
			"artist": {"Joanne"}, "artists": {"Joanne"},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(edit).To(BeEmpty())
	})

	It("sets fields, dropping those that go with a changed one", func() {
		r := Rewrite{
			Set:  tagedit.Values{"album": {"Record"}, "albumartist": {"Band"}},
			Drop: map[string][]string{"albumartist": {"albumartists", "musicbrainz_albumartistid"}},
		}
		edit, err := r.Apply(tagedit.Values{"album": {"Record"}, "albumartist": {"Bnad"}, "albumartists": {"Bnad"},
			"musicbrainz_albumartistid": {}})
		Expect(err).ToNot(HaveOccurred())
		Expect(edit).To(Equal(tagedit.Values{"albumartist": {"Band"}, "albumartists": {}}))

		// An album artist the File already holds keeps what goes with it.
		edit, err = r.Apply(tagedit.Values{"album": {"Record"}, "albumartist": {"Band"}, "albumartists": {"Band", "Guest"}})
		Expect(err).ToNot(HaveOccurred())
		Expect(edit).To(BeEmpty())
	})

	It("puts every date in the year, keeping a date already in it whole", func() {
		edit, err := Rewrite{Year: 1999}.Apply(tagedit.Values{"date": {"2005-03-01"}, "originaldate": {"1999-06-02"}})
		Expect(err).ToNot(HaveOccurred())
		Expect(edit).To(Equal(tagedit.Values{"date": {"1999"}}))

		edit, err = Rewrite{Year: 1999}.Apply(tagedit.Values{"date": {}, "originaldate": {}})
		Expect(err).ToNot(HaveOccurred())
		Expect(edit).To(Equal(tagedit.Values{"date": {"1999"}}))
	})
})
