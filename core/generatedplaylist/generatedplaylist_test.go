package generatedplaylist_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core/generatedplaylist"
	"github.com/navidrome/navidrome/core/llm"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/persistence"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
)

// fakeLLM answers the filters call with a fixed reply and the ranking call by asking rank
// which candidate numbers to pick.
type fakeLLM struct {
	requests []llm.Request
	filters  map[string]any
	rank     func(candidates []string) []int
	err      error
}

func (f *fakeLLM) Complete(_ context.Context, req llm.Request, out any) error {
	f.requests = append(f.requests, req)
	if f.err != nil {
		return f.err
	}
	var reply any
	switch req.SchemaName {
	case "filters":
		reply = f.filters
	case "ranking":
		reply = map[string]any{"tracks": f.rank(candidateLines(req.User))}
	default:
		return fmt.Errorf("unexpected schema %q", req.SchemaName)
	}
	b, _ := json.Marshal(reply)
	return json.Unmarshal(b, out)
}

func (f *fakeLLM) request(schema string) llm.Request {
	for _, r := range f.requests {
		if r.SchemaName == schema {
			return r
		}
	}
	Fail("no " + schema + " request was sent")
	return llm.Request{}
}

var candidateLine = regexp.MustCompile(`^\d+\. `)

// candidateLines returns the numbered candidate lines of a ranking request, in order.
func candidateLines(user string) []string {
	var lines []string
	for _, l := range strings.Split(user, "\n") {
		if candidateLine.MatchString(l) {
			lines = append(lines, l)
		}
	}
	return lines
}

// fakeSimilar answers similar-artist lookups from a fixed map of artist name to Library
// artists, after delay, or fails with err.
type fakeSimilar struct {
	similar map[string][]string
	delay   time.Duration
	err     error
	mu      sync.Mutex
	asked   []string
}

func (f *fakeSimilar) SimilarArtists(ctx context.Context, name string, count int) (model.Artists, error) {
	f.mu.Lock()
	f.asked = append(f.asked, name)
	f.mu.Unlock()
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	var res model.Artists
	for _, n := range f.similar[name] {
		if len(res) == count {
			break
		}
		res = append(res, model.Artist{ID: "ar-" + n, Name: n})
	}
	return res, nil
}

func titles(mfs model.MediaFiles) []string {
	res := make([]string, len(mfs))
	for i, mf := range mfs {
		res[i] = mf.Title
	}
	return res
}

func firstN(n int) func([]string) []int {
	return func(c []string) []int {
		var res []int
		for i := 1; i <= n && i <= len(c); i++ {
			res = append(res, i)
		}
		return res
	}
}

