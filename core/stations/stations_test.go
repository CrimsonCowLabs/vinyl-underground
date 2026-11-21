package stations_test

import (
	"context"
	"fmt"
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

var _ = Describe("Stations", Ordered, func() {
	var ds model.DataStore
	var svc stations.Service
	var alice, bob model.User
	var aliceCtx, bobCtx context.Context
	var seq int

	BeforeAll(func() {
		ds = persistence.New(db.Db())

		lib := model.Library{ID: 2, Name: "Other Library", Path: GinkgoT().TempDir()}
		Expect(ds.Library(context.Background()).Put(&lib)).To(Succeed())

		for _, u := range []*model.User{&alice, &bob} {
			name := map[*model.User]string{&alice: "alice", &bob: "bob"}[u]
			*u = model.User{ID: name + "-id", UserName: name, Name: name, NewPassword: "pw"}
			Expect(ds.User(context.Background()).Put(u)).To(Succeed())
			Expect(ds.User(context.Background()).SetUserLibraries(u.ID, []int{1})).To(Succeed())
			u.Libraries = model.Libraries{{ID: 1}}
		}
		aliceCtx = request.WithUser(context.Background(), alice)
		bobCtx = request.WithUser(context.Background(), bob)
	})

	BeforeEach(func() {
		for _, table := range []string{"station_track", "station_skip", "annotation", "media_file"} {
			_, err := db.Db().Exec("delete from " + table)
			Expect(err).ToNot(HaveOccurred())
		}
		svc = stations.New(ds, nil)
	})

	// addTracks puts n Tracks, each by its own artist, in the Library (library 1 unless lib is
	// given) and returns their IDs.
	addTracks := func(n int, lib ...int) []string {
		libID := 1
		if len(lib) > 0 {
			libID = lib[0]
		}
		var res []string
		for range n {
			seq++
			mf := model.MediaFile{
				ID: fmt.Sprintf("mf-%04d", seq), LibraryID: libID, Path: fmt.Sprintf("a/%d.mp3", seq),
				Title: fmt.Sprintf("Song %d", seq), Artist: fmt.Sprintf("Artist %d", seq),
				AlbumArtist: "Various", Album: "Album", Tags: model.Tags{},
			}
			Expect(ds.MediaFile(aliceCtx).Put(&mf)).To(Succeed())
			res = append(res, mf.ID)
		}
		return res
	}

	It("lists the built-in DJs", func() {
		djs, err := svc.DJs(aliceCtx)
		Expect(err).ToNot(HaveOccurred())
		Expect(djs).To(HaveLen(3))
		for _, dj := range djs {
			Expect(dj.BuiltIn).To(BeTrue())
		}
	})

	Describe("a Listener's own DJs", func() {
		const mellow = "Slow, soft songs for a rainy afternoon."

		BeforeEach(func() {
			_, err := db.Db().Exec("delete from dj")
			Expect(err).ToNot(HaveOccurred())
		})

		It("are listed after the built-in ones, for that Listener only", func() {
			mine, err := svc.CreateDJ(aliceCtx, "  Mr.   Mellow ", "  "+mellow+"\n")
			Expect(err).ToNot(HaveOccurred())
			Expect(mine.ID).ToNot(BeEmpty())
			Expect(mine.Name).To(Equal("Mr. Mellow"))
			Expect(mine.Description).To(Equal(mellow))
			Expect(mine.BuiltIn).To(BeFalse())

			djs, err := svc.DJs(aliceCtx)
			Expect(err).ToNot(HaveOccurred())
			Expect(djs).To(HaveLen(4))
			Expect(djs[3].ID).To(Equal(mine.ID))
			Expect(djs[3].Name).To(Equal("Mr. Mellow"))

			Expect(svc.DJs(bobCtx)).To(HaveLen(3))
			_, err = svc.DJ(bobCtx, mine.ID)
			Expect(err).To(MatchError(stations.ErrUnknownDJ))
		})

		It("are found by id, like the built-in ones", func() {
			mine, err := svc.CreateDJ(aliceCtx, "Mr. Mellow", mellow)
			Expect(err).ToNot(HaveOccurred())

			got, err := svc.DJ(aliceCtx, mine.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.Name).To(Equal("Mr. Mellow"))
			Expect(got.Description).To(Equal(mellow))

			owl, err := svc.DJ(aliceCtx, "night-owl")
			Expect(err).ToNot(HaveOccurred())
			Expect(owl.Name).To(Equal("Night Owl"))
			Expect(owl.BuiltIn).To(BeTrue())
		})

		It("need a name and a description of a sensible length", func() {
			_, err := svc.CreateDJ(aliceCtx, "   ", mellow)
			Expect(err).To(MatchError(stations.ErrInvalidDJName))
			_, err = svc.CreateDJ(aliceCtx, strings.Repeat("n", stations.MaxDJNameLength+1), mellow)
			Expect(err).To(MatchError(stations.ErrInvalidDJName))
			_, err = svc.CreateDJ(aliceCtx, "Mr. Mellow", " ")
			Expect(err).To(MatchError(stations.ErrInvalidDJDescription))
			_, err = svc.CreateDJ(aliceCtx, "Mr. Mellow", strings.Repeat("d", stations.MaxDJDescriptionLength+1))
			Expect(err).To(MatchError(stations.ErrInvalidDJDescription))
			Expect(svc.DJs(aliceCtx)).To(HaveLen(3))
		})

		It("can be edited and deleted by their Listener only", func() {
			mine, err := svc.CreateDJ(aliceCtx, "Mr. Mellow", mellow)
			Expect(err).ToNot(HaveOccurred())

			_, err = svc.UpdateDJ(bobCtx, mine.ID, "Hijacked", "Bob's now.")
			Expect(err).To(MatchError(stations.ErrUnknownDJ))
			Expect(svc.DeleteDJ(bobCtx, mine.ID)).To(MatchError(stations.ErrUnknownDJ))

			edited, err := svc.UpdateDJ(aliceCtx, mine.ID, "Mr. Loud", "Fast and loud.")
			Expect(err).ToNot(HaveOccurred())
			Expect(edited.ID).To(Equal(mine.ID))
			Expect(edited.Name).To(Equal("Mr. Loud"))
			got, err := svc.DJ(aliceCtx, mine.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.Description).To(Equal("Fast and loud."))

			_, err = svc.UpdateDJ(aliceCtx, mine.ID, "", "Fast and loud.")
			Expect(err).To(MatchError(stations.ErrInvalidDJName))

			Expect(svc.DeleteDJ(aliceCtx, mine.ID)).To(Succeed())
			_, err = svc.DJ(aliceCtx, mine.ID)
			Expect(err).To(MatchError(stations.ErrUnknownDJ))
			Expect(svc.DeleteDJ(aliceCtx, mine.ID)).To(MatchError(stations.ErrUnknownDJ))
		})

		It("leave the built-in DJs read-only", func() {
			_, err := svc.UpdateDJ(aliceCtx, "night-owl", "My Owl", mellow)
			Expect(err).To(MatchError(stations.ErrBuiltInDJ))
			Expect(svc.DeleteDJ(aliceCtx, "night-owl")).To(MatchError(stations.ErrBuiltInDJ))
			owl, err := svc.DJ(aliceCtx, "night-owl")
			Expect(err).ToNot(HaveOccurred())
			Expect(owl.Name).To(Equal("Night Owl"))
		})

		It("host Stations, with skips, for their Listener only", func() {
			tracks := addTracks(6)
			mine, err := svc.CreateDJ(aliceCtx, "Mr. Mellow", mellow)
			Expect(err).ToNot(HaveOccurred())

			served, err := svc.Next(aliceCtx, mine.ID, "", 3)
			Expect(err).ToNot(HaveOccurred())
			Expect(served).To(HaveLen(3))
			Expect(ids(served)).To(HaveEach(BeElementOf(tracks)))
			Expect(svc.Skip(aliceCtx, mine.ID, served[0].ID)).To(Succeed())

			_, err = svc.Next(bobCtx, mine.ID, "", 3)
			Expect(err).To(MatchError(stations.ErrUnknownDJ))
			Expect(svc.Skip(bobCtx, mine.ID, served[0].ID)).To(MatchError(stations.ErrUnknownDJ))
		})

		It("can be deleted without breaking play history", func() {
			tracks := addTracks(6)
			mine, err := svc.CreateDJ(aliceCtx, "Mr. Mellow", mellow)
			Expect(err).ToNot(HaveOccurred())
			served, err := svc.Next(aliceCtx, mine.ID, "", 3)
			Expect(err).ToNot(HaveOccurred())
			Expect(svc.Skip(aliceCtx, mine.ID, served[0].ID)).To(Succeed())
			Expect(ds.MediaFile(aliceCtx).IncPlayCount(served[1].ID, time.Now())).To(Succeed())

			Expect(svc.DeleteDJ(aliceCtx, mine.ID)).To(Succeed())

			mf, err := ds.MediaFile(aliceCtx).Get(served[1].ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(mf.PlayCount).To(Equal(int64(1)))
			// What the deleted DJ's Station served still counts against repeats on another one.
			next, err := svc.Next(aliceCtx, "sunny", "", 3)
			Expect(err).ToNot(HaveOccurred())
			Expect(ids(next)).ToNot(ContainElement(BeElementOf(ids(served))))
			Expect(ids(next)).To(HaveEach(BeElementOf(tracks)))
		})
	})

	It("serves the number of Tracks asked for from the Listener's Library", func() {
		mine := addTracks(8)
		addTracks(8, 2)

		tracks, err := svc.Next(aliceCtx, "night-owl", "", 5)

		Expect(err).ToNot(HaveOccurred())
		Expect(tracks).To(HaveLen(5))
		Expect(ids(tracks)).To(HaveEach(BeElementOf(mine)))
	})

	It("serves 3 Tracks when no count is given", func() {
		addTracks(8)
		tracks, err := svc.Next(aliceCtx, "sunny", "", 0)
		Expect(err).ToNot(HaveOccurred())
		Expect(tracks).To(HaveLen(3))
	})

	It("refuses an unknown DJ or a count over the limit", func() {
		addTracks(3)
		_, err := svc.Next(aliceCtx, "nobody", "", 3)
		Expect(err).To(MatchError(stations.ErrUnknownDJ))
		_, err = svc.Next(aliceCtx, "sunny", "", stations.MaxCount+1)
		Expect(err).To(MatchError(stations.ErrInvalidCount))
	})

	It("says when the Library has no Tracks to play", func() {
		_, err := svc.Next(aliceCtx, "sunny", "", 3)
		Expect(err).To(MatchError(stations.ErrNoTracks))
	})

	It("leaves out missing Tracks", func() {
		present := addTracks(2)
		gone := addTracks(2)
		for _, id := range gone {
			_, err := db.Db().Exec("update media_file set missing = true where id = ?", id)
			Expect(err).ToNot(HaveOccurred())
		}
		tracks, err := svc.Next(aliceCtx, "sunny", "", 2)
		Expect(err).ToNot(HaveOccurred())
		Expect(ids(tracks)).To(ConsistOf(present))
	})

	It("keeps refilling without repeating a Track", func() {
		all := addTracks(12)
		var served []string
		for range 4 {
			tracks, err := svc.Next(aliceCtx, "crate-digger", "", 3)
			Expect(err).ToNot(HaveOccurred())
			served = append(served, ids(tracks)...)
		}
		Expect(served).To(ConsistOf(all))
	})

	It("leaves out Tracks the Listener played in the last 48h", func() {
		all := addTracks(6)
		Expect(ds.MediaFile(aliceCtx).IncPlayCount(all[0], time.Now().Add(-time.Hour))).To(Succeed())
		Expect(ds.MediaFile(aliceCtx).IncPlayCount(all[1], time.Now().Add(-50*time.Hour))).To(Succeed())

		tracks, err := svc.Next(aliceCtx, "sunny", "", 5)

		Expect(err).ToNot(HaveOccurred())
		Expect(ids(tracks)).To(ConsistOf(all[1:]))
	})

	It("gives each Listener their own Station", func() {
		addTracks(20)
		aliceTracks, err := svc.Next(aliceCtx, "sunny", "", 10)
		Expect(err).ToNot(HaveOccurred())

		// Had Bob shared Alice's Station, he would get exactly the 10 Tracks she wasn't served.
		bobTracks, err := svc.Next(bobCtx, "sunny", "", 10)
		Expect(err).ToNot(HaveOccurred())
		Expect(ids(bobTracks)).To(ContainElement(BeElementOf(ids(aliceTracks))))
	})

	Describe("skips", func() {
		// firstPicks counts how often each Track is the first one a Station serves over many
		// fresh starts. Forgetting what the Station served stands in for 48h passing.
		firstPicks := func(ctx context.Context, djID string) map[string]int {
			res := map[string]int{}
			for range 150 {
				_, err := db.Db().Exec("delete from station_track")
				Expect(err).ToNot(HaveOccurred())
				tracks, err := svc.Next(ctx, djID, "", 1)
				Expect(err).ToNot(HaveOccurred())
				res[tracks[0].ID]++
			}
			return res
		}

		// skipHard serves both Tracks on Alice's Sunny Station and has her skip the first one
		// again and again, so it plays at its least: a sixth of the first picks against half.
		skipHard := func(tracks []string) {
			_, err := svc.Next(aliceCtx, "sunny", "", 2)
			Expect(err).ToNot(HaveOccurred())
			for range 10 {
				Expect(svc.Skip(aliceCtx, "sunny", tracks[0])).To(Succeed())
			}
		}

		It("make that Station less likely to play the Track", func() {
			tracks := addTracks(2)
			skipHard(tracks)
			n := firstPicks(aliceCtx, "sunny")
			Expect(n[tracks[0]]).To(BeNumerically("<", 50))
		})

		It("leave the Listener's other Stations and the other Listener alone", func() {
			tracks := addTracks(2)
			skipHard(tracks)
			Expect(firstPicks(aliceCtx, "night-owl")[tracks[0]]).To(BeNumerically(">", 50))
			Expect(firstPicks(bobCtx, "sunny")[tracks[0]]).To(BeNumerically(">", 50))
		})

		It("leave ratings, favorites and play counts alone", func() {
			tracks := addTracks(2)
			repo := ds.MediaFile(aliceCtx)
			Expect(repo.SetStar(true, tracks[0])).To(Succeed())
			Expect(repo.SetRating(5, tracks[0])).To(Succeed())
			Expect(repo.IncPlayCount(tracks[0], time.Now().Add(-72*time.Hour))).To(Succeed())

			skipHard(tracks)

			mf, err := repo.Get(tracks[0])
			Expect(err).ToNot(HaveOccurred())
			Expect(mf.Starred).To(BeTrue())
			Expect(mf.Rating).To(Equal(5))
			Expect(mf.PlayCount).To(Equal(int64(1)))
		})

		It("only count for Tracks the Station served, on a known DJ", func() {
			tracks := addTracks(2)
			Expect(svc.Skip(aliceCtx, "sunny", tracks[0])).To(MatchError(stations.ErrNotServed))
			_, err := svc.Next(aliceCtx, "sunny", "", 2)
			Expect(err).ToNot(HaveOccurred())
			Expect(svc.Skip(bobCtx, "sunny", tracks[0])).To(MatchError(stations.ErrNotServed))
			Expect(svc.Skip(aliceCtx, "nobody", tracks[0])).To(MatchError(stations.ErrUnknownDJ))
		})
	})
})
