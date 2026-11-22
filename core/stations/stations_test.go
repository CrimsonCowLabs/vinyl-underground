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
		svc = stations.New(ds, nil, nil)
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
			mine, err := svc.CreateDJ(aliceCtx, stations.DJEdit{Name: "  Mr.   Mellow ", Description: "  "+mellow+"\n"})
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
			mine, err := svc.CreateDJ(aliceCtx, stations.DJEdit{Name: "Mr. Mellow", Description: mellow})
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
			_, err := svc.CreateDJ(aliceCtx, stations.DJEdit{Name: "   ", Description: mellow})
			Expect(err).To(MatchError(stations.ErrInvalidDJName))
			_, err = svc.CreateDJ(aliceCtx, stations.DJEdit{Name: strings.Repeat("n", stations.MaxDJNameLength+1), Description: mellow})
			Expect(err).To(MatchError(stations.ErrInvalidDJName))
			_, err = svc.CreateDJ(aliceCtx, stations.DJEdit{Name: "Mr. Mellow", Description: " "})
			Expect(err).To(MatchError(stations.ErrInvalidDJDescription))
			_, err = svc.CreateDJ(aliceCtx, stations.DJEdit{Name: "Mr. Mellow", Description: strings.Repeat("d", stations.MaxDJDescriptionLength+1)})
			Expect(err).To(MatchError(stations.ErrInvalidDJDescription))
			Expect(svc.DJs(aliceCtx)).To(HaveLen(3))
		})

		It("can be edited and deleted by their Listener only", func() {
			mine, err := svc.CreateDJ(aliceCtx, stations.DJEdit{Name: "Mr. Mellow", Description: mellow})
			Expect(err).ToNot(HaveOccurred())

			_, err = svc.UpdateDJ(bobCtx, mine.ID, stations.DJEdit{Name: "Hijacked", Description: "Bob's now."})
			Expect(err).To(MatchError(stations.ErrUnknownDJ))
			Expect(svc.DeleteDJ(bobCtx, mine.ID)).To(MatchError(stations.ErrUnknownDJ))

			edited, err := svc.UpdateDJ(aliceCtx, mine.ID, stations.DJEdit{Name: "Mr. Loud", Description: "Fast and loud."})
			Expect(err).ToNot(HaveOccurred())
			Expect(edited.ID).To(Equal(mine.ID))
			Expect(edited.Name).To(Equal("Mr. Loud"))
			got, err := svc.DJ(aliceCtx, mine.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.Description).To(Equal("Fast and loud."))

			_, err = svc.UpdateDJ(aliceCtx, mine.ID, stations.DJEdit{Name: "", Description: "Fast and loud."})
			Expect(err).To(MatchError(stations.ErrInvalidDJName))

			Expect(svc.DeleteDJ(aliceCtx, mine.ID)).To(Succeed())
			_, err = svc.DJ(aliceCtx, mine.ID)
			Expect(err).To(MatchError(stations.ErrUnknownDJ))
			Expect(svc.DeleteDJ(aliceCtx, mine.ID)).To(MatchError(stations.ErrUnknownDJ))
		})

		It("leave the built-in DJs read-only", func() {
			_, err := svc.UpdateDJ(aliceCtx, "night-owl", stations.DJEdit{Name: "My Owl", Description: mellow})
			Expect(err).To(MatchError(stations.ErrBuiltInDJ))
			Expect(svc.DeleteDJ(aliceCtx, "night-owl")).To(MatchError(stations.ErrBuiltInDJ))
			owl, err := svc.DJ(aliceCtx, "night-owl")
			Expect(err).ToNot(HaveOccurred())
			Expect(owl.Name).To(Equal("Night Owl"))
		})

		It("shape Stations, with skips, for their Listener only", func() {
			tracks := addTracks(6)
			mine, err := svc.CreateDJ(aliceCtx, stations.DJEdit{Name: "Mr. Mellow", Description: mellow})
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
			mine, err := svc.CreateDJ(aliceCtx, stations.DJEdit{Name: "Mr. Mellow", Description: mellow})
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

	Describe("a DJ's genres", func() {
		// addInGenre puts n Tracks tagged with the genre in the Library, as a scan does: the
		// genre lives in the Track's tags only, the plain genre column stays empty.
		addInGenre := func(n int, genre string) []string {
			tag := model.NewTag(model.TagGenre, genre)
			Expect(ds.Tag(aliceCtx).Add(1, tag)).To(Succeed())
			ids := addTracks(n)
			for _, id := range ids {
				mf, err := ds.MediaFile(aliceCtx).Get(id)
				Expect(err).ToNot(HaveOccurred())
				mf.Tags = model.Tags{model.TagGenre: {genre}}
				Expect(ds.MediaFile(aliceCtx).Put(mf)).To(Succeed())
			}
			return ids
		}

		// serve refills the Station with the DJ until it has served n Tracks.
		serve := func(djID string, n int) []string {
			var res []string
			for len(res) < n {
				tracks, err := svc.Next(aliceCtx, djID, "", stations.MaxCount)
				Expect(err).ToNot(HaveOccurred())
				res = append(res, ids(tracks)...)
			}
			return res
		}

		BeforeEach(func() {
			_, err := db.Db().Exec("delete from library_tag")
			Expect(err).ToNot(HaveOccurred())
			_, err = db.Db().Exec("delete from tag")
			Expect(err).ToNot(HaveOccurred())
		})

		It("only play Tracks in them when 50 or more Library Tracks are", func() {
			jazz := addInGenre(30, "Cool Jazz")
			jazz = append(jazz, addInGenre(20, "Acid Jazz")...)
			addInGenre(30, "Thrash Metal")
			addTracks(40)

			Expect(serve("night-owl", 50)).To(HaveEach(BeElementOf(jazz)))
		})

		It("are matched within a Track's genre tags", func() {
			dance := addInGenre(50, "Electronica & Dance")
			addTracks(40)

			Expect(serve("sunny", 50)).To(HaveEach(BeElementOf(dance)))
		})

		It("keep the Station playing them once they have all been served", func() {
			jazz := addInGenre(50, "Cool Jazz")
			addTracks(40)

			Expect(serve("night-owl", 70)).To(HaveEach(BeElementOf(jazz)))
		})

		It("are only a boost when fewer than 50 Library Tracks are in them", func() {
			jazz := addInGenre(49, "Cool Jazz")
			untagged := addTracks(40)

			served := serve("night-owl", 60)
			Expect(served).To(ContainElement(BeElementOf(untagged)))
			Expect(served).To(ContainElement(BeElementOf(jazz)))
		})

		It("leave a DJ without genres playing the whole Library", func() {
			addInGenre(60, "Cool Jazz")
			untagged := addTracks(40)

			Expect(serve("crate-digger", 60)).To(ContainElement(BeElementOf(untagged)))
		})

		Describe("of a Listener's own DJ", func() {
			// library puts one Track in each genre, so the Library has those genres.
			library := func(genres ...string) {
				for _, g := range genres {
					addInGenre(1, g)
				}
			}

			BeforeEach(func() {
				_, err := db.Db().Exec("delete from dj")
				Expect(err).ToNot(HaveOccurred())
			})

			It("are matched from the words of its name and description", func() {
				library("Techno", "Hardcore Techno", "Euro-Techno", "House", "Club-House", "Deep House",
					"Electronica & Dance", "Rock", "World Music")

				tina, err := svc.CreateDJ(aliceCtx, stations.DJEdit{Name: "Techno Tina", Description: "Pounding beats all night."})
				Expect(err).ToNot(HaveOccurred())
				Expect(tina.Genres).To(ConsistOf("Techno", "Hardcore Techno", "Euro-Techno"))
				Expect(tina.GenresEdited).To(BeFalse())

				pump, err := svc.CreateDJ(aliceCtx, stations.DJEdit{Name: "Pump", Description: "Pump the house music, and some DEEP HOUSE."})
				Expect(err).ToNot(HaveOccurred())
				Expect(pump.Genres).To(ConsistOf("House", "Club-House", "Deep House"))

				got, err := svc.DJ(aliceCtx, pump.ID)
				Expect(err).ToNot(HaveOccurred())
				Expect(got.Genres).To(ConsistOf("House", "Club-House", "Deep House"))
			})

			It("are matched by a word that says something, or by their whole name", func() {
				library("New Wave", "New Age", "World Music", "R&B", "Rock")
				Expect(svc.MatchGenres(aliceCtx, "Wendy", "New wave, and new songs from the world.")).To(ConsistOf("New Wave", "World Music"))
				Expect(svc.MatchGenres(aliceCtx, "Rita", "Some R&B, nothing that rocks.")).To(ConsistOf("R&B"))
			})

			It("are matched again when the name or description changes", func() {
				library("Techno", "House", "Rock")
				dj, err := svc.CreateDJ(aliceCtx, stations.DJEdit{Name: "Tina", Description: "Techno all night."})
				Expect(err).ToNot(HaveOccurred())

				dj, err = svc.UpdateDJ(aliceCtx, dj.ID, stations.DJEdit{Name: "Tina", Description: "House and rock now."})
				Expect(err).ToNot(HaveOccurred())
				Expect(dj.Genres).To(ConsistOf("House", "Rock"))
			})

			It("can be picked by hand from the Library's genres, and then survive description edits", func() {
				library("Techno", "Hardcore Techno", "House", "Rock")
				dj, err := svc.CreateDJ(aliceCtx, stations.DJEdit{Name: "Tina", Description: "Techno all night."})
				Expect(err).ToNot(HaveOccurred())

				dj, err = svc.UpdateDJ(aliceCtx, dj.ID, stations.DJEdit{Name: "Tina", Description: "Techno all night.",
					Genres: []string{"hardcore techno", "Polka", "Rock", "rock"}})
				Expect(err).ToNot(HaveOccurred())
				Expect(dj.Genres).To(Equal([]string{"Hardcore Techno", "Rock"}))
				Expect(dj.GenresEdited).To(BeTrue())

				dj, err = svc.UpdateDJ(aliceCtx, dj.ID, stations.DJEdit{Name: "House Tina", Description: "Only house now."})
				Expect(err).ToNot(HaveOccurred())
				Expect(dj.Description).To(Equal("Only house now."))
				Expect(dj.Genres).To(Equal([]string{"Hardcore Techno", "Rock"}))
				Expect(dj.GenresEdited).To(BeTrue())
				got, err := svc.DJ(aliceCtx, dj.ID)
				Expect(err).ToNot(HaveOccurred())
				Expect(got.Genres).To(Equal([]string{"Hardcore Techno", "Rock"}))
			})

			It("can be picked by hand when creating the DJ, even none", func() {
				library("Techno", "House")
				dj, err := svc.CreateDJ(aliceCtx, stations.DJEdit{Name: "Tina", Description: "Techno all night.", Genres: []string{}})
				Expect(err).ToNot(HaveOccurred())
				Expect(dj.Genres).To(BeEmpty())
				Expect(dj.GenresEdited).To(BeTrue())

				dj, err = svc.UpdateDJ(aliceCtx, dj.ID, stations.DJEdit{Name: "Tina", Description: "House, techno."})
				Expect(err).ToNot(HaveOccurred())
				Expect(dj.Genres).To(BeEmpty())
			})

			It("only play Tracks in them when 50 or more Library Tracks are", func() {
				techno := addInGenre(30, "Techno")
				techno = append(techno, addInGenre(20, "Hardcore Techno")...)
				addInGenre(30, "Rock")
				addTracks(40)
				dj, err := svc.CreateDJ(aliceCtx, stations.DJEdit{Name: "Tina", Description: "Techno all night."})
				Expect(err).ToNot(HaveOccurred())

				Expect(serve(dj.ID, 50)).To(HaveEach(BeElementOf(techno)))
			})

			It("play the genres picked, not every genre with the same word", func() {
				techno := addInGenre(60, "Techno")
				addInGenre(30, "Hardcore Techno")
				dj, err := svc.CreateDJ(aliceCtx, stations.DJEdit{Name: "Tina", Description: "Techno all night.",
					Genres: []string{"Techno"}})
				Expect(err).ToNot(HaveOccurred())

				Expect(serve(dj.ID, 60)).To(HaveEach(BeElementOf(techno)))
			})

			It("are only a boost when fewer than 50 Library Tracks are in them", func() {
				techno := addInGenre(49, "Techno")
				untagged := addTracks(40)
				dj, err := svc.CreateDJ(aliceCtx, stations.DJEdit{Name: "Tina", Description: "Techno all night."})
				Expect(err).ToNot(HaveOccurred())

				served := serve(dj.ID, 60)
				Expect(served).To(ContainElement(BeElementOf(untagged)))
				Expect(served).To(ContainElement(BeElementOf(techno)))
			})

			It("leave a DJ without them playing the whole Library", func() {
				addInGenre(60, "Techno")
				untagged := addTracks(40)
				dj, err := svc.CreateDJ(aliceCtx, stations.DJEdit{Name: "Mr. Mellow", Description: "Slow, soft songs."})
				Expect(err).ToNot(HaveOccurred())
				Expect(dj.Genres).To(BeEmpty())

				Expect(serve(dj.ID, 60)).To(ContainElement(BeElementOf(untagged)))
			})

			It("are picked from the Library's genres, which the form lists by name", func() {
				library("techno", "Rock", "House")
				Expect(svc.Genres(aliceCtx)).To(Equal([]string{"House", "Rock", "techno"}))
			})

			It("are matched for the form before the DJ is saved", func() {
				library("Techno", "House", "Rock")
				Expect(svc.MatchGenres(aliceCtx, "Tina", "Techno and a bit of house.")).To(ConsistOf("Techno", "House"))
				Expect(svc.MatchGenres(aliceCtx, "Mr. Mellow", "Slow, soft songs.")).To(BeEmpty())
				Expect(svc.DJs(aliceCtx)).To(HaveLen(3))
			})

			It("can't be changed on another Listener's DJ", func() {
				library("Techno")
				dj, err := svc.CreateDJ(aliceCtx, stations.DJEdit{Name: "Tina", Description: "Techno all night."})
				Expect(err).ToNot(HaveOccurred())
				_, err = svc.UpdateDJ(bobCtx, dj.ID, stations.DJEdit{Name: "Bob", Description: "Mine.", Genres: []string{}})
				Expect(err).To(MatchError(stations.ErrUnknownDJ))
			})
		})
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
