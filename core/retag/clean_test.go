package retag

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Clean", func() {
	DescribeTable("a leading track number",
		func(in Local, title string, track int) {
			out := Clean(in)
			Expect(out.Title).To(Equal(title))
			Expect(out.Track).To(Equal(track))
		},
		Entry("after a dash, with no track number", Local{Title: "10 - Bones"}, "Bones", 10),
		Entry("after a dash, matching the track number", Local{Title: "10 - Bones", Track: 10}, "Bones", 10),
		Entry("after a dot", Local{Title: "03. Airbag", Track: 3}, "Airbag", 3),
		Entry("after an underscore", Local{Title: "04_Lucky"}, "Lucky", 4),
		Entry("zero-padded, matching", Local{Title: "01 Halloween", Track: 1}, "Halloween", 1),
		Entry("zero-padded, no track number", Local{Title: "07 Paranoid Android"}, "Paranoid Android", 7),
		Entry("differing from the track number stays", Local{Title: "20 Eyes", Track: 29}, "20 Eyes", 29),
		Entry("differing, zero-padded, stays", Local{Title: "08 Guns", Track: 3}, "08 Guns", 3),
		Entry("with only digits after it stays", Local{Title: "19-2000"}, "19-2000", 0),
		Entry("in a number title stays", Local{Title: "5-3-10-4", Track: 9}, "5-3-10-4", 9),
		Entry("a chemical formula stays", Local{Title: "C11 H17 N2 O2 S Na", Track: 4}, "C11 H17 N2 O2 S Na", 4),
		Entry("a disc and track number", Local{Title: "5-15 After da Hour"}, "After da Hour", 15),
		Entry("a decimal stays", Local{Title: "3.5 Minutes", Track: 3}, "3.5 Minutes", 3),
		Entry("one digit joined by a hyphen stays", Local{Title: "4-Ever"}, "4-Ever", 0),
	)

	It("keeps what the File had before the local fixes", func() {
		out := Clean(Local{Title: "10 - Bones"})
		Expect(out.Orig.Track).To(Equal(0))
		Expect(out.Track).To(Equal(10))
	})

	It("keeps an unpadded number with no separator, and offers the title without it", func() {
		out := Clean(Local{Title: "12 Fake Plastic Trees", Track: 12})
		Expect(out.Title).To(Equal("12 Fake Plastic Trees"))
		Expect(out.Flags).To(ContainElement(FlagNumberKept))
		Expect(out.Alt).To(Equal("Fake Plastic Trees"))
	})

	It("leaves a title starting with a number that isn't a track number alone", func() {
		out := Clean(Local{Title: "15 Step", Artist: "Radiohead", Album: "In Rainbows", Track: 1})
		Expect(out.Title).To(Equal("15 Step"))
		Expect(out.Flags).To(BeEmpty())
		Expect(out.Fixes).To(BeEmpty())
	})

	It("takes the disc number from a disc-track prefix", func() {
		out := Clean(Local{Title: "1-07 17"})
		Expect(out.Disc).To(Equal(1))
		Expect(out.Track).To(Equal(7))
		Expect(out.Title).To(Equal("17"))
		Expect(out.Flags).To(ContainElement(FlagNoTitle))
	})

	It("flags a title that is only a number", func() {
		out := Clean(Local{Title: "13"})
		Expect(out.Title).To(Equal("13"))
		Expect(out.Track).To(Equal(0))
		Expect(out.Flags).To(ContainElement(FlagNoTitle))
	})

	It("takes the track number from a Track NN title, and flags it", func() {
		out := Clean(Local{Title: "Track 06"})
		Expect(out.Track).To(Equal(6))
		Expect(out.Flags).To(ContainElement(FlagNoTitle))
	})

	It("splits NN. Artist - Title (Year) under an unknown artist", func() {
		out := Clean(Local{Title: "05. Toni Basil - Mickey (1982)", Artist: "[Unknown Artist]"})
		Expect(out.Artist).To(Equal("Toni Basil"))
		Expect(out.Title).To(Equal("Mickey"))
		Expect(out.Year).To(Equal(1982))
		Expect(out.Track).To(Equal(5))
		Expect(out.Flags).NotTo(ContainElement(FlagUnknownArtist))
	})

	It("drops the artist's name from the front of the title", func() {
		out := Clean(Local{Title: "Bob Marley & The Wailers - Positive", Artist: "Bob Marley & the Wailers"})
		Expect(out.Title).To(Equal("Positive"))
	})

	It("drops the artist's name after a track number too", func() {
		out := Clean(Local{Title: "02 - Radiohead - Planet Telex", Artist: "Radiohead", Track: 2})
		Expect(out.Title).To(Equal("Planet Telex"))
	})

	It("keeps a dash when what is before it isn't the artist", func() {
		out := Clean(Local{Title: "Bach - Jesu, Joy of Man's Desiring", Artist: "Decca Orchestra"})
		Expect(out.Title).To(Equal("Bach - Jesu, Joy of Man's Desiring"))
	})

	It("drops a file extension", func() {
		Expect(Clean(Local{Title: "More Than A Feeling.mp3"}).Title).To(Equal("More Than A Feeling"))
		Expect(Clean(Local{Title: "I'm Burning For You.MP3"}).Title).To(Equal("I'm Burning For You"))
	})

	It("trims and collapses spaces", func() {
		out := Clean(Local{Title: "  Walk Like An  Egyptian ", Artist: "The Bangles ", Album: " Different  Light"})
		Expect(out.Title).To(Equal("Walk Like An Egyptian"))
		Expect(out.Artist).To(Equal("The Bangles"))
		Expect(out.Album).To(Equal("Different Light"))
	})

	It("turns underscores into spaces in a title taken from the file name", func() {
		out := Clean(Local{Title: "Rosa_Parks", Stem: "Rosa_Parks"})
		Expect(out.Title).To(Equal("Rosa Parks"))
		Expect(out.Flags).To(ContainElement(FlagSlug))
	})

	It("leaves underscores in a title that isn't the file name", func() {
		Expect(Clean(Local{Title: "Rosa_Parks", Stem: "06 Rosa Parks"}).Title).To(Equal("Rosa_Parks"))
	})

	It("unslugs a title taken from the file name, without the artist", func() {
		out := Clean(Local{Title: "02-sublime-sinsemilla-romeo", Stem: "02-sublime-sinsemilla-romeo", Artist: "Sublime"})
		Expect(out.Title).To(Equal("Sinsemilla Romeo"))
		Expect(out.Track).To(Equal(2))
		Expect(out.Flags).To(ContainElement(FlagSlug))
	})

	It("leaves a hyphenated title alone", func() {
		Expect(Clean(Local{Title: "Fu-gee-la", Stem: "Fu-gee-la", Artist: "Fugees"}).Title).To(Equal("Fu-gee-la"))
		Expect(Clean(Local{Title: "fu-gee-la", Stem: "fu-gee-la", Artist: "Fugees"}).Title).To(Equal("fu-gee-la"))
	})

	It("keeps a part number (9 of 11) in an audiobook's title", func() {
		out := Clean(Local{Title: "09 of 11 - The Sworn Sword", Stem: "09 09 of 11 - The Sworn Sword"})
		Expect(out.Title).To(Equal("09 of 11 - The Sworn Sword"))
		Expect(out.Flags).NotTo(ContainElement(FlagNumberKept))
	})

	It("splits NN - Artist - Title under an unknown artist", func() {
		out := Clean(Local{Title: "14- Dmx - Pull Out (Skit)", Artist: "[Unknown Artist]"})
		Expect(out.Artist).To(Equal("Dmx"))
		Expect(out.Title).To(Equal("Pull Out (Skit)"))
		Expect(out.Track).To(Equal(14))
	})

	It("leaves capitals alone", func() {
		Expect(Clean(Local{Title: "FEEL THE LOVE"}).Title).To(Equal("FEEL THE LOVE"))
	})

	Describe("filling empty fields", func() {
		It("takes the track number from the file name", func() {
			Expect(Clean(Local{Title: "Foo", Stem: "07 Foo"}).Track).To(Equal(7))
			Expect(Clean(Local{Title: "Foo", Stem: "07. Foo"}).Track).To(Equal(7))
		})
		It("doesn't take a number that may be part of the name", func() {
			Expect(Clean(Local{Title: "99 Luftballons", Stem: "99 Luftballons"}).Track).To(Equal(0))
		})
		It("takes disc and track numbers from the file name", func() {
			out := Clean(Local{Title: "Foo", Stem: "2-03 Foo"})
			Expect(out.Disc).To(Equal(2))
			Expect(out.Track).To(Equal(3))
		})
		It("never overwrites a track number", func() {
			Expect(Clean(Local{Title: "Foo", Stem: "07 Foo", Track: 5}).Track).To(Equal(5))
		})
		It("takes the disc number from a disc folder", func() {
			Expect(Clean(Local{Title: "Foo", Folder: "CD2"}).Disc).To(Equal(2))
			Expect(Clean(Local{Title: "Foo", Folder: "Disc 3"}).Disc).To(Equal(3))
		})
		It("takes the year from the album folder", func() {
			Expect(Clean(Local{Title: "Foo", Folder: "Rodriguez - Cold Fact (1970)"}).Year).To(Equal(1970))
			Expect(Clean(Local{Title: "Foo", Folder: "Cold Fact (1970)", Year: 1971}).Year).To(Equal(1971))
			Expect(Clean(Local{Title: "Foo", Folder: "Top 1000 (4.32gb)"}).Year).To(Equal(0))
		})
	})

	Describe("flags", func() {
		It("flags an unknown artist", func() {
			Expect(Clean(Local{Title: "Foo", Artist: "[Unknown Artist]"}).Flags).To(ContainElement(FlagUnknownArtist))
			Expect(Clean(Local{Title: "Foo", Artist: "Unknown"}).Flags).To(ContainElement(FlagUnknownArtist))
		})
		It("flags a missing album", func() {
			Expect(Clean(Local{Title: "Foo", Album: "Unknown Album"}).Flags).To(ContainElement(FlagNoAlbum))
			Expect(Clean(Local{Title: "Foo"}).Flags).To(ContainElement(FlagNoAlbum))
		})
		It("has none for a clean Track", func() {
			out := Clean(Local{Title: "Airbag", Artist: "Radiohead", Album: "OK Computer", Track: 1, Year: 1997})
			Expect(out.Flags).To(BeEmpty())
			Expect(out.Fixes).To(BeEmpty())
		})
	})

	DescribeTable("is idempotent",
		func(in Local) {
			once := Clean(in)
			twice := Clean(once.Local)
			Expect(twice.Local).To(Equal(once.Local))
			Expect(twice.Fixes).To(BeEmpty())
		},
		Entry(nil, Local{Title: "10 - Bones"}),
		Entry(nil, Local{Title: "05. Toni Basil - Mickey (1982)", Artist: "[Unknown Artist]"}),
		Entry(nil, Local{Title: "02-sublime-sinsemilla-romeo", Stem: "02-sublime-sinsemilla-romeo", Artist: "Sublime"}),
		Entry(nil, Local{Title: "Rosa_Parks", Stem: "Rosa_Parks"}),
		Entry(nil, Local{Title: "1-07 17"}),
		Entry(nil, Local{Title: "  Walk Like An  Egyptian.mp3 "}),
	)
})
