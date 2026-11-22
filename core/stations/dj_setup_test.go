package stations_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/navidrome/navidrome/core/stations"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/persistence"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Setting up a Listener's DJ", Ordered, func() {
	var ds model.DataStore
	var sel *fakeSelector
	var genreLLM *fakeGenreLLM
	var svc stations.Service
	var ctx context.Context
	var seq int
	var house []string      // the Library's Tracks in House
	var houseWords []string // and in the genres "house" names by word: House and Deep House

	BeforeAll(func() {
		ds = persistence.New(db.Db())
		hal := model.User{ID: "hal-id", UserName: "hal", Name: "hal", NewPassword: "pw"}
		Expect(ds.User(context.Background()).Put(&hal)).To(Succeed())
		Expect(ds.User(context.Background()).SetUserLibraries(hal.ID, []int{1})).To(Succeed())
		hal.Libraries = model.Libraries{{ID: 1}}
		ctx = request.WithUser(context.Background(), hal)
	})

	track := func(kind, genre string) model.MediaFile {
		seq++
		return model.MediaFile{
			ID: fmt.Sprintf("setup-%s-%04d", kind, seq), LibraryID: 1, Path: fmt.Sprintf("setup/%s/%d.mp3", kind, seq),
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
			res = append(res, track(kind, "House"))
		}
		return res
	}

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
		for _, table := range []string{"station_track", "station_skip", "annotation", "media_file", "library_tag", "tag", "dj"} {
			_, err := db.Db().Exec("delete from " + table)
			Expect(err).ToNot(HaveOccurred())
		}
		house = addInGenre(50, "House")
		addInGenre(30, "Polka")
		houseWords = append(addInGenre(1, "Deep House"), house...)
		sel = &fakeSelector{pick: func(string) model.MediaFiles { return nil }}
		genreLLM = &fakeGenreLLM{}
		genreLLM.answer("House")
		svc = stations.New(ds, sel, nil, stations.WithGenreLLM(genreLLM))
		// Long enough that a start which waits never looks like one that starts at once.
		stations.SetStartWait(svc, 10*time.Second)
		stations.SetSetupTimes(svc, 5*time.Second, time.Hour, time.Hour)
		DeferCleanup(func() {
			stations.StopSetups(svc)
			stations.WaitForSelection(svc)
		})
	})

	// startNow starts the DJ's Station, failing when it waits for the LLM.
	startNow := func(djID string) model.MediaFiles {
		done := make(chan model.MediaFiles, 1)
		go func() {
			defer GinkgoRecover()
			tracks, err := svc.Start(ctx, djID, "", 5)
			Expect(err).ToNot(HaveOccurred())
			done <- tracks
		}()
		var tracks model.MediaFiles
		Eventually(done, time.Second).Should(Receive(&tracks))
		return tracks
	}

	It("plans the first hour of LLM picks from its genres, so its Station starts with them at once", func() {
		pump := picks("pump", 15)
		sel.pick = func(string) model.MediaFiles { return pump }

		dj, err := svc.CreateDJ(ctx, stations.DJEdit{Name: "Pump", Description: "Pump the house music"})
		Expect(err).ToNot(HaveOccurred())
		Expect(svc.AwaitSetup(ctx, dj.ID)).To(Equal(stations.SetupReady))
		Expect(sel.askedGenres()).To(Equal([][]string{{"House"}}))
		stations.WaitForSetups(svc)
		Expect(svc.AwaitSetup(ctx, dj.ID)).To(Equal(stations.SetupReady)) // asked again once done

		sel.release = make(chan struct{}) // the LLM hangs from now on
		DeferCleanup(func() { close(sel.release) })
		Expect(ids(startNow(dj.ID))).To(Equal(ids(pump[:5])))
	})

	// create creates a DJ Pump, mapped to House by the LLM.
	create := func() stations.DJ {
		dj, err := svc.CreateDJ(ctx, stations.DJEdit{Name: "Pump", Description: "Pump the house music"})
		Expect(err).ToNot(HaveOccurred())
		return dj
	}

	It("sets up an edited DJ too, from its new persona", func() {
		dj := create()
		Expect(svc.AwaitSetup(ctx, dj.ID)).To(Equal(stations.SetupSettingUp)) // the fake LLM picked nothing
		loud := picks("loud", 15)
		sel.mu.Lock()
		sel.pick = func(req string) model.MediaFiles {
			if strings.Contains(req, "Loud and proud") {
				return loud
			}
			return nil
		}
		sel.mu.Unlock()

		_, err := svc.UpdateDJ(ctx, dj.ID, stations.DJEdit{Name: "Pump", Description: "Loud and proud house"})
		Expect(err).ToNot(HaveOccurred())
		Expect(svc.AwaitSetup(ctx, dj.ID)).To(Equal(stations.SetupReady))
		Expect(ids(startNow(dj.ID))).To(Equal(ids(loud[:5])))
	})

	It("plays a starter of rule-based Tracks on the DJ's genres when the LLM is slow, then its picks after them", func() {
		pump := picks("pump", 15)
		sel.pick = func(string) model.MediaFiles { return pump }
		sel.release = make(chan struct{})
		stations.SetSetupTimes(svc, 300*time.Millisecond, time.Hour, time.Hour)

		dj := create()
		Expect(svc.AwaitSetup(ctx, dj.ID)).To(Equal(stations.SetupSettingUp))
		starter := startNow(dj.ID)
		Expect(starter).To(HaveLen(5))
		Expect(ids(starter)).To(HaveEach(BeElementOf(house)))

		close(sel.release)
		stations.WaitForSelection(svc)
		next, err := svc.Next(ctx, dj.ID, "", 3)
		Expect(err).ToNot(HaveOccurred())
		Expect(ids(next)).To(Equal(ids(pump[:3])))
	})

	It("plays a starter at once when the LLM can't be reached", func() {
		sel.failWith(errors.New("AI unavailable"))

		dj := create()
		Expect(svc.AwaitSetup(ctx, dj.ID)).To(Equal(stations.SetupSettingUp))
		Expect(sel.asked()).To(HaveLen(1))
		starter := startNow(dj.ID)
		Expect(starter).To(HaveLen(5))
		Expect(ids(starter)).To(HaveEach(BeElementOf(house)))
	})

	It("doesn't ask for picks when mapping the DJ's genres just failed", func() {
		genreLLM.fail()

		dj := create()
		Expect(svc.AwaitSetup(ctx, dj.ID)).To(Equal(stations.SetupSettingUp))
		Expect(sel.asked()).To(BeEmpty())
		Expect(ids(startNow(dj.ID))).To(HaveEach(BeElementOf(houseWords)))
	})

	It("keeps asking the LLM in the background, putting its picks after the starter", func() {
		stations.SetSetupTimes(svc, 5*time.Second, 50*time.Millisecond, 5*time.Second)
		sel.failWith(errors.New("AI unavailable"))
		dj := create()
		Expect(svc.AwaitSetup(ctx, dj.ID)).To(Equal(stations.SetupSettingUp))

		pump := picks("pump", 15)
		sel.mu.Lock()
		sel.pick = func(string) model.MediaFiles { return pump }
		sel.mu.Unlock()
		sel.failWith(nil)
		stations.WaitForSetups(svc)
		Expect(len(sel.asked())).To(BeNumerically(">=", 2))

		starter := startNow(dj.ID)
		Expect(ids(starter)).To(HaveEach(BeElementOf(house)))
		next, err := svc.Next(ctx, dj.ID, "", 3)
		Expect(err).ToNot(HaveOccurred())
		Expect(ids(next)).To(Equal(ids(pump[:3])))
	})

	It("maps the DJ's genres again in the background when the LLM was down at the save", func() {
		stations.SetSetupTimes(svc, 5*time.Second, 50*time.Millisecond, 5*time.Second)
		genreLLM.fail()
		pump := picks("pump", 15)
		sel.pickIn = func(_ string, genres []string) model.MediaFiles {
			if slices.Equal(genres, []string{"Deep House"}) {
				return pump
			}
			return nil
		}
		dj := create()
		Expect(dj.Genres).To(ConsistOf("House", "Deep House")) // matched by word
		Expect(svc.AwaitSetup(ctx, dj.ID)).To(Equal(stations.SetupSettingUp))

		genreLLM.answer("Deep House")
		stations.WaitForSetups(svc)
		got, err := svc.DJ(ctx, dj.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Genres).To(Equal([]string{"Deep House"}))

		starter := startNow(dj.ID)
		Expect(ids(starter)).To(HaveEach(BeElementOf(houseWords))) // carried into the new genres' plan
		next, err := svc.Next(ctx, dj.ID, "", 3)
		Expect(err).ToNot(HaveOccurred())
		Expect(ids(next)).To(Equal(ids(pump[:3])))
	})

	It("stops asking the LLM once the retry window is over", func() {
		stations.SetSetupTimes(svc, 5*time.Second, 50*time.Millisecond, 400*time.Millisecond)
		sel.failWith(errors.New("AI unavailable"))
		dj := create()
		Expect(svc.AwaitSetup(ctx, dj.ID)).To(Equal(stations.SetupSettingUp))

		stations.WaitForSetups(svc)
		tries := len(sel.asked())
		Expect(tries).To(BeNumerically(">=", 3))
		Expect(tries).To(BeNumerically("<=", 8))
		Expect(svc.AwaitSetup(ctx, dj.ID)).To(Equal(stations.SetupNone))
		Consistently(sel.asked, 200*time.Millisecond).Should(HaveLen(tries))

		// The Station-start wait takes over: the next start waits for the LLM's picks.
		pump := picks("pump", 15)
		sel.mu.Lock()
		sel.pick = func(string) model.MediaFiles { return pump }
		sel.release = make(chan struct{})
		sel.mu.Unlock()
		sel.failWith(nil)
		done := make(chan model.MediaFiles, 1)
		go func() {
			defer GinkgoRecover()
			tracks, err := svc.Start(ctx, dj.ID, "", 5)
			Expect(err).ToNot(HaveOccurred())
			done <- tracks
		}()
		Consistently(done, 100*time.Millisecond).ShouldNot(Receive())
		close(sel.release)
		var tracks model.MediaFiles
		Eventually(done, 2*time.Second).Should(Receive(&tracks))
		Expect(ids(tracks)).To(Equal(ids(pump[:5])))
	})

	It("waits for a selection still running when the setup step ran out, then is ready", func() {
		stations.SetSetupTimes(svc, 300*time.Millisecond, time.Hour, time.Hour)
		pump := picks("pump", 15)
		sel.pick = func(string) model.MediaFiles { return pump }
		sel.release = make(chan struct{})
		dj := create()
		Expect(svc.AwaitSetup(ctx, dj.ID)).To(Equal(stations.SetupSettingUp))

		close(sel.release)
		stations.WaitForSetups(svc) // long before the hour's retry
		Expect(svc.AwaitSetup(ctx, dj.ID)).To(Equal(stations.SetupReady))
		Expect(sel.asked()).To(HaveLen(1))
	})

	It("waits for the Listener's selection under way before asking for its own", func() {
		owl, pump := picks("owl", 15), picks("pump", 15)
		sel.pick = func(req string) model.MediaFiles {
			if strings.Contains(req, "DJ Night Owl.") {
				return owl
			}
			return pump
		}
		sel.release = make(chan struct{})
		_, err := svc.Next(ctx, "night-owl", "", 3) // starts Night Owl's selection
		Expect(err).ToNot(HaveOccurred())
		Eventually(sel.asked).Should(HaveLen(1))

		dj := create()
		Consistently(sel.asked, 100*time.Millisecond).Should(HaveLen(1))
		close(sel.release)
		Expect(svc.AwaitSetup(ctx, dj.ID)).To(Equal(stations.SetupReady))
		Expect(sel.asked()).To(HaveLen(2))
	})

	It("stops when the DJ is deleted, forgetting its plan", func() {
		stations.SetSetupTimes(svc, 5*time.Second, 50*time.Millisecond, time.Hour)
		sel.failWith(errors.New("AI unavailable"))
		dj := create()
		Expect(svc.AwaitSetup(ctx, dj.ID)).To(Equal(stations.SetupSettingUp))

		Expect(svc.DeleteDJ(ctx, dj.ID)).To(Succeed())
		done := make(chan struct{})
		go func() {
			stations.WaitForSetups(svc)
			close(done)
		}()
		Eventually(done, time.Second).Should(BeClosed())
		Expect(stations.HasPlan(svc, "hal-id", dj.ID)).To(BeFalse())
	})

	It("has no setup step without an LLM", func() {
		noLLM := stations.New(ds, nil, nil)
		dj, err := noLLM.CreateDJ(ctx, stations.DJEdit{Name: "Pump", Description: "Pump the house music"})
		Expect(err).ToNot(HaveOccurred())
		Expect(noLLM.AwaitSetup(ctx, dj.ID)).To(Equal(stations.SetupNone))
	})

	It("has no setup for a built-in DJ", func() {
		_, err := svc.AwaitSetup(ctx, "night-owl")
		Expect(err).To(MatchError(stations.ErrBuiltInDJ))
	})
})
