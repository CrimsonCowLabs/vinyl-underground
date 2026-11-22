package stations_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/navidrome/navidrome/core/llm"
	"github.com/navidrome/navidrome/core/stations"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/persistence"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeGenreLLM stands in for the LLM mapping a DJ to genres: it answers genres, or fails with
// err. Until release is closed (when set), it hangs like a slow LLM.
type fakeGenreLLM struct {
	mu      sync.Mutex
	genres  []string
	err     error
	release chan struct{}
	asked   []llm.Request
}

func (f *fakeGenreLLM) Complete(ctx context.Context, req llm.Request, out any) error {
	f.mu.Lock()
	f.asked = append(f.asked, req)
	genres, err, release := f.genres, f.err, f.release
	f.mu.Unlock()
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err != nil {
		return err
	}
	b, _ := json.Marshal(map[string]any{"genres": genres})
	return json.Unmarshal(b, out)
}

func (f *fakeGenreLLM) answer(genres ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.genres, f.err = genres, nil
}

func (f *fakeGenreLLM) fail() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = fmt.Errorf("%w: the host is asleep", llm.ErrUnavailable)
}

func (f *fakeGenreLLM) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.asked)
}

func (f *fakeGenreLLM) lastAsked() llm.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.asked[len(f.asked)-1]
}

