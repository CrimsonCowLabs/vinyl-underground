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
		mine, err := svc.CreateDJ(ctx, "Mr. Mellow", "Slow, soft songs for a rainy afternoon.")
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

		_, err = svc.UpdateDJ(ctx, mine.ID, "Mr. Loud", "Fast and loud.")
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
})
