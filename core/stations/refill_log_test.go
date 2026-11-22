package stations_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/navidrome/navidrome/core/stations"
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

var _ = Describe("Station refill logging", Ordered, func() {
	var ds model.DataStore
	var sel *fakeSelector
	var svc stations.Service
	var ctx context.Context
	var hook *test.Hook
	var seq int

	BeforeAll(func() {
		ds = persistence.New(db.Db())
		dave := model.User{ID: "dave-id", UserName: "dave", Name: "dave", NewPassword: "pw"}
		Expect(ds.User(context.Background()).Put(&dave)).To(Succeed())
		Expect(ds.User(context.Background()).SetUserLibraries(dave.ID, []int{1})).To(Succeed())
		dave.Libraries = model.Libraries{{ID: 1}}
		ctx = request.WithUser(context.Background(), dave)
	})

	BeforeEach(func() {
		for _, table := range []string{"station_track", "annotation", "media_file"} {
			_, err := db.Db().Exec("delete from " + table)
			Expect(err).ToNot(HaveOccurred())
		}
		sel = &fakeSelector{pick: func(string) model.MediaFiles { return nil }}
		svc = stations.New(ds, sel, nil)

		l, h := test.NewNullLogger()
		hook = h
		prevLogger := log.SetDefaultLogger(l)
		prevLevel := log.CurrentLevel()
		log.SetLevel(log.LevelInfo)
		DeferCleanup(func() {
			stations.WaitForSelection(svc)
			log.SetDefaultLogger(prevLogger)
			log.SetLevel(prevLevel)
		})
	})

	// track makes a Track with a recognisable artist and title.
	track := func(kind string) model.MediaFile {
		seq++
		return model.MediaFile{
			ID: fmt.Sprintf("log-%s-%04d", kind, seq), LibraryID: 1, Path: fmt.Sprintf("log/%s/%d.mp3", kind, seq),
			Title: fmt.Sprintf("%s song %d", kind, seq), Artist: fmt.Sprintf("%s band %d", kind, seq),
			AlbumArtist: "Various", Album: "Album", Tags: model.Tags{},
		}
	}

	// addLibrary puts n Tracks in the Library, for rule-based selection to choose from.
	addLibrary := func(n int) {
		for range n {
			mf := track("library")
			Expect(ds.MediaFile(ctx).Put(&mf)).To(Succeed())
		}
	}

	// planOf makes the fake LLM pick n Tracks kept out of the database, so only the plan serves them.
	planOf := func(n int) model.MediaFiles {
		var res model.MediaFiles
		for range n {
			res = append(res, track("planned"))
		}
		sel.pick = func(string) model.MediaFiles { return res }
		return res
	}

	// refills returns the info lines logged for Station refills since the last call.
	refills := func() []*logrus.Entry {
		var res []*logrus.Entry
		for _, e := range hook.AllEntries() {
			if e.Message == "Station refill" {
				res = append(res, e)
			}
		}
		hook.Reset()
		return res
	}

	described := func(source string, mf model.MediaFile) string {
		return fmt.Sprintf("%s: %s - %s", source, mf.Artist, mf.Title)
	}

	BeforeEach(func() {
		addLibrary(20)
	})

	It("logs a refill served from the LLM's plan alone", func() {
		planned := planOf(8)
		_, err := svc.Next(ctx, "night-owl", "rain", 3)
		Expect(err).ToNot(HaveOccurred())
		stations.WaitForSelection(svc)
		refills()

		_, err = svc.Next(ctx, "night-owl", "rain", 3)
		Expect(err).ToNot(HaveOccurred())

		lines := refills()
		Expect(lines).To(HaveLen(1))
		Expect(lines[0].Level).To(Equal(logrus.InfoLevel))
		Expect(lines[0].Data).To(HaveKeyWithValue("listener", "dave"))
		Expect(lines[0].Data).To(HaveKeyWithValue("dj", "night-owl"))
		Expect(lines[0].Data).To(HaveKeyWithValue("planned", 3))
		Expect(lines[0].Data).To(HaveKeyWithValue("byRule", 0))
		Expect(lines[0].Data).To(HaveKeyWithValue("tracks", fmt.Sprintf("[`%s`,`%s`,`%s`]",
			described("plan", planned[0]), described("plan", planned[1]), described("plan", planned[2]))))
	})

	It("logs a refill served by rule alone, as when a Station starts", func() {
		served, err := svc.Next(ctx, "sunny", "", 2)
		Expect(err).ToNot(HaveOccurred())

		lines := refills()
		Expect(lines).To(HaveLen(1))
		Expect(lines[0].Data).To(HaveKeyWithValue("dj", "sunny"))
		Expect(lines[0].Data).To(HaveKeyWithValue("planned", 0))
		Expect(lines[0].Data).To(HaveKeyWithValue("byRule", 2))
		Expect(lines[0].Data).To(HaveKeyWithValue("tracks", fmt.Sprintf("[`%s`,`%s`]",
			described("rule", served[0]), described("rule", served[1]))))
	})

	It("logs which Tracks of a mixed refill came from the plan and which by rule", func() {
		planned := planOf(2)
		_, err := svc.Next(ctx, "night-owl", "", 3)
		Expect(err).ToNot(HaveOccurred())
		stations.WaitForSelection(svc)
		refills()

		served, err := svc.Next(ctx, "night-owl", "", 4)
		Expect(err).ToNot(HaveOccurred())

		Expect(ids(served[:2])).To(Equal(ids(planned)))
		lines := refills()
		Expect(lines).To(HaveLen(1))
		Expect(lines[0].Data).To(HaveKeyWithValue("planned", 2))
		Expect(lines[0].Data).To(HaveKeyWithValue("byRule", 2))
		Expect(lines[0].Data).To(HaveKeyWithValue("tracks", fmt.Sprintf("[`%s`,`%s`,`%s`,`%s`]",
			described("plan", planned[0]), described("plan", planned[1]),
			described("rule", served[2]), described("rule", served[3]))))
	})

	It("warns, with the error, when a selection fails", func() {
		sel.err = errors.New("AI unavailable")
		_, err := svc.Next(ctx, "night-owl", "", 3)
		Expect(err).ToNot(HaveOccurred())
		stations.WaitForSelection(svc)

		var warnings []*logrus.Entry
		for _, e := range hook.AllEntries() {
			if e.Level == logrus.WarnLevel {
				warnings = append(warnings, e)
			}
		}
		Expect(warnings).To(HaveLen(1))
		Expect(warnings[0].Data).To(HaveKeyWithValue("dj", "night-owl"))
		Expect(warnings[0].Data).To(HaveKeyWithValue("error", "AI unavailable"))
	})
})
