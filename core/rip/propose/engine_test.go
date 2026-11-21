package propose_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/navidrome/navidrome/core/musicbrainz"
	"github.com/navidrome/navidrome/core/rip/propose"
	"github.com/navidrome/navidrome/model"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fixtureMB answers recording searches from MusicBrainz JSON recorded into testdata/ (pruned to
// the fields the engine reads), or from made-up recordings for the cases no recording shows. A
// search it has neither for finds nothing.
type fixtureMB struct {
	searches map[string]string
	madeUp   map[string][]musicbrainz.Recording
	err      error
	queries  []string
}

func (f *fixtureMB) SearchRecordings(_ context.Context, query string) ([]musicbrainz.Recording, error) {
	f.queries = append(f.queries, query)
	if f.err != nil {
		return nil, f.err
	}
	if recs, ok := f.madeUp[query]; ok {
		return recs, nil
	}
	var res struct {
		Recordings []musicbrainz.Recording `json:"recordings"`
	}
	if name, ok := f.searches[query]; ok {
		b, err := os.ReadFile(filepath.Join("core", "rip", "propose", "testdata", name))
		Expect(err).ToNot(HaveOccurred())
		Expect(json.Unmarshal(b, &res)).To(Succeed())
	}
	return res.Recordings, nil
}

var _ = Describe("Engine", func() {
	ctx := context.Background()

	It("picks the earliest studio album over the single it was first released on, and every compilation", func() {
		// Recorded: Rick Astley's recording is on 500+ Various Artists compilations, his Greatest
		// Hits, soundtracks, the 1987 single and the album Whenever You Need Somebody (1987, and
		// reissues up to 2022).
		mb := &fixtureMB{searches: map[string]string{
			propose.SearchQuery("Rick Astley", "Never Gonna Give You Up"): "search-never-gonna-give-you-up.json",
		}}
		p := propose.New(mb).Propose(ctx, propose.Input{
			Artist: "Rick Astley", Title: "Rick Astley - Never Gonna Give You Up (Official Music Video)",
			Duration: 213 * time.Second,
		})
		Expect(p.Status).To(Equal(model.RipProposalReady))
		Expect(p.Title).To(Equal("Never Gonna Give You Up"))
		Expect(p.Album).To(Equal("Whenever You Need Somebody"))
		Expect(p.ReleaseType).To(Equal("Album"))
		Expect(p.Year).To(Equal("1987"))
		Expect(p.AlbumArtist).To(Equal("Rick Astley"))
		Expect(p.AlbumFrom).To(Equal(propose.AlbumFromMusicBrainz))
		Expect(p.ReleaseGroupID).To(Equal("082c6aff-a7cc-36e0-a960-35a578ecd937"))
	})

	It("finds the studio album of a Topic upload whose recording is credited with its featured artists", func() {
		// Recorded: the album recording is credited "Daft Punk feat. Pharrell Williams and Nile
		// Rodgers" and scores only 71, below the radio edit's single; covers and remixes score higher.
		mb := &fixtureMB{searches: map[string]string{
			propose.SearchQuery("Daft Punk", "Get Lucky"): "search-get-lucky.json",
		}}
		p := propose.New(mb).Propose(ctx, propose.Input{Artist: "Daft Punk - Topic", Title: "Get Lucky (feat. Pharrell Williams & Nile Rodgers)",
			YouTubeMusic: &propose.YouTubeMusic{Artist: "Daft Punk, Pharrell Williams, Nile Rodgers",
				Artists: []string{"Daft Punk", "Pharrell Williams", "Nile Rodgers"},
				Track:   "Get Lucky (feat. Pharrell Williams & Nile Rodgers)", Album: "Random Access Memories", Year: 2013}})
		Expect(p.Album).To(Equal("Random Access Memories"))
		Expect(p.ReleaseType).To(Equal("Album"))
		Expect(p.Year).To(Equal("2013"))
		Expect(p.AlbumArtist).To(Equal("Daft Punk"))
		Expect(p.From).To(Equal(string(propose.FromYouTubeMusic)))
		Expect(p.Title).To(Equal("Get Lucky (feat. Pharrell Williams & Nile Rodgers)"))
	})

	It("picks the single for a song that was only ever a single", func() {
		// Recorded: the single, Various Artists compilations, and other artists' remixes.
		mb := &fixtureMB{searches: map[string]string{
			propose.SearchQuery("Childish Gambino", "This Is America"): "search-this-is-america.json",
		}}
		p := propose.New(mb).Propose(ctx, propose.Input{
			Artist: "ChildishGambinoVEVO", Title: "Childish Gambino - This Is America (Official Video)",
		})
		Expect(p.Artist).To(Equal("Childish Gambino"))
		Expect(p.Title).To(Equal("This Is America"))
		Expect(p.Album).To(Equal("This Is America"))
		Expect(p.ReleaseType).To(Equal("Single"))
		Expect(p.AlbumArtist).To(Equal("Childish Gambino"))
		Expect(p.Year).To(Equal("2018"))
		Expect(p.From).To(Equal(string(propose.FromTitle)))
	})

	It("uses the Track's own title as the album when nothing is found", func() {
		mb := &fixtureMB{searches: map[string]string{
			propose.SearchQuery("Nobody Qqzx", "Zzqx Unfindable Wibble"): "search-nothing.json",
		}}
		p := propose.New(mb).Propose(ctx, propose.Input{Artist: "Nobody Qqzx", Title: "Zzqx Unfindable Wibble (Official Video)"})
		Expect(p.Status).To(Equal(model.RipProposalReady))
		Expect(p.Album).To(Equal("Zzqx Unfindable Wibble"))
		Expect(p.AlbumArtist).To(Equal("Nobody Qqzx"))
		Expect(p.AlbumFrom).To(Equal(propose.AlbumFromTitle))
		Expect(p.Year).To(BeEmpty())
		Expect(p.Note).To(BeEmpty())
	})

	It("never proposes a compilation, live or remix release: a search finding only those finds nothing", func() {
		// Recorded (without the status and type filters): Various Artists compilations, DJ-mixes
		// and promotional releases only.
		mb := &fixtureMB{searches: map[string]string{
			propose.SearchQuery("Rick Astley", "Never Gonna Give You Up"): "search-compilations-only.json",
		}}
		p := propose.New(mb).Propose(ctx, propose.Input{Artist: "Rick Astley", Title: "Never Gonna Give You Up"})
		Expect(p.Album).To(Equal("Never Gonna Give You Up"))
		Expect(p.AlbumFrom).To(Equal(propose.AlbumFromTitle))
	})

	It("falls back to YouTube Music's album and year when MusicBrainz finds nothing for a Topic upload", func() {
		p := propose.New(&fixtureMB{}).Propose(ctx, propose.Input{Artist: "Band - Topic", Title: "Song",
			YouTubeMusic: &propose.YouTubeMusic{Artist: "Band", Track: "Song", Album: "Record", Year: 2001}})
		Expect(p.From).To(Equal(string(propose.FromYouTubeMusic)))
		Expect(p.Album).To(Equal("Record"))
		Expect(p.AlbumFrom).To(Equal(propose.AlbumFromYouTubeMusic))
		Expect(p.Year).To(Equal("2001"))
	})

	It("searches for the main artist, without featured artists, with quotes escaped", func() {
		mb := &fixtureMB{}
		propose.New(mb).Propose(ctx, propose.Input{
			YouTubeMusic: &propose.YouTubeMusic{Artist: "Daft Punk, Pharrell Williams", Track: `Get "Lucky"`}})
		Expect(mb.queries).To(HaveLen(1))
		Expect(mb.queries[0]).To(HavePrefix(`recording:"Get \"Lucky\"" AND artist:"Daft Punk" AND status:official`))
	})

	DescribeTable("searches for the song itself, without featured artists or a remaster note",
		func(title, searched string) {
			Expect(propose.SearchQuery("Band", title)).To(HavePrefix(`recording:"` + searched + `" AND artist:"Band"`))
		},
		Entry("a featuring credit", "Get Lucky (feat. Pharrell Williams & Nile Rodgers)", "Get Lucky"),
		Entry("a remaster", "Bohemian Rhapsody (Remastered)", "Bohemian Rhapsody"),
		Entry("a dated remaster", "Heroes - 2017 Remaster", "Heroes"),
		Entry("a remastered version", "Song (Remastered 2011 Version)", "Song"),
		Entry("a live version stays live", "Lithium (Live)", "Lithium (Live)"),
		Entry("a remix stays a remix", "Song (Remix)", "Song (Remix)"),
	)

	It("matches the recording without its remaster note, and keeps the note in the proposed title", func() {
		mb := &fixtureMB{searches: map[string]string{
			propose.SearchQuery("Rick Astley", "Never Gonna Give You Up"): "search-never-gonna-give-you-up.json",
		}}
		p := propose.New(mb).Propose(ctx, propose.Input{Artist: "Rick Astley", Title: "Never Gonna Give You Up (Remastered 2022)"})
		Expect(p.Album).To(Equal("Whenever You Need Somebody"))
		Expect(p.Title).To(Equal("Never Gonna Give You Up (Remastered 2022)"))
	})

	// album is a made-up recording of title by artist on an official studio album.
	album := func(artist, title, albumTitle, date string) musicbrainz.Recording {
		credit := musicbrainz.ArtistCredits{{Name: artist}}
		return musicbrainz.Recording{ID: "rec", Title: title, Score: 100, ArtistCredit: credit,
			Releases: []musicbrainz.Release{{ID: "rel", Title: albumTitle, Status: "Official", Date: date,
				ArtistCredit: credit, ReleaseGroup: musicbrainz.ReleaseGroup{ID: "rg", Title: albumTitle, PrimaryType: "Album"}}}}
	}

	It("reads a dashed title whole, with the uploader as the artist, when MusicBrainz knows only that", func() {
		mb := &fixtureMB{madeUp: map[string][]musicbrainz.Recording{
			propose.SearchQuery("Radiohead", "Creep - Acoustic"): {album("Radiohead", "Creep - Acoustic", "My Iron Lung", "1994")},
		}}
		p := propose.New(mb).Propose(ctx, propose.Input{Artist: "Radiohead", Title: "Creep - Acoustic"})
		Expect(mb.queries).To(HaveLen(2), "the split reading first, then the whole title")
		Expect(p.Artist).To(Equal("Radiohead"))
		Expect(p.Title).To(Equal("Creep - Acoustic"))
		Expect(p.Album).To(Equal("My Iron Lung"))
	})

	It("keeps the split reading when MusicBrainz knows neither", func() {
		p := propose.New(&fixtureMB{}).Propose(ctx, propose.Input{Artist: "Some Lyrics Channel", Title: "Band - Song"})
		Expect(p.Artist).To(Equal("Band"))
		Expect(p.Title).To(Equal("Song"))
	})

	It("matches a MusicBrainz recording whose own title has a remaster note", func() {
		mb := &fixtureMB{madeUp: map[string][]musicbrainz.Recording{
			propose.SearchQuery("David Bowie", "Heroes"): {album("David Bowie", "Heroes - 2017 Remaster", "Heroes", "1977")},
		}}
		p := propose.New(mb).Propose(ctx, propose.Input{Artist: "David Bowie", Title: "Heroes (2017 Remaster)"})
		Expect(p.Album).To(Equal("Heroes"))
		Expect(p.Year).To(Equal("1977"))
	})

	It("searches for a duo by its whole name, and compares it by the first", func() {
		mb := &fixtureMB{madeUp: map[string][]musicbrainz.Recording{
			propose.SearchQuery("Simon & Garfunkel", "The Boxer"): {album("Simon & Garfunkel", "The Boxer", "Bridge Over Troubled Water", "1970")},
		}}
		p := propose.New(mb).Propose(ctx, propose.Input{Artist: "Simon & Garfunkel", Title: "Simon & Garfunkel - The Boxer (Audio)"})
		Expect(mb.queries[0]).To(ContainSubstring(`artist:"Simon & Garfunkel"`))
		Expect(p.Album).To(Equal("Bridge Over Troubled Water"))
		Expect(p.AlbumArtist).To(Equal("Simon & Garfunkel"))
	})

	It("still proposes from the title, with a note, when MusicBrainz can't be reached", func() {
		mb := &fixtureMB{err: musicbrainz.ErrBusy}
		p := propose.New(mb).Propose(ctx, propose.Input{Artist: "Adele", Title: "Adele - Hello (Official Video)"})
		Expect(p.Status).To(Equal(model.RipProposalReady))
		Expect(p.Title).To(Equal("Hello"))
		Expect(p.Album).To(Equal("Hello"))
		Expect(p.Note).To(ContainSubstring("busy"))
		Expect(p.LookupFailed).To(BeTrue(), "so the next start tries again")
	})

	It("proposes from the title alone, with no lookups, when MusicBrainz is off", func() {
		p := propose.New(nil).Propose(ctx, propose.Input{Artist: "Adele", Title: "Adele - Hello (Official Video)"})
		Expect(p.Status).To(Equal(model.RipProposalReady))
		Expect(p.Album).To(Equal("Hello"))
		Expect(p.Note).To(ContainSubstring("off"))
	})
})
