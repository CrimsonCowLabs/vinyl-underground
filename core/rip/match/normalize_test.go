package match_test

import (
	"github.com/navidrome/navidrome/core/rip/match"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Guesses", func() {
	// first is the most likely reading of a video's artist and title.
	first := func(artist, title string) match.Guess {
		gs := match.Guesses(artist, title)
		Expect(gs).ToNot(BeEmpty())
		return gs[0]
	}

	DescribeTable("reads real-world YouTube titles",
		func(artist, title, wantArtist, wantTitle string) {
			g := first(artist, title)
			Expect(match.Key(g.Artist)).To(Equal(wantArtist))
			Expect(match.Key(g.Title)).To(Equal(wantTitle))
		},
		Entry("official video", "Daft Punk", "Daft Punk - One More Time (Official Video)", "daft punk", "one more time"),
		Entry("official music video", "Rick Astley", "Rick Astley - Never Gonna Give You Up (Official Music Video)", "rick astley", "never gonna give you up"),
		Entry("[HD] and a VEVO channel", "BeyonceVEVO", "Beyoncé - Halo [HD]", "beyonce", "halo"),
		Entry("a featuring credit after the noise", "Mark Ronson", "Mark Ronson - Uptown Funk (Official Video) ft. Bruno Mars", "mark ronson", "uptown funk"),
		Entry("(Lyrics)", "Hozier", "Hozier - Take Me To Church (Lyrics)", "hozier", "take me to church"),
		Entry("a Topic channel and (feat. …)", "Daft Punk - Topic", "Get Lucky (feat. Pharrell Williams)", "daft punk", "get lucky"),
		Entry("accents", "Sigur Rós", "Hoppípolla", "sigur ros", "hoppipolla"),
		Entry("punctuation in a name", "AC/DC", "AC/DC - Back In Black (Official Video)", "ac dc", "back in black"),
		Entry("& in a name", "Simon & Garfunkel", "Simon & Garfunkel - The Sound of Silence (Audio)", "simon and garfunkel", "the sound of silence"),
		Entry("an en dash and a visualizer", "Some Channel", "Phoebe Bridgers – Kyoto [Official Visualizer]", "phoebe bridgers", "kyoto"),
		Entry("a | suffix", "Adele", "Adele - Hello | Official Music Video", "adele", "hello"),
		Entry("4K and an official channel", "Queen Official", "Queen - Bohemian Rhapsody (Official Video) [4K]", "queen", "bohemian rhapsody"),
		Entry("a VEVO channel alone", "TaylorSwiftVEVO", "Shake It Off", "taylorswift", "shake it off"),
		Entry("a hyphen inside a name is no split", "JAY-Z", "Empire State Of Mind", "jay z", "empire state of mind"),
		Entry("the artist's feat. credit", "Calvin Harris feat. Rihanna", "This Is What You Came For", "calvin harris", "this is what you came for"),
	)

	It("keeps a remaster or live version apart: it is another recording", func() {
		Expect(match.Key(first("Queen", "Bohemian Rhapsody (Remastered 2011)").Title)).To(Equal("bohemian rhapsody remastered 2011"))
		Expect(match.Key(first("Nirvana", "Lithium (Live)").Title)).To(Equal("lithium live"))
	})

	It("also tries the whole title with the given artist when the title has a dash", func() {
		gs := match.Guesses("Radiohead", "Creep - Acoustic")
		Expect(gs).To(HaveLen(2))
		Expect(match.Key(gs[1].Artist)).To(Equal("radiohead"))
		Expect(match.Key(gs[1].Title)).To(Equal("creep acoustic"))
	})

	It("has no guess without a title", func() {
		Expect(match.Guesses("Artist", " (Official Video) ")).To(BeEmpty())
	})
})

var _ = Describe("Key", func() {
	DescribeTable("normalises case, accents, punctuation and featuring credits",
		func(in, want string) { Expect(match.Key(in)).To(Equal(want)) },
		Entry("case", "ONE More time", "one more time"),
		Entry("accents", "Café Del Mar", "cafe del mar"),
		Entry("atomic letters", "Bjørk", "bjork"),
		Entry("apostrophes join", "Don't Stop Me Now", "dont stop me now"),
		Entry("punctuation", "Hello, Goodbye!", "hello goodbye"),
		Entry("&", "Florence + the Machine & Co", "florence the machine and co"),
		Entry("feat.", "Get Lucky feat. Pharrell", "get lucky"),
		Entry("(ft …)", "Get Lucky (ft Pharrell)", "get lucky"),
		Entry("featuring", "Song featuring Someone Else", "song"),
		Entry("empty", "  ", ""),
	)
})