var _ = Describe("Generated Playlists", Ordered, func() {
	var ds model.DataStore
	var ctx context.Context
	var fake *fakeLLM
	var similar *fakeSimilar
	var svc generatedplaylist.Service
	var listener model.User
	var seq int

	BeforeAll(func() {
		// db.Db() is a process-wide singleton, so the whole suite shares one database.
		DeferCleanup(configtest.SetupConfig())
		conf.Server.DbPath = filepath.Join(GinkgoT().TempDir(), "generated-playlists.db") + "?_journal_mode=WAL"
		DeferCleanup(db.Init(context.Background()))
		ds = persistence.New(db.Db())

		lib := model.Library{ID: 2, Name: "Other Library", Path: GinkgoT().TempDir()}
		Expect(ds.Library(context.Background()).Put(&lib)).To(Succeed())

		listener = model.User{ID: "listener-1", UserName: "listener", Name: "Listener", NewPassword: "pw"}
		Expect(ds.User(context.Background()).Put(&listener)).To(Succeed())
		Expect(ds.User(context.Background()).SetUserLibraries(listener.ID, []int{1})).To(Succeed())
		listener.Libraries = model.Libraries{{ID: 1}}
	})

	BeforeEach(func() {
		for _, table := range []string{"playlist_tracks", "playlist", "media_file", "library_tag", "tag"} {
			_, err := db.Db().Exec("delete from " + table)
			Expect(err).ToNot(HaveOccurred())
		}
		ctx = request.WithUser(context.Background(), listener)
		fake = &fakeLLM{
			filters: map[string]any{"name": "Rainy Sunday", "genres": []string{}, "seed_artists": []string{}, "year_from": 0, "year_to": 0},
			rank:    firstN(100),
		}
		similar = &fakeSimilar{}
		svc = generatedplaylist.New(ds, fake, similar)
	})

	// addTrack puts a Track in the Library (library 1 unless lib is given) and returns it.
	addTrack := func(artist, title string, year int, genre string, lib ...int) model.MediaFile {
		seq++
		libID := 1
		if len(lib) > 0 {
			libID = lib[0]
		}
		mf := model.MediaFile{
			ID: fmt.Sprintf("mf-%04d", seq), LibraryID: libID, Path: fmt.Sprintf("%s/%s-%d.mp3", artist, title, seq),
			Title: title, Artist: artist, AlbumArtist: artist, Album: title + " album", Year: year,
			Tags: model.Tags{},
		}
		if genre != "" {
			mf.Tags[model.TagGenre] = []string{genre}
			Expect(ds.Tag(ctx).Add(libID, model.NewTag(model.TagGenre, genre))).To(Succeed())
		}
		Expect(ds.MediaFile(ctx).Put(&mf)).To(Succeed())
		return mf
	}

	refreshGenres := func() {
		Expect(ds.Tag(ctx).UpdateCounts()).To(Succeed())
	}

	Describe("Enabled", func() {
		It("is enabled only with an LLM", func() {
			Expect(svc.Enabled()).To(BeTrue())
			Expect(generatedplaylist.New(ds, nil, nil).Enabled()).To(BeFalse())
		})
	})

	Describe("Preview", func() {
		It("refuses an empty Prompt", func() {
			_, err := svc.Preview(ctx, "   ", 25)
			Expect(err).To(MatchError(generatedplaylist.ErrInvalidPrompt))
			Expect(fake.requests).To(BeEmpty())
		})

		It("refuses a length outside 10 to 100", func() {
			_, err := svc.Preview(ctx, "indie", 9)
			Expect(err).To(MatchError(generatedplaylist.ErrInvalidLength))
			_, err = svc.Preview(ctx, "indie", 101)
			Expect(err).To(MatchError(generatedplaylist.ErrInvalidLength))
		})

		It("makes 25 Tracks when no length is given", func() {
			for i := range 40 {
				addTrack("Artist", fmt.Sprintf("Song %d", i), 2000, "Pop")
			}
			p, err := svc.Preview(ctx, "anything", 0)
			Expect(err).ToNot(HaveOccurred())
			Expect(p.Tracks).To(HaveLen(25))
		})

		It("tells the LLM which genres the Library has when turning the Prompt into filters", func() {
			addTrack("Pixies", "Debaser", 1989, "Indie Rock")
			addTrack("Madonna", "Vogue", 1990, "Pop")
			refreshGenres()

			_, err := svc.Preview(ctx, "rainy Sunday 90s indie", 10)
			Expect(err).ToNot(HaveOccurred())

			req := fake.request("filters")
			Expect(req.User).To(ContainSubstring("rainy Sunday 90s indie"))
			Expect(req.User).To(ContainSubstring("Indie Rock"))
			Expect(req.User).To(ContainSubstring("Pop"))
			Expect(req.Schema).To(HaveKey("properties"))
		})

		It("builds candidates from the Library that match the filters, one compact line each", func() {
			addTrack("Pixies", "Debaser", 1989, "Alternative")
			addTrack("Pixies", "Velouria", 1990, "Alternative")
			addTrack("Pavement", "Cut Your Hair", 1994, "Indie Rock")
			addTrack("Madonna", "Vogue", 1990, "Pop")
			addTrack("Arctic Monkeys", "Do I Wanna Know?", 2013, "Indie Rock")
			refreshGenres()
			fake.filters = map[string]any{"name": "Rainy 90s", "genres": []string{"indie rock"},
				"seed_artists": []string{"pixies"}, "year_from": 1990, "year_to": 1999}

			_, err := svc.Preview(ctx, "rainy Sunday 90s indie", 10)
			Expect(err).ToNot(HaveOccurred())

			// Best matches first: the seed artist, then the genre, within the years. Then, as
			// that's too few to choose from, the same without the years, then the years alone.
			Expect(candidateLines(fake.request("ranking").User)).To(Equal([]string{
				"1. Pixies - Velouria (1990, Alternative)",
				"2. Pavement - Cut Your Hair (1994, Indie Rock)",
				"3. Pixies - Debaser (1989, Alternative)",
				"4. Arctic Monkeys - Do I Wanna Know? (2013, Indie Rock)",
				"5. Madonna - Vogue (1990, Pop)",
			}))
		})

		It("offers Tracks by the seed artists' similar artists that are in the Library", func() {
			addTrack("Pixies", "Debaser", 1989, "")
			addTrack("Pavement", "Cut Your Hair", 1994, "")
			addTrack("Madonna", "Vogue", 1990, "")
			for i := range 30 {
				addTrack(fmt.Sprintf("Artist %d", i), fmt.Sprintf("Song %d", i), 2000, "")
			}
			// Genre tags are sparse: the LLM's genre finds nothing, and its seed artist isn't
			// in the Library.
			fake.filters = map[string]any{"name": "x", "genres": []string{"Indie Rock"},
				"seed_artists": []string{"Sonic Youth"}, "year_from": 0, "year_to": 0}
			similar.similar = map[string][]string{"Sonic Youth": {"Pixies", "Pavement"}}

			_, err := svc.Preview(ctx, "noisy 90s indie", 10)
			Expect(err).ToNot(HaveOccurred())

			Expect(similar.asked).To(Equal([]string{"Sonic Youth"}))
			lines := candidateLines(fake.request("ranking").User)
			// The similar artists come first, in Last.fm's order, before the fallback to anything.
			Expect(lines[:2]).To(Equal([]string{"1. Pixies - Debaser (1989)", "2. Pavement - Cut Your Hair (1994)"}))
		})

		It("takes the seed artists' similar artists in turn, once each, never a seed itself", func() {
			for _, artist := range []string{"Pixies", "Pavement", "Blur", "Pulp", "Madonna"} {
				addTrack(artist, artist+" Song", 1995, "")
			}
			fake.filters = map[string]any{"name": "x", "genres": []string{},
				"seed_artists": []string{"Sonic Youth", "Oasis"}, "year_from": 0, "year_to": 0}
			similar.similar = map[string][]string{
				"Sonic Youth": {"Pixies", "Pavement"},
				"Oasis":       {"Blur", "Pixies", "Sonic Youth", "Pulp"},
			}

			_, err := svc.Preview(ctx, "90s guitars", 10)
			Expect(err).ToNot(HaveOccurred())

			Expect(candidateLines(fake.request("ranking").User)[:4]).To(Equal([]string{
				"1. Pixies - Pixies Song (1995)",
				"2. Blur - Blur Song (1995)",
				"3. Pavement - Pavement Song (1995)",
				"4. Pulp - Pulp Song (1995)",
			}))
		})

		Context("when Last.fm is unreachable", func() {
			BeforeEach(func() {
				addTrack("Pixies", "Debaser", 1989, "Indie Rock")
				addTrack("Pavement", "Cut Your Hair", 1994, "Indie Rock")
				addTrack("Madonna", "Vogue", 1990, "Pop")
				refreshGenres()
				fake.filters = map[string]any{"name": "x", "genres": []string{"Indie Rock"},
					"seed_artists": []string{"Sonic Youth"}, "year_from": 0, "year_to": 0}
				similar.similar = map[string][]string{"Sonic Youth": {"Madonna"}}
			})

			It("builds the candidates from local tags alone", func() {
				similar.err = errors.New("dial tcp: no route to host")

				p, err := svc.Preview(ctx, "noisy 90s indie", 10)
				Expect(err).ToNot(HaveOccurred())
				Expect(p.Tracks).To(HaveLen(3))
				lines := candidateLines(fake.request("ranking").User)
				Expect(lines[2]).To(Equal("3. Madonna - Vogue (1990, Pop)"))
			})

			It("doesn't wait long for a lookup that hangs", func() {
				DeferCleanup(generatedplaylist.SetSimilarWait(50 * time.Millisecond))
				similar.delay = time.Minute

				start := time.Now()
				p, err := svc.Preview(ctx, "noisy 90s indie", 10)
				Expect(err).ToNot(HaveOccurred())
				Expect(time.Since(start)).To(BeNumerically("<", 2*time.Second))
				Expect(p.Tracks).To(HaveLen(3))
				lines := candidateLines(fake.request("ranking").User)
				Expect(lines[2]).To(Equal("3. Madonna - Vogue (1990, Pop)"))
			})
		})

		It("caps the candidates sent to the LLM at 150", func() {
			for i := range 400 {
				addTrack(fmt.Sprintf("Artist %d", i), fmt.Sprintf("Song %d", i), 1995, "Rock")
			}
			refreshGenres()
			fake.filters["genres"] = []string{"Rock"}

			_, err := svc.Preview(ctx, "rock", 100)
			Expect(err).ToNot(HaveOccurred())
			Expect(candidateLines(fake.request("ranking").User)).To(HaveLen(150))
		})

		It("spreads the candidates across artists when the Library allows", func() {
			for i := range 100 {
				addTrack("The Beatles", fmt.Sprintf("Abbey Road %d", i), 1988, "Pop")
				addTrack(fmt.Sprintf("Artist %d", i), fmt.Sprintf("Song %d", i), 1988, "Pop")
			}
			refreshGenres()
			fake.filters["genres"] = []string{"Pop"}

			_, err := svc.Preview(ctx, "pop", 25)
			Expect(err).ToNot(HaveOccurred())
			lines := candidateLines(fake.request("ranking").User)
			Expect(lines).To(HaveLen(75))
			beatles := 0
			for _, l := range lines {
				if strings.Contains(l, "The Beatles") {
					beatles++
				}
			}
			Expect(beatles).To(BeNumerically("<=", 7))
		})

		It("keeps the LLM's order, and discards Tracks that aren't candidates", func() {
			addTrack("Pavement", "Cut Your Hair", 1994, "Indie Rock")
			addTrack("Pavement", "Gold Soundz", 1994, "Indie Rock")
			addTrack("Pavement", "Range Life", 1994, "Indie Rock")
			refreshGenres()
			fake.filters["seed_artists"] = []string{"Pavement"}
			var lines []string
			fake.rank = func(c []string) []int {
				lines = c
				return []int{3, 999, 0, -1, 1, 3}
			}

			p, err := svc.Preview(ctx, "pavement", 10)
			Expect(err).ToNot(HaveOccurred())

			Expect(lines).To(HaveLen(3))
			want := func(n int) string { return strings.SplitN(strings.SplitN(lines[n-1], " - ", 2)[1], " (", 2)[0] }
			// Only 3 candidates exist, so the Playlist is shorter than asked. The one the LLM
			// left out is added at the end.
			Expect(titles(p.Tracks)).To(Equal([]string{want(3), want(1), want(2)}))
		})

		It("trims the LLM's pick to the length asked for, topping it up when it picks too few", func() {
			for i := range 30 {
				addTrack("Artist", fmt.Sprintf("Song %d", i), 2000, "Pop")
			}
			fake.rank = firstN(30)
			p, err := svc.Preview(ctx, "pop", 12)
			Expect(err).ToNot(HaveOccurred())
			Expect(p.Tracks).To(HaveLen(12))

			fake.rank = firstN(3)
			p, err = svc.Preview(ctx, "pop", 12)
			Expect(err).ToNot(HaveOccurred())
			Expect(p.Tracks).To(HaveLen(12))
		})

		It("gives one artist at most 3 Tracks in 25, even if the LLM picks more", func() {
			for i := range 20 {
				addTrack("Guns N' Roses", fmt.Sprintf("Appetite %d", i), 1987, "Rock")
			}
			for i := range 30 {
				addTrack(fmt.Sprintf("Artist %d", i), fmt.Sprintf("Song %d", i), 1987, "Rock")
			}
			fake.filters["seed_artists"] = []string{"Guns N' Roses"}
			fake.rank = func(c []string) []int {
				// Every Guns N' Roses candidate first, as a small model tends to.
				var gnr, rest []int
				for i, l := range c {
					if strings.Contains(l, "Guns N' Roses") {
						gnr = append(gnr, i+1)
					} else {
						rest = append(rest, i+1)
					}
				}
				return append(gnr, rest...)
			}

			p, err := svc.Preview(ctx, "80s rock", 25)
			Expect(err).ToNot(HaveOccurred())
			Expect(p.Tracks).To(HaveLen(25))
			gnr := 0
			for _, t := range p.Tracks {
				if t.Artist == "Guns N' Roses" {
					gnr++
				}
			}
			Expect(gnr).To(Equal(3))
		})

		It("still fills the Playlist from one artist when the Library has nothing else", func() {
			for i := range 15 {
				addTrack("Guns N' Roses", fmt.Sprintf("Appetite %d", i), 1987, "Rock")
			}
			p, err := svc.Preview(ctx, "80s rock", 10)
			Expect(err).ToNot(HaveOccurred())
			Expect(p.Tracks).To(HaveLen(10))
		})

		It("still finds Tracks when the filters match nothing in the Library", func() {
			for i := range 15 {
				addTrack("Artist", fmt.Sprintf("Song %d", i), 2000, "Pop")
			}
			refreshGenres()
			fake.filters = map[string]any{"name": "x", "genres": []string{"Polka"},
				"seed_artists": []string{"Nobody Real"}, "year_from": 1950, "year_to": 1955}

			p, err := svc.Preview(ctx, "polka", 10)
			Expect(err).ToNot(HaveOccurred())
			Expect(p.Tracks).To(HaveLen(10))
		})

		It("only offers Tracks the Listener can see, and never missing ones", func() {
			addTrack("Pavement", "Cut Your Hair", 1994, "Indie Rock")
			addTrack("Pavement", "Hidden", 1994, "Indie Rock", 2)
			gone := addTrack("Pavement", "Gone", 1994, "Indie Rock")
			Expect(ds.MediaFile(ctx).MarkMissing(true, &gone)).To(Succeed())
			fake.filters["seed_artists"] = []string{"Pavement"}

			p, err := svc.Preview(ctx, "pavement", 10)
			Expect(err).ToNot(HaveOccurred())
			Expect(titles(p.Tracks)).To(Equal([]string{"Cut Your Hair"}))
		})

		It("names the preview after the LLM's title, or the Prompt", func() {
			addTrack("Pavement", "Cut Your Hair", 1994, "Indie Rock")
			p, err := svc.Preview(ctx, "rainy Sunday 90s indie", 10)
			Expect(err).ToNot(HaveOccurred())
			Expect(p.Name).To(Equal("Rainy Sunday"))
			Expect(p.Prompt).To(Equal("rainy Sunday 90s indie"))

			fake.filters["name"] = "  "
			p, err = svc.Preview(ctx, "rainy Sunday 90s indie", 10)
			Expect(err).ToNot(HaveOccurred())
			Expect(p.Name).To(Equal("rainy Sunday 90s indie"))
		})

		It("reports an unreachable LLM as unavailable", func() {
			addTrack("Pavement", "Cut Your Hair", 1994, "Indie Rock")
			fake.err = fmt.Errorf("%w: dial tcp: i/o timeout", llm.ErrUnavailable)
			_, err := svc.Preview(ctx, "indie", 10)
			Expect(errors.Is(err, llm.ErrUnavailable)).To(BeTrue())
		})

		It("reports an empty Library", func() {
			_, err := svc.Preview(ctx, "indie", 10)
			Expect(err).To(MatchError(generatedplaylist.ErrNoTracks))
		})
	})

	Describe("Save", func() {
		It("creates the Listener's own ordinary Playlist that remembers its Prompt", func() {
			a := addTrack("Pavement", "Cut Your Hair", 1994, "Indie Rock")
			b := addTrack("Pixies", "Velouria", 1990, "Alternative")

			id, err := svc.Save(ctx, "Rainy Sunday", "rainy Sunday 90s indie", 25, []string{b.ID, a.ID})
			Expect(err).ToNot(HaveOccurred())

			pls, err := ds.Playlist(ctx).GetWithTracks(id, false, false)
			Expect(err).ToNot(HaveOccurred())
			Expect(pls.Name).To(Equal("Rainy Sunday"))
			Expect(pls.Prompt).To(Equal("rainy Sunday 90s indie"))
			Expect(pls.GeneratedLength).To(Equal(25))
			Expect(pls.OwnerID).To(Equal(listener.ID))
			Expect(pls.Public).To(BeFalse())
			Expect(pls.IsSmartPlaylist()).To(BeFalse())
			Expect(titles(pls.MediaFiles())).To(Equal([]string{"Velouria", "Cut Your Hair"}))
		})

		It("discards Tracks that aren't in the Listener's Library", func() {
			a := addTrack("Pavement", "Cut Your Hair", 1994, "Indie Rock")
			hidden := addTrack("Pavement", "Hidden", 1994, "Indie Rock", 2)

			id, err := svc.Save(ctx, "x", "indie", 25, []string{"made-up", a.ID, hidden.ID, a.ID})
			Expect(err).ToNot(HaveOccurred())
			pls, err := ds.Playlist(ctx).GetWithTracks(id, false, false)
			Expect(err).ToNot(HaveOccurred())
			Expect(titles(pls.MediaFiles())).To(Equal([]string{"Cut Your Hair"}))
		})

		It("refuses a Playlist with no real Tracks", func() {
			_, err := svc.Save(ctx, "x", "indie", 25, []string{"made-up"})
			Expect(err).To(MatchError(generatedplaylist.ErrNoTracks))
		})

		It("refuses more than 100 Tracks, or no Prompt", func() {
			a := addTrack("Pavement", "Cut Your Hair", 1994, "Indie Rock")
			ids := make([]string, 101)
			for i := range ids {
				ids[i] = a.ID
			}
			_, err := svc.Save(ctx, "x", "indie", 25, ids)
			Expect(err).To(MatchError(generatedplaylist.ErrInvalidLength))
			_, err = svc.Save(ctx, "x", " ", 25, []string{a.ID})
			Expect(err).To(MatchError(generatedplaylist.ErrInvalidPrompt))
		})

		It("refuses a length outside 10 to 100, and remembers 25 when none is given", func() {
			a := addTrack("Pavement", "Cut Your Hair", 1994, "Indie Rock")
			_, err := svc.Save(ctx, "x", "indie", 9, []string{a.ID})
			Expect(err).To(MatchError(generatedplaylist.ErrInvalidLength))
			_, err = svc.Save(ctx, "x", "indie", 101, []string{a.ID})
			Expect(err).To(MatchError(generatedplaylist.ErrInvalidLength))

			id, err := svc.Save(ctx, "x", "indie", 0, []string{a.ID})
			Expect(err).ToNot(HaveOccurred())
			pls, err := ds.Playlist(ctx).Get(id)
			Expect(err).ToNot(HaveOccurred())
			Expect(pls.GeneratedLength).To(Equal(25))
		})

		It("names the Playlist after its Prompt when no name is given", func() {
			a := addTrack("Pavement", "Cut Your Hair", 1994, "Indie Rock")
			id, err := svc.Save(ctx, "", "rainy Sunday 90s indie", 25, []string{a.ID})
			Expect(err).ToNot(HaveOccurred())
			pls, err := ds.Playlist(ctx).Get(id)
			Expect(err).ToNot(HaveOccurred())
			Expect(pls.Name).To(Equal("rainy Sunday 90s indie"))
		})
	})

	// ordinaryPlaylist saves a Playlist owned by ownerID that wasn't generated.
	ordinaryPlaylist := func(ownerID string, tracks ...model.MediaFile) string {
		pls := &model.Playlist{Name: "Ordinary", OwnerID: ownerID}
		pls.AddMediaFiles(tracks)
		Expect(ds.Playlist(ctx).Put(pls)).To(Succeed())
		return pls.ID
	}

	Describe("Regenerate", func() {
		It("previews the Playlist's stored Prompt at its stored length", func() {
			for i := range 40 {
				addTrack(fmt.Sprintf("Artist %d", i), fmt.Sprintf("Song %d", i), 2000, "Pop")
			}
			first := addTrack("Pavement", "Cut Your Hair", 1994, "Indie Rock")
			id, err := svc.Save(ctx, "Rainy Sunday", "rainy Sunday 90s indie", 12, []string{first.ID})
			Expect(err).ToNot(HaveOccurred())
			fake.requests = nil

			p, err := svc.Regenerate(ctx, id)
			Expect(err).ToNot(HaveOccurred())
			Expect(fake.request("filters").User).To(ContainSubstring("rainy Sunday 90s indie"))
			Expect(p.Prompt).To(Equal("rainy Sunday 90s indie"))
			Expect(p.Length).To(Equal(12))
			Expect(p.Tracks).To(HaveLen(12))
		})

		It("refuses an ordinary Playlist", func() {
			a := addTrack("Pavement", "Cut Your Hair", 1994, "Indie Rock")
			_, err := svc.Regenerate(ctx, ordinaryPlaylist(listener.ID, a))
			Expect(err).To(MatchError(generatedplaylist.ErrNotGenerated))
			Expect(fake.requests).To(BeEmpty())
		})

		It("reports a Playlist the Listener can't see as not found", func() {
			_, err := svc.Regenerate(ctx, "no-such-playlist")
			Expect(err).To(MatchError(model.ErrNotFound))
		})
	})

	Describe("Replace", func() {
		It("replaces the Tracks of the Listener's Generated Playlist, keeping its name and Prompt", func() {
			a := addTrack("Pavement", "Cut Your Hair", 1994, "Indie Rock")
			b := addTrack("Pixies", "Velouria", 1990, "Alternative")
			c := addTrack("Slowdive", "Alison", 1993, "Shoegaze")
			id, err := svc.Save(ctx, "Rainy Sunday", "rainy Sunday 90s indie", 10, []string{a.ID})
			Expect(err).ToNot(HaveOccurred())

			Expect(svc.Replace(ctx, id, []string{c.ID, "made-up", b.ID})).To(Succeed())

			pls, err := ds.Playlist(ctx).GetWithTracks(id, false, false)
			Expect(err).ToNot(HaveOccurred())
			Expect(titles(pls.MediaFiles())).To(Equal([]string{"Alison", "Velouria"}))
			Expect(pls.Name).To(Equal("Rainy Sunday"))
			Expect(pls.Prompt).To(Equal("rainy Sunday 90s indie"))
			Expect(pls.GeneratedLength).To(Equal(10))
			Expect(pls.SongCount).To(Equal(2))
		})

		It("refuses an ordinary Playlist", func() {
			a := addTrack("Pavement", "Cut Your Hair", 1994, "Indie Rock")
			id := ordinaryPlaylist(listener.ID, a)
			Expect(svc.Replace(ctx, id, []string{a.ID})).To(MatchError(generatedplaylist.ErrNotGenerated))
		})

		It("refuses another Listener's shared Generated Playlist", func() {
			a := addTrack("Pavement", "Cut Your Hair", 1994, "Indie Rock")
			other := model.User{ID: "listener-2", UserName: "other", Name: "Other", NewPassword: "pw"}
			Expect(ds.User(context.Background()).Put(&other)).To(Succeed())
			pls := &model.Playlist{Name: "Theirs", OwnerID: other.ID, Public: true, Prompt: "indie", GeneratedLength: 10}
			pls.AddMediaFiles(model.MediaFiles{a})
			Expect(ds.Playlist(ctx).Put(pls)).To(Succeed())

			Expect(svc.Replace(ctx, pls.ID, []string{a.ID})).To(MatchError(model.ErrNotAuthorized))
		})

		It("refuses no real Tracks, or more than 100", func() {
			a := addTrack("Pavement", "Cut Your Hair", 1994, "Indie Rock")
			id, err := svc.Save(ctx, "x", "indie", 10, []string{a.ID})
			Expect(err).ToNot(HaveOccurred())
			Expect(svc.Replace(ctx, id, []string{"made-up"})).To(MatchError(generatedplaylist.ErrNoTracks))
			ids := make([]string, 101)
			for i := range ids {
				ids[i] = a.ID
			}
			Expect(svc.Replace(ctx, id, ids)).To(MatchError(generatedplaylist.ErrInvalidLength))
		})
	})

	// Select is the same candidate approach, shared with Stations: their DJ's persona and Theme
	// are the request.
	Describe("Select", func() {
		var sel generatedplaylist.Selector

		BeforeEach(func() {
			sel = generatedplaylist.NewSelector(ds, fake, similar)
		})

		It("turns the request into filters, and returns the LLM's pick of matching Library Tracks in its order", func() {
			addTrack("Miles Davis", "Blue in Green", 1959, "Jazz")
			addTrack("John Coltrane", "Naima", 1959, "Jazz")
			addTrack("Madonna", "Vogue", 1990, "Pop")
			refreshGenres()
			fake.filters = map[string]any{"name": "", "genres": []string{"jazz"}, "seed_artists": []string{}, "year_from": 0, "year_to": 0}
			var offered []string
			fake.rank = func(c []string) []int {
				offered = c
				return []int{2, 1}
			}

			tracks, err := sel.Select(ctx, "Night Owl, low-key and late-night. Theme: late-night drive", nil, 2, nil)

			Expect(err).ToNot(HaveOccurred())
			Expect(fake.request("filters").User).To(ContainSubstring("Theme: late-night drive"))
			Expect(fake.request("ranking").User).To(ContainSubstring("Pick 2 Tracks"))
			// The genre's Tracks come first, in random order; the LLM picked the second, then the first.
			Expect(offered[:2]).To(ConsistOf(HaveSuffix("Blue in Green (1959, Jazz)"), HaveSuffix("Naima (1959, Jazz)")))
			Expect(tracks).To(HaveLen(2))
			Expect(offered[1]).To(HaveSuffix(tracks[0].Title + " (1959, Jazz)"))
			Expect(offered[0]).To(HaveSuffix(tracks[1].Title + " (1959, Jazz)"))
		})

		It("never offers the Tracks it is told to leave out", func() {
			kept := addTrack("Miles Davis", "Blue in Green", 1959, "Jazz")
			left := addTrack("John Coltrane", "Naima", 1959, "Jazz")

			tracks, err := sel.Select(ctx, "anything", nil, 5, []string{left.ID})

			Expect(err).ToNot(HaveOccurred())
			Expect(candidateLines(fake.request("ranking").User)).To(HaveLen(1))
			Expect(titles(tracks)).To(Equal([]string{kept.Title}))
		})

		It("logs, at info, the filters it used and the Tracks it picked", func() {
			l, hook := test.NewNullLogger()
			prevLogger := log.SetDefaultLogger(l)
			prevLevel := log.CurrentLevel()
			log.SetLevel(log.LevelInfo)
			DeferCleanup(func() {
				log.SetDefaultLogger(prevLogger)
				log.SetLevel(prevLevel)
			})
			addTrack("Miles Davis", "Blue in Green", 1959, "Jazz")
			addTrack("Madonna", "Vogue", 1990, "Pop")
			refreshGenres()
			fake.filters = map[string]any{"name": "", "genres": []string{"Jazz"}, "seed_artists": []string{"Chet Baker"},
				"year_from": 1950, "year_to": 1969}
			fake.rank = firstN(1)

			_, err := sel.Select(ctx, "late-night jazz", nil, 1, nil)
			Expect(err).ToNot(HaveOccurred())

			var picked []*logrus.Entry
			for _, e := range hook.AllEntries() {
				if e.Message == "LLM picked Tracks" {
					picked = append(picked, e)
				}
			}
			Expect(picked).To(HaveLen(1))
			Expect(picked[0].Level).To(Equal(logrus.InfoLevel))
			Expect(picked[0].Data).To(HaveKeyWithValue("genres", "[`Jazz`]"))
			Expect(picked[0].Data).To(HaveKeyWithValue("seedArtists", "[`Chet Baker`]"))
			Expect(picked[0].Data).To(HaveKeyWithValue("years", "1950-1969"))
			Expect(picked[0].Data).To(HaveKeyWithValue("picked", "[`Miles Davis - Blue in Green`]"))
		})

		// A Station's DJ with genres limits the candidates to them (#108).
		Describe("within genres", func() {
			var offered []string

			BeforeEach(func() {
				offered = nil
				fake.rank = func(c []string) []int {
					offered = c
					return []int{1}
				}
			})

			// addGenreTracks fills the Library with Tracks in and out of the genres, and some with
			// no genre at all, so the looser searches have plenty outside them to reach for.
			addGenreTracks := func() {
				for i := range 4 {
					addTrack("Miles Davis", fmt.Sprintf("Jazz %d", i), 1959, "Jazz")
					addTrack("Chet Baker", fmt.Sprintf("Cool %d", i), 1954, "Cool Jazz")
					addTrack("Madonna", fmt.Sprintf("Pop %d", i), 1990, "Pop")
					addTrack("Unknown", fmt.Sprintf("Untagged %d", i), 1990, "")
				}
				addTrack("Madonna", "Jazzy Vogue", 1990, "Jazz")
				refreshGenres()
			}

			It("offers only Tracks in the genres, however the LLM reads the request", func() {
				addGenreTracks()
				// The LLM reads the request as 80s and 90s pop by Madonna, outside the genres.
				fake.filters = map[string]any{"name": "", "genres": []string{"pop"}, "seed_artists": []string{"Madonna"},
					"year_from": 1985, "year_to": 1995}

				tracks, err := sel.Select(ctx, "Music for a Station hosted by the DJ Night Owl.", []string{"Jazz", "Cool Jazz"}, 5, nil)

				Expect(err).ToNot(HaveOccurred())
				// Every Jazz and Cool Jazz Track, the seed artist's first, and nothing else.
				Expect(offered).To(HaveLen(9))
				Expect(offered[0]).To(HaveSuffix("Madonna - Jazzy Vogue (1990, Jazz)"))
				for _, line := range offered {
					Expect(line).To(Or(HaveSuffix(", Jazz)"), HaveSuffix(", Cool Jazz)")))
				}
				Expect(tracks).To(HaveLen(5))
				// The LLM chooses its filters from the genres alone.
				Expect(fake.request("filters").User).To(HaveSuffix("Genres in the library: Jazz, Cool Jazz"))
			})

			It("lets a Theme narrow within the genres, never outside them", func() {
				addGenreTracks()
				// The Theme asks for cool jazz, and the LLM adds pop, outside the genres.
				fake.filters = map[string]any{"name": "", "genres": []string{"cool jazz", "pop"}, "seed_artists": []string{},
					"year_from": 0, "year_to": 0}

				_, err := sel.Select(ctx, "Music for a Station hosted by the DJ Night Owl.\nTheme: cool jazz", []string{"Jazz", "Cool Jazz"}, 5, nil)

				Expect(err).ToNot(HaveOccurred())
				// The Theme's Cool Jazz Tracks come first, then the rest of the genres' Tracks.
				Expect(offered).To(HaveLen(9))
				for _, line := range offered[:4] {
					Expect(line).To(HaveSuffix(", Cool Jazz)"))
				}
				for _, line := range offered[4:] {
					Expect(line).To(HaveSuffix(", Jazz)"))
				}
			})

			It("finds nothing when the Library has no Tracks in the genres", func() {
				addTrack("Madonna", "Vogue", 1990, "Pop")
				addTrack("Unknown", "Untagged", 1990, "")
				refreshGenres()

				_, err := sel.Select(ctx, "anything", []string{"Jazz"}, 5, nil)

				Expect(err).To(MatchError(generatedplaylist.ErrNoTracks))
			})
		})

		It("refuses a length outside 1 to 100", func() {
			_, err := sel.Select(ctx, "anything", nil, 0, nil)
			Expect(err).To(MatchError(generatedplaylist.ErrInvalidLength))
			_, err = sel.Select(ctx, "anything", nil, 101, nil)
			Expect(err).To(MatchError(generatedplaylist.ErrInvalidLength))
		})
	})
})