var _ = Describe("A Listener's DJ's genres, mapped by the LLM", Ordered, func() {
	const pumpDescription = "Pump the house music"
	var ds model.DataStore
	var fake *fakeGenreLLM
	var svc stations.Service
	var ctx context.Context
	var seq int

	BeforeAll(func() {
		ds = persistence.New(db.Db())
		gina := model.User{ID: "gina-id", UserName: "gina", Name: "gina", NewPassword: "pw"}
		Expect(ds.User(context.Background()).Put(&gina)).To(Succeed())
		Expect(ds.User(context.Background()).SetUserLibraries(gina.ID, []int{1})).To(Succeed())
		gina.Libraries = model.Libraries{{ID: 1}}
		ctx = request.WithUser(context.Background(), gina)
	})

	// library puts a Track in each genre, so the Library has those genres.
	library := func(genres ...string) {
		for _, g := range genres {
			Expect(ds.Tag(ctx).Add(1, model.NewTag(model.TagGenre, g))).To(Succeed())
			seq++
			mf := model.MediaFile{
				ID: fmt.Sprintf("genre-llm-%04d", seq), LibraryID: 1, Path: fmt.Sprintf("g/%d.mp3", seq),
				Title: fmt.Sprintf("Song %d", seq), Artist: fmt.Sprintf("Artist %d", seq),
				AlbumArtist: "Various", Album: "Album", Tags: model.Tags{model.TagGenre: {g}},
			}
			Expect(ds.MediaFile(ctx).Put(&mf)).To(Succeed())
		}
	}

	BeforeEach(func() {
		for _, table := range []string{"station_track", "station_skip", "annotation", "media_file", "library_tag", "tag", "dj"} {
			_, err := db.Db().Exec("delete from " + table)
			Expect(err).ToNot(HaveOccurred())
		}
		library("Electronica & Dance", "Dance", "Electronica", "House", "Club-House", "Deep House", "Disco", "Rock")
		fake = &fakeGenreLLM{}
		svc = stations.New(ds, nil, nil, stations.WithGenreLLM(fake))
	})

	It("are the Library genres the LLM finds in the DJ's name and description when it is saved", func() {
		fake.answer("electronica & dance", "Dance", "Electronica", "House", "Club-House", "Big Beat", "house")

		pump, err := svc.CreateDJ(ctx, stations.DJEdit{Name: "Pump", Description: pumpDescription})
		Expect(err).ToNot(HaveOccurred())
		Expect(pump.Genres).To(Equal([]string{"Electronica & Dance", "Dance", "Electronica", "House", "Club-House"}))
		Expect(pump.GenresEdited).To(BeFalse())

		asked := fake.lastAsked()
		Expect(asked.User).To(ContainSubstring("Pump"))
		Expect(asked.User).To(ContainSubstring(pumpDescription))
		Expect(asked.User).To(ContainSubstring("Electronica & Dance"))
		Expect(asked.Schema).ToNot(BeEmpty())

		got, err := svc.DJ(ctx, pump.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Genres).To(Equal(pump.Genres))
	})

	It("fall back to word matching when the LLM names none of the Library's genres", func() {
		fake.answer("Big Beat", "Jungle")
		pump, err := svc.CreateDJ(ctx, stations.DJEdit{Name: "Pump", Description: pumpDescription})
		Expect(err).ToNot(HaveOccurred())
		Expect(pump.Genres).To(ConsistOf("House", "Club-House", "Deep House"))

		fake.answer()
		disco, err := svc.CreateDJ(ctx, stations.DJEdit{Name: "Dee", Description: "Disco all night"})
		Expect(err).ToNot(HaveOccurred())
		Expect(disco.Genres).To(ConsistOf("Disco"))
	})

	It("are word-matched when the LLM is down, and mapped by it the next time the DJ plays", func() {
		fake.fail()
		pump, err := svc.CreateDJ(ctx, stations.DJEdit{Name: "Pump", Description: pumpDescription})
		Expect(err).ToNot(HaveOccurred())
		Expect(pump.Genres).To(ConsistOf("House", "Club-House", "Deep House"))

		fake.answer("Dance", "House")
		stations.ForgetGenreFailures(svc)
		_, err = svc.Next(ctx, pump.ID, "", 3)
		Expect(err).ToNot(HaveOccurred())
		stations.WaitForGenres(svc)

		got, err := svc.DJ(ctx, pump.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Genres).To(Equal([]string{"Dance", "House"}))
	})

	It("are mapped for a DJ saved before DJs had genres, the first time it plays, without waiting for the LLM", func() {
		old := model.DJ{Name: "Pump", Description: pumpDescription}
		Expect(ds.DJ(ctx).Put(&old)).To(Succeed())
		fake.answer("Dance", "Electronica")
		fake.release = make(chan struct{})

		tracks, err := svc.Start(ctx, old.ID, "", 3)
		Expect(err).ToNot(HaveOccurred())
		Expect(tracks).ToNot(BeEmpty())
		// Until the LLM answers, the DJ plays its word-matched genres.
		got, err := svc.DJ(ctx, old.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Genres).To(ConsistOf("House", "Club-House", "Deep House"))

		close(fake.release)
		stations.WaitForGenres(svc)
		got, err = svc.DJ(ctx, old.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Genres).To(Equal([]string{"Dance", "Electronica"}))
	})

	It("are mapped for a DJ saved before DJs had genres when it is opened in the form", func() {
		old := model.DJ{Name: "Pump", Description: pumpDescription}
		Expect(ds.DJ(ctx).Put(&old)).To(Succeed())
		fake.answer("Dance", "Electronica")

		opened, err := svc.OpenDJ(ctx, old.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(opened.Genres).To(Equal([]string{"Dance", "Electronica"}))
		Expect(opened.GenresEdited).To(BeFalse())

		got, err := svc.DJ(ctx, old.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Genres).To(Equal([]string{"Dance", "Electronica"}))

		_, err = svc.OpenDJ(ctx, "night-owl")
		Expect(err).To(MatchError(stations.ErrBuiltInDJ))
	})

	It("aren't asked of the LLM again once it mapped them", func() {
		fake.answer("Dance")
		pump, err := svc.CreateDJ(ctx, stations.DJEdit{Name: "Pump", Description: pumpDescription})
		Expect(err).ToNot(HaveOccurred())
		calls := fake.calls()

		_, err = svc.Next(ctx, pump.ID, "", 3)
		Expect(err).ToNot(HaveOccurred())
		_, err = svc.OpenDJ(ctx, pump.ID)
		Expect(err).ToNot(HaveOccurred())
		_, err = svc.UpdateDJ(ctx, pump.ID, stations.DJEdit{Name: "Pump", Description: pumpDescription})
		Expect(err).ToNot(HaveOccurred())
		stations.WaitForGenres(svc)
		Expect(fake.calls()).To(Equal(calls))
	})

	It("are mapped again when the description changes, unless picked by hand", func() {
		fake.answer("Dance")
		dj, err := svc.CreateDJ(ctx, stations.DJEdit{Name: "Pump", Description: pumpDescription})
		Expect(err).ToNot(HaveOccurred())

		fake.answer("Rock", "Disco")
		dj, err = svc.UpdateDJ(ctx, dj.ID, stations.DJEdit{Name: "Pump", Description: "Rock and disco now"})
		Expect(err).ToNot(HaveOccurred())
		Expect(dj.Genres).To(Equal([]string{"Rock", "Disco"}))

		dj, err = svc.UpdateDJ(ctx, dj.ID, stations.DJEdit{Name: "Pump", Description: "Rock and disco now", Genres: []string{"Deep House"}})
		Expect(err).ToNot(HaveOccurred())
		calls := fake.calls()

		fake.answer("Dance")
		dj, err = svc.UpdateDJ(ctx, dj.ID, stations.DJEdit{Name: "Pump", Description: "Only house"})
		Expect(err).ToNot(HaveOccurred())
		Expect(dj.Genres).To(Equal([]string{"Deep House"}))
		Expect(dj.GenresEdited).To(BeTrue())
		_, err = svc.Next(ctx, dj.ID, "", 3)
		Expect(err).ToNot(HaveOccurred())
		_, err = svc.OpenDJ(ctx, dj.ID)
		Expect(err).ToNot(HaveOccurred())
		stations.WaitForGenres(svc)
		Expect(fake.calls()).To(Equal(calls))
		got, err := svc.DJ(ctx, dj.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Genres).To(Equal([]string{"Deep House"}))
	})

	It("are suggested again on request, replacing the ones picked by hand", func() {
		dj, err := svc.CreateDJ(ctx, stations.DJEdit{Name: "Pump", Description: pumpDescription, Genres: []string{"Rock"}})
		Expect(err).ToNot(HaveOccurred())

		fake.answer("Dance", "House")
		Expect(svc.MatchGenres(ctx, "Pump", pumpDescription)).To(Equal([]string{"Dance", "House"}))
		dj, err = svc.UpdateDJ(ctx, dj.ID, stations.DJEdit{Name: "Pump", Description: pumpDescription, Resuggest: true})
		Expect(err).ToNot(HaveOccurred())
		Expect(dj.Genres).To(Equal([]string{"Dance", "House"}))
		Expect(dj.GenresEdited).To(BeFalse())
		// Saving what the form just showed doesn't ask the LLM again.
		Expect(fake.calls()).To(Equal(1))
	})

	It("are suggested again on request without an LLM, from the words", func() {
		noLLM := stations.New(ds, nil, nil)
		dj, err := noLLM.CreateDJ(ctx, stations.DJEdit{Name: "Pump", Description: pumpDescription, Genres: []string{"Rock"}})
		Expect(err).ToNot(HaveOccurred())
		dj, err = noLLM.UpdateDJ(ctx, dj.ID, stations.DJEdit{Name: "Pump", Description: pumpDescription, Resuggest: true})
		Expect(err).ToNot(HaveOccurred())
		Expect(dj.Genres).To(ConsistOf("House", "Club-House", "Deep House"))
		Expect(dj.GenresEdited).To(BeFalse())
	})

	It("are word-matched for a DJ saved before DJs had genres the first time it plays, without an LLM", func() {
		old := model.DJ{Name: "Pump", Description: pumpDescription}
		Expect(ds.DJ(ctx).Put(&old)).To(Succeed())
		noLLM := stations.New(ds, nil, nil)
		_, err := noLLM.Next(ctx, old.ID, "", 3)
		Expect(err).ToNot(HaveOccurred())
		got, err := noLLM.DJ(ctx, old.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Genres).To(ConsistOf("House", "Club-House", "Deep House"))
	})
})
