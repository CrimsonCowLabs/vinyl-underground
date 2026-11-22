package stations_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/navidrome/navidrome/core/stations"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/persistence"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeSelector stands in for the LLM candidate approach: pick decides which Tracks a request
// gets. Until release is closed (when set), it hangs like a slow LLM.
type fakeSelector struct {
	mu       sync.Mutex
	requests []string
	excludes [][]string
	pick     func(request string) model.MediaFiles
	release  chan struct{}
	err      error
}

func (f *fakeSelector) Select(_ context.Context, request string, length int, exclude []string) (model.MediaFiles, error) {
	f.mu.Lock()
	f.requests = append(f.requests, request)
	f.excludes = append(f.excludes, exclude)
	release := f.release
	f.mu.Unlock()
	if release != nil {
		<-release
	}
	if f.err != nil {
		return nil, f.err
	}
	var res model.MediaFiles
	for _, mf := range f.pick(request) {
		if !contains(exclude, mf.ID) && len(res) < length {
			res = append(res, mf)
		}
	}
	return res, nil
}

func (f *fakeSelector) asked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

func contains(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

var _ = Describe("Stations with an LLM", Ordered, func() {
	var ds model.DataStore
	var sel *fakeSelector
	var svc stations.Service
	var ctx context.Context
	var seq int

	BeforeAll(func() {
		ds = persistence.New(db.Db())
		carol := model.User{ID: "carol-id", UserName: "carol", Name: "carol", NewPassword: "pw"}
		Expect(ds.User(context.Background()).Put(&carol)).To(Succeed())
		Expect(ds.User(context.Background()).SetUserLibraries(carol.ID, []int{1})).To(Succeed())
		carol.Libraries = model.Libraries{{ID: 1}}
		ctx = request.WithUser(context.Background(), carol)
	})

	BeforeEach(func() {
		for _, table := range []string{"station_track", "annotation", "media_file"} {
			_, err := db.Db().Exec("delete from " + table)
			Expect(err).ToNot(HaveOccurred())
		}
		sel = &fakeSelector{pick: func(string) model.MediaFiles { return nil }}
		svc = stations.New(ds, sel, nil)
	})

	// track makes a Track, by its own artist, named after the kind.
	track := func(kind string) model.MediaFile {
		seq++
		return model.MediaFile{
			ID: fmt.Sprintf("%s-%04d", kind, seq), LibraryID: 1, Path: fmt.Sprintf("%s/%d.mp3", kind, seq),
			Title: fmt.Sprintf("%s %d", kind, seq), Artist: fmt.Sprintf("%s artist %d", kind, seq),
			AlbumArtist: "Various", Album: "Album", Tags: model.Tags{},
		}
	}

	// addTracks puts n Tracks in the Library, for rule-based selection to choose from.
	addTracks := func(n int) {
		for range n {
			mf := track("library")
			Expect(ds.MediaFile(ctx).Put(&mf)).To(Succeed())
		}
	}

	// llmPicks makes n Tracks for the fake LLM to pick. They are kept out of the database, so
	// rule-based selection can never serve them and only the LLM's picks do.
	llmPicks := func(kind string, n int) model.MediaFiles {
		var res model.MediaFiles
		for range n {
			res = append(res, track(kind))
		}
		return res
	}

	// listen serves the Station's first Tracks (rule-based, while the LLM selects), lets its
	// selection finish, and serves the next.
	listen := func(djID, theme string, count int) model.MediaFiles {
		_, err := svc.Next(ctx, djID, theme, stations.DefaultCount)
		Expect(err).ToNot(HaveOccurred())
		stations.WaitForSelection(svc)
		tracks, err := svc.Next(ctx, djID, theme, count)
		Expect(err).ToNot(HaveOccurred())
		return tracks
	}

	BeforeEach(func() {
		addTracks(20)
	})

	It("asks for Tracks with the DJ's persona and the Theme", func() {
		listen("night-owl", "  late-night drive ", 3)

		owl, _ := stations.FindDJ("night-owl")
		Expect(sel.asked()).ToNot(BeEmpty())
		req := sel.asked()[0]
		Expect(req).To(ContainSubstring(owl.Name))
		Expect(req).To(ContainSubstring(owl.Description))
		Expect(req).To(ContainSubstring("Theme: late-night drive"))
	})

	It("asks with the persona of the Listener's own DJ, and starts over when they edit it", func() {
		_, err := db.Db().Exec("delete from dj")
		Expect(err).ToNot(HaveOccurred())
		mine, err := svc.CreateDJ(ctx, stations.DJEdit{Name: "Mr. Mellow", Description: "Slow, soft songs for a rainy afternoon."})
		Expect(err).ToNot(HaveOccurred())
		mellow, loud := llmPicks("mellow", 8), llmPicks("loud", 8)
		sel.pick = func(req string) model.MediaFiles {
			switch {
			case strings.Contains(req, "Mr. Mellow") && strings.Contains(req, "rainy afternoon"):
				return mellow
			case strings.Contains(req, "Mr. Loud") && strings.Contains(req, "Fast and loud"):
				return loud
			}
			return nil
		}

		Expect(ids(listen(mine.ID, "", 3))).To(Equal(ids(mellow[:3])))
		stations.WaitForSelection(svc)

		_, err = svc.UpdateDJ(ctx, mine.ID, stations.DJEdit{Name: "Mr. Loud", Description: "Fast and loud."})
		Expect(err).ToNot(HaveOccurred())
		Expect(ids(listen(mine.ID, "", 3))).To(Equal(ids(loud[:3])))
	})

	It("asks with the DJ's persona alone when no Theme is given", func() {
		listen("sunny", "", 3)
		Expect(sel.asked()[0]).To(ContainSubstring("Sunny"))
		Expect(sel.asked()[0]).ToNot(ContainSubstring("Theme"))
	})

	It("plays the LLM's picks in its order, so different DJs and Themes play different Tracks", func() {
		drive := llmPicks("drive", 8)
		morning := llmPicks("morning", 8)
		sel.pick = func(req string) model.MediaFiles {
			switch {
			case strings.Contains(req, "Theme: late-night drive"):
				return drive
			case strings.Contains(req, "Sunny"):
				return morning
			}
			return nil
		}

		Expect(ids(listen("night-owl", "late-night drive", 4))).To(Equal(ids(drive[:4])))
		Expect(ids(listen("sunny", "", 4))).To(Equal(ids(morning[:4])))
	})

	It("never waits on the LLM: the first Tracks are rule-based while it selects", func() {
		sel.release = make(chan struct{})
		DeferCleanup(func() { stations.WaitForSelection(svc) })
		defer close(sel.release)

		done := make(chan model.MediaFiles)
		go func() {
			defer GinkgoRecover()
			tracks, err := svc.Next(ctx, "night-owl", "rain", 5)
			Expect(err).ToNot(HaveOccurred())
			done <- tracks
		}()
		Eventually(done, time.Second).Should(Receive(HaveLen(5)))
		Eventually(sel.asked).Should(HaveLen(1))
	})

	It("selects ahead again before its picks run out, leaving out what it already has", func() {
		picks := llmPicks("pick", 40)
		sel.pick = func(string) model.MediaFiles { return picks }

		first := listen("night-owl", "rain", 3)
		Expect(ids(first)).To(Equal(ids(picks[:3])))
		for range 3 {
			_, err := svc.Next(ctx, "night-owl", "rain", 3)
			Expect(err).ToNot(HaveOccurred())
			stations.WaitForSelection(svc)
		}
		Expect(len(sel.asked())).To(BeNumerically(">", 1))
		for _, ex := range sel.excludes[1:] {
			Expect(ex).To(ContainElements(ids(first)))
		}
	})

	// picksByDJ has the fake LLM pick n Tracks of its own for each named DJ, whatever the Theme.
	picksByDJ := func(n int, djNames ...string) map[string]model.MediaFiles {
		picks := map[string]model.MediaFiles{}
		for _, name := range djNames {
			picks[name] = llmPicks(name, n)
		}
		sel.pick = func(req string) model.MediaFiles {
			for name, tracks := range picks {
				if strings.Contains(req, "DJ "+name+".") {
					return tracks
				}
			}
			return nil
		}
		return picks
	}

	// owlAndSunny has the fake LLM pick n Tracks for each of two built-in DJs.
	owlAndSunny := func(n int) (owl, sunny model.MediaFiles) {
		picks := picksByDJ(n, "Night Owl", "Sunny")
		return picks["Night Owl"], picks["Sunny"]
	}

	// next serves the Station's next Tracks and lets any selection it started finish. Unlike
	// listen, it serves from whatever is planned already.
	next := func(djID, theme string, count int) model.MediaFiles {
		tracks, err := svc.Next(ctx, djID, theme, count)
		Expect(err).ToNot(HaveOccurred())
		stations.WaitForSelection(svc)
		return tracks
	}

	It("keeps each DJ's picks when the Listener switches Station and back", func() {
		owl, sunny := owlAndSunny(10)

		Expect(ids(listen("night-owl", "rain", 3))).To(Equal(ids(owl[:3])))
		stations.WaitForSelection(svc)
		Expect(ids(listen("sunny", "", 3))).To(Equal(ids(sunny[:3])))
		stations.WaitForSelection(svc)

		Expect(ids(next("night-owl", "rain", 3))).To(Equal(ids(owl[3:6])))
		Expect(ids(next("sunny", "", 3))).To(Equal(ids(sunny[3:6])))
	})

	It("starts afresh only for the DJ whose Theme changed", func() {
		owl, sunny := owlAndSunny(10)
		listen("night-owl", "rain", 3)
		stations.WaitForSelection(svc)
		listen("sunny", "", 3)
		stations.WaitForSelection(svc)

		Expect(ids(next("night-owl", "sunshine", 3))).ToNot(ContainElement(BeElementOf(ids(owl))))
		Expect(ids(next("sunny", "", 3))).To(Equal(ids(sunny[3:6])))
	})

	It("starts afresh only for the DJ the Listener edited", func() {
		_, err := db.Db().Exec("delete from dj")
		Expect(err).ToNot(HaveOccurred())
		mine, err := svc.CreateDJ(ctx, stations.DJEdit{Name: "Mr. Mellow", Description: "Slow, soft songs for a rainy afternoon."})
		Expect(err).ToNot(HaveOccurred())
		picks := picksByDJ(10, "Mr. Mellow", "Sunny")
		mellow, sunny := picks["Mr. Mellow"], picks["Sunny"]
		listen(mine.ID, "", 3)
		stations.WaitForSelection(svc)
		listen("sunny", "", 3)
		stations.WaitForSelection(svc)

		// Only the description changes, so the DJ is still Mr. Mellow to the fake LLM.
		_, err = svc.UpdateDJ(ctx, mine.ID, stations.DJEdit{Name: "Mr. Mellow", Description: "Fast and loud."})
		Expect(err).ToNot(HaveOccurred())

		Expect(ids(next(mine.ID, "", 3))).ToNot(ContainElement(BeElementOf(ids(mellow))))
		Expect(ids(next("sunny", "", 3))).To(Equal(ids(sunny[3:6])))
	})

	It("keeps a Track and an artist from repeating, as rule-based selection does", func() {
		picks := llmPicks("pick", 4)
		heard := time.Now().Add(-time.Hour)
		picks[1].PlayDate = &heard // the Listener played it an hour ago
		sameArtist := track("pick")
		sameArtist.Artist = picks[0].Artist
		sel.pick = func(string) model.MediaFiles {
			return model.MediaFiles{picks[0], picks[1], sameArtist, picks[0], picks[2], picks[3]}
		}

		tracks := listen("night-owl", "rain", 3)

		Expect(ids(tracks)).To(Equal([]string{picks[0].ID, picks[2].ID, picks[3].ID}))
	})

	It("starts afresh when the Theme changes, never playing the old Theme's picks", func() {
		rain := llmPicks("rain", 10)
		sel.pick = func(req string) model.MediaFiles {
			if strings.Contains(req, "Theme: rain") {
				return rain
			}
			return nil
		}
		listen("night-owl", "rain", 3)

		tracks, err := svc.Next(ctx, "night-owl", "sunshine", 5)
		stations.WaitForSelection(svc)

		Expect(err).ToNot(HaveOccurred())
		Expect(tracks).To(HaveLen(5))
		Expect(ids(tracks)).ToNot(ContainElement(BeElementOf(ids(rain))))
	})

	It("falls back to rule-based selection when the LLM fails", func() {
		sel.err = errors.New("AI unavailable")
		tracks := listen("sunny", "rain", 5)
		Expect(tracks).To(HaveLen(5))
	})

	It("refuses a Theme over 200 characters", func() {
		_, err := svc.Next(ctx, "sunny", strings.Repeat("x", 201), 3)
		Expect(err).To(MatchError(stations.ErrInvalidTheme))
	})

	It("ignores the Theme without an LLM", func() {
		noLLM := stations.New(ds, nil, nil)
		tracks, err := noLLM.Next(ctx, "sunny", "late-night drive", 3)
		Expect(err).ToNot(HaveOccurred())
		Expect(tracks).To(HaveLen(3))
		_, err = noLLM.Next(ctx, "sunny", strings.Repeat("x", 201), 3)
		Expect(err).ToNot(HaveOccurred())
		Expect(sel.asked()).To(BeEmpty())
	})

	It("never asks the LLM twice at once for one Listener, even when they switch Station", func() {
		sel.release = make(chan struct{})
		_, err := svc.Next(ctx, "night-owl", "rain", 3)
		Expect(err).ToNot(HaveOccurred())
		_, err = svc.Next(ctx, "sunny", "", 3)
		Expect(err).ToNot(HaveOccurred())
		Consistently(sel.asked, 100*time.Millisecond).Should(HaveLen(1))

		close(sel.release)
		stations.WaitForSelection(svc)
		_, err = svc.Next(ctx, "sunny", "", 3)
		Expect(err).ToNot(HaveOccurred())
		stations.WaitForSelection(svc)
		Expect(sel.asked()).To(HaveLen(2))
		Expect(sel.asked()[1]).To(ContainSubstring("Sunny"))
	})

	It("keeps a selection's picks for its own DJ when the Listener switched Station meanwhile", func() {
		owl, _ := owlAndSunny(10)
		sel.release = make(chan struct{})
		_, err := svc.Next(ctx, "night-owl", "rain", 3)
		Expect(err).ToNot(HaveOccurred())
		Eventually(sel.asked).Should(HaveLen(1))
		_, err = svc.Next(ctx, "sunny", "", 3)
		Expect(err).ToNot(HaveOccurred())

		close(sel.release)
		stations.WaitForSelection(svc)
		Expect(ids(next("night-owl", "rain", 3))).To(Equal(ids(owl[:3])))
	})
})

var _ = Describe("Starting a Station with an LLM", Ordered, func() {
	var ds model.DataStore
	var sel *fakeSelector
	var svc stations.Service
	var ctx context.Context
	var seq int
	var jazz []string // the Library's Tracks in Night Owl's genres

	BeforeAll(func() {
		ds = persistence.New(db.Db())
		dave := model.User{ID: "dave-id", UserName: "dave", Name: "dave", NewPassword: "pw"}
		Expect(ds.User(context.Background()).Put(&dave)).To(Succeed())
		Expect(ds.User(context.Background()).SetUserLibraries(dave.ID, []int{1})).To(Succeed())
		dave.Libraries = model.Libraries{{ID: 1}}
		ctx = request.WithUser(context.Background(), dave)
	})

	// track makes a Track, by its own artist, named after the kind, in the genre.
	track := func(kind, genre string) model.MediaFile {
		seq++
		return model.MediaFile{
			ID: fmt.Sprintf("start-%s-%04d", kind, seq), LibraryID: 1, Path: fmt.Sprintf("start/%s/%d.mp3", kind, seq),
			Title: fmt.Sprintf("%s %d", kind, seq), Artist: fmt.Sprintf("%s artist %d", kind, seq),
			AlbumArtist: "Various", Album: "Album", Genre: genre,
			Tags: model.Tags{model.TagGenre: []string{genre}},
		}
	}

	// picks makes n Tracks for the fake LLM to pick, kept out of the database so only the
	// LLM can serve them.
	picks := func(kind string, n int) model.MediaFiles {
		var res model.MediaFiles
		for range n {
			res = append(res, track(kind, "Pop"))
		}
		return res
	}

	// addInGenre puts n Tracks in the genre in the Library, and returns their IDs.
	addInGenre := func(n int, genre string) []string {
		Expect(ds.Tag(ctx).Add(1, model.NewTag(model.TagGenre, genre))).To(Succeed())
		var res []string
		for range n {
			mf := track("library", genre)
			Expect(ds.MediaFile(ctx).Put(&mf)).To(Succeed())
			res = append(res, mf.ID)
		}
		return res
	}

	BeforeEach(func() {
		for _, table := range []string{"station_track", "annotation", "media_file", "dj"} {
			_, err := db.Db().Exec("delete from " + table)
			Expect(err).ToNot(HaveOccurred())
		}
		sel = &fakeSelector{pick: func(string) model.MediaFiles { return nil }}
		svc = stations.New(ds, sel, nil)
		// Long enough that a start which waits never looks like one that starts at once.
		stations.SetStartWait(svc, 10*time.Second)
		DeferCleanup(func() { stations.WaitForSelection(svc) })
		for _, table := range []string{"library_tag", "tag"} {
			_, err := db.Db().Exec("delete from " + table)
			Expect(err).ToNot(HaveOccurred())
		}
		jazz = addInGenre(50, "Cool Jazz")
		addInGenre(30, "Polka")
	})

	// start starts the Station in the background, sending its first Tracks on the channel.
	start := func(djID string, count int) chan model.MediaFiles {
		done := make(chan model.MediaFiles, 1)
		go func() {
			defer GinkgoRecover()
			tracks, err := svc.Start(ctx, djID, "", count)
			Expect(err).ToNot(HaveOccurred())
			done <- tracks
		}()
		return done
	}

	It("waits for the LLM's picks when none are ready, then starts with them", func() {
		owl := picks("owl", 10)
		sel.pick = func(string) model.MediaFiles { return owl }
		sel.release = make(chan struct{})

		done := start("night-owl", 5)
		Consistently(done, 100*time.Millisecond).ShouldNot(Receive())
		close(sel.release)

		var tracks model.MediaFiles
		Eventually(done).Should(Receive(&tracks))
		Expect(ids(tracks)).To(Equal(ids(owl[:5])))
	})

	It("starts by rule, on the DJ's genres, past the wait, and plays the LLM's picks once they land", func() {
		owl := picks("owl", 10)
		sel.pick = func(string) model.MediaFiles { return owl }
		sel.release = make(chan struct{})

		var tracks model.MediaFiles
		stations.SetStartWait(svc, 300*time.Millisecond)
		Eventually(start("night-owl", 5), 3*time.Second).Should(Receive(&tracks))
		Expect(tracks).To(HaveLen(5))
		Expect(ids(tracks)).To(HaveEach(BeElementOf(jazz)))

		close(sel.release)
		stations.WaitForSelection(svc)
		next, err := svc.Next(ctx, "night-owl", "", 3)
		Expect(err).ToNot(HaveOccurred())
		Expect(ids(next)).To(Equal(ids(owl[:3])))
	})

	It("starts at once with the LLM's picks when they are ready", func() {
		owl := picks("owl", 10)
		sel.pick = func(string) model.MediaFiles { return owl }
		_, err := svc.Start(ctx, "night-owl", "", 3) // plans Night Owl's picks
		Expect(err).ToNot(HaveOccurred())
		stations.WaitForSelection(svc)
		sel.release = make(chan struct{}) // the LLM hangs from now on
		DeferCleanup(func() { close(sel.release) })

		var tracks model.MediaFiles
		Eventually(start("night-owl", 5), 2*time.Second).Should(Receive(&tracks))
		Expect(ids(tracks)).To(Equal(ids(owl[3:8])))
	})

	It("starts at once on the DJ's genres when the LLM failed in the last couple of minutes", func() {
		sel.err = errors.New("AI unavailable")
		_, err := svc.Start(ctx, "sunny", "", 3) // waits for the failure
		Expect(err).ToNot(HaveOccurred())
		Expect(sel.asked()).To(HaveLen(1))

		sel.release = make(chan struct{})
		DeferCleanup(func() { close(sel.release) })
		var tracks model.MediaFiles
		Eventually(start("night-owl", 5), 2*time.Second).Should(Receive(&tracks))
		Expect(ids(tracks)).To(HaveEach(BeElementOf(jazz)))
		Expect(sel.asked()).To(HaveLen(1))
	})

	It("starts at once on the DJ's genres without an LLM", func() {
		svc = stations.New(ds, nil, nil)
		stations.SetStartWait(svc, time.Hour)

		var tracks model.MediaFiles
		Eventually(start("night-owl", 5), 2*time.Second).Should(Receive(&tracks))
		Expect(tracks).To(HaveLen(5))
		Expect(ids(tracks)).To(HaveEach(BeElementOf(jazz)))
	})

	It("waits for the picks of the Listener's own DJ too", func() {
		mine, err := svc.CreateDJ(ctx, stations.DJEdit{Name: "Mr. Mellow", Description: "Slow, soft songs for a rainy afternoon."})
		Expect(err).ToNot(HaveOccurred())
		mellow := picks("mellow", 10)
		sel.pick = func(string) model.MediaFiles { return mellow }
		sel.release = make(chan struct{})

		done := start(mine.ID, 5)
		Consistently(done, 100*time.Millisecond).ShouldNot(Receive())
		close(sel.release)

		var tracks model.MediaFiles
		Eventually(done).Should(Receive(&tracks))
		Expect(ids(tracks)).To(Equal(ids(mellow[:5])))
	})

	It("still gets its DJ's picks when the Listener switched from a Station whose selection is under way", func() {
		owl, sunny := picks("owl", 10), picks("sunny", 10)
		sel.pick = func(req string) model.MediaFiles {
			if strings.Contains(req, "DJ Sunny.") {
				return sunny
			}
			return owl
		}
		stations.SetStartWait(svc, 5*time.Second)
		sel.release = make(chan struct{})
		owlDone := start("night-owl", 3)
		Eventually(sel.asked).Should(HaveLen(1))

		sunnyDone := start("sunny", 5)
		Consistently(sunnyDone, 100*time.Millisecond).ShouldNot(Receive())
		close(sel.release)

		Eventually(owlDone).Should(Receive())
		var tracks model.MediaFiles
		Eventually(sunnyDone).Should(Receive(&tracks))
		Expect(ids(tracks)).To(Equal(ids(sunny[:5])))
	})
})

var _ = Describe("Plans", func() {
	var ps *stations.Plans
	var owl, sunny stations.DJ
	var picks model.MediaFiles
	var start time.Time

	BeforeEach(func() {
		ps = &stations.Plans{}
		picks = nil
		for i := range 10 {
			picks = append(picks, model.MediaFile{ID: fmt.Sprintf("pick-%d", i), Artist: fmt.Sprintf("artist %d", i)})
		}
		sel := &fakeSelector{pick: func(string) model.MediaFiles { return picks }}
		owl, _ = stations.FindDJ("night-owl")
		sunny, _ = stations.FindDJ("sunny")
		start = time.Now()

		ps.Take("carol-id", owl, "", start, 3) // starts the plan
		ps.SelectAhead(context.Background(), sel, "carol-id", owl, "")
	})

	It("keeps a plan a while after its Station was last played", func() {
		later := start.Add(stations.PlanIdle - time.Minute)
		ps.Take("carol-id", sunny, "", later, 3)
		Expect(ids(ps.Take("carol-id", owl, "", later, 3))).To(Equal(ids(picks[:3])))
	})

	It("drops a plan unused for a long while", func() {
		later := start.Add(stations.PlanIdle + time.Minute)
		ps.Take("carol-id", sunny, "", later, 3)
		Expect(ps.Take("carol-id", owl, "", later, 3)).To(BeEmpty())
	})
})
