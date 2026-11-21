package propose_test

import (
	"github.com/navidrome/navidrome/core/rip/propose"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("StartFrom", func() {
	DescribeTable("cleans up a video's title when there is no YouTube Music metadata",
		func(uploader, title, wantArtist, wantTitle string) {
			s := propose.StartFrom(propose.Input{Artist: uploader, Title: title})
			Expect(s.From).To(Equal(propose.FromTitle))
			Expect(s.Artist).To(Equal(wantArtist))
			Expect(s.Title).To(Equal(wantTitle))
		},
		Entry("(Official Video)", "Daft Punk", "Daft Punk - One More Time (Official Video)", "Daft Punk", "One More Time"),
		Entry("[HD] and a VEVO channel", "BeyonceVEVO", "Beyoncé - Halo [HD]", "Beyoncé", "Halo"),
		Entry("(Lyrics)", "Some Lyrics Channel", "Hozier - Take Me To Church (Lyrics)", "Hozier", "Take Me To Church"),
		Entry("a bare trailing Lyrics", "7clouds", "Hozier - Take Me To Church Lyrics", "Hozier", "Take Me To Church"),
		Entry("a bare trailing Official Music Video", "Adele", "Adele - Hello Official Music Video", "Adele", "Hello"),
		Entry("an en dash and [Official Visualizer]", "Some Channel", "Phoebe Bridgers – Kyoto [Official Visualizer]", "Phoebe Bridgers", "Kyoto"),
		Entry("a | suffix", "Adele", "Adele - Hello | Official Music Video", "Adele", "Hello"),
		Entry("a Topic channel and no dash", "Daft Punk - Topic", "Get Lucky", "Daft Punk", "Get Lucky"),
		Entry("no dash: the channel is the artist", "Hozier", "Too Sweet (Official Video)", "Hozier", "Too Sweet"),
		Entry("a hyphen in a name is no split", "JAY-Z", "Empire State Of Mind", "JAY-Z", "Empire State Of Mind"),
		Entry("a live version keeps its Live", "Nirvana", "Nirvana - Lithium (Live)", "Nirvana", "Lithium (Live)"),
		Entry("a song called Music is kept", "Madonna", "Madonna - Music", "Madonna", "Music"),
		Entry("Pop Music keeps its Music", "M", "M - Pop Music", "M", "Pop Music"),
		Entry("noise beside a remaster in one bracket", "Queen Official", "Queen – Bohemian Rhapsody (Official Video Remastered)", "Queen", "Bohemian Rhapsody (Remastered)"),
		Entry("noise beside Live in one bracket", "Nirvana", "Nirvana - Lithium (Live Official Video)", "Nirvana", "Lithium (Live)"),
		Entry("punctuation left in a cleaned bracket", "Band", "Band - Song (Official Video - Remastered)", "Band", "Song (Remastered)"),
		Entry("a song that ends in Official", "Band", "Band - Make It Official", "Band", "Make It Official"),
		Entry("a song that ends in HD alone is cut only with a second noise word", "Band", "Band - Song HD Video", "Band", "Song"),
	)

	It("starts from YouTube Music's own metadata when the upload is a Topic one", func() {
		s := propose.StartFrom(propose.Input{
			Artist: "Daft Punk - Topic", Title: "Get Lucky (feat. Pharrell Williams & Nile Rodgers)",
			YouTubeMusic: &propose.YouTubeMusic{
				Artists: []string{"Daft Punk", "Pharrell Williams", "Nile Rodgers"},
				Artist:  "Daft Punk, Pharrell Williams, Nile Rodgers", Track: "Get Lucky (feat. Pharrell Williams & Nile Rodgers)",
				Album: "Random Access Memories", Year: 2013,
			},
		})
		Expect(s).To(Equal(propose.Start{
			Artist: "Daft Punk, Pharrell Williams, Nile Rodgers", Title: "Get Lucky (feat. Pharrell Williams & Nile Rodgers)",
			AlbumArtist: "Daft Punk", Album: "Random Access Memories", Year: 2013, From: propose.FromYouTubeMusic,
		}))
	})

	It("falls back to the title when YouTube Music gives no artist or track", func() {
		s := propose.StartFrom(propose.Input{
			Artist: "Adele", Title: "Adele - Hello (Official Video)",
			YouTubeMusic: &propose.YouTubeMusic{Album: "25"},
		})
		Expect(s.From).To(Equal(propose.FromTitle))
		Expect(s.Title).To(Equal("Hello"))
	})

	It("keeps an upload's own album and year (for Files that already have tags)", func() {
		s := propose.StartFrom(propose.Input{Artist: "Adele", Title: "Hello", Album: "25", Year: 2015})
		Expect(s.Album).To(Equal("25"))
		Expect(s.Year).To(Equal(2015))
	})

	It("keeps a title that is all noise as it is, rather than proposing nothing", func() {
		s := propose.StartFrom(propose.Input{Artist: "Someone", Title: "(Official Video)"})
		Expect(s.Title).To(Equal("(Official Video)"))
	})

	Describe("a File's own tags (an upload or a dropped File, #35)", func() {
		It("reads a tagged title with a dash whole first, as the File's artist tag is the artist", func() {
			starts := propose.StartsFrom(propose.Input{Artist: "Radiohead", Title: "Creep - Acoustic", Tagged: true})
			Expect(starts[0].From).To(Equal(propose.FromTags))
			Expect(starts[0].Artist).To(Equal("Radiohead"))
			Expect(starts[0].Title).To(Equal("Creep - Acoustic"))
			Expect(starts[0].AlbumArtist).To(Equal("Radiohead"))
		})

		It("still takes YouTube noise out of a tagged File's title (a File ripped elsewhere)", func() {
			s := propose.StartFrom(propose.Input{Artist: "Adele", Title: "Hello (Official Video)", Tagged: true})
			Expect(s.Title).To(Equal("Hello"))
			Expect(s.From).To(Equal(propose.FromTags))
		})

		It("keeps a tagged title that already names its artist as it reads best", func() {
			s := propose.StartFrom(propose.Input{Artist: "AdeleVEVO", Title: "Adele - Hello", Tagged: true})
			Expect(s.Artist).To(Equal("Adele"))
			Expect(s.Title).To(Equal("Hello"))
		})
	})
})
