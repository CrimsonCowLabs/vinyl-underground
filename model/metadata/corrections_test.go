package metadata_test

import (
	"os"

	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/metadata"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Corrections", func() {
	var props metadata.Info
	rename := func(id, from, to string) model.Correction {
		return model.Correction{ID: id, Kind: model.CorrectionArtistRename, MatchKey: model.ArtistNameKey(from), From: from, To: to}
	}
	read := func(tags model.RawTags, cs ...model.Correction) model.MediaFile {
		props.Tags = tags
		return metadata.New("Greatful Dead/Live/01.mp3", props).ToCorrectedMediaFile(1, "folderID", metadata.NewCorrections(cs))
	}
	names := func(mf model.MediaFile, role model.Role) []string {
		var res []string
		for _, p := range mf.Participants[role] {
			res = append(res, p.Name)
		}
		return res
	}

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		_, filePath, _ := tests.TempFile(GinkgoT(), "test", ".mp3")
		fileInfo, _ := os.Stat(filePath)
		props = metadata.Info{FileInfo: testFileInfo{fileInfo}}
	})

	Describe("artist rename", func() {
		fix := rename("c1", "Greatful Dead", "Grateful Dead")

		It("renames the Track artist and album artist, with the IDs of the new name", func() {
			mf := read(model.RawTags{"ARTIST": {"Greatful Dead"}, "ALBUMARTIST": {"Greatful Dead"}, "ALBUM": {"Live"}}, fix)
			want := read(model.RawTags{"ARTIST": {"Grateful Dead"}, "ALBUMARTIST": {"Grateful Dead"}, "ALBUM": {"Live"}})

			Expect(mf.Artist).To(Equal("Grateful Dead"))
			Expect(mf.AlbumArtist).To(Equal("Grateful Dead"))
			Expect(mf.ArtistID).To(Equal(want.ArtistID))
			Expect(mf.AlbumArtistID).To(Equal(want.AlbumArtistID))
			Expect(mf.Participants.First(model.RoleArtist)).To(Equal(want.Participants.First(model.RoleArtist)))
			Expect(mf.OrderAlbumArtistName).To(Equal("grateful dead"))
			Expect(mf.AlbumID).To(Equal(want.AlbumID))
			Expect(mf.CorrectionIDs).To(Equal([]string{"c1"}))
		})

		It("keeps the persistent ID of the uncorrected tags, so the Track is still the same Track", func() {
			tags := model.RawTags{"ARTIST": {"Greatful Dead"}, "ALBUMARTIST": {"Greatful Dead"}, "ALBUM": {"Live"}, "TITLE": {"Ripple"}}
			Expect(read(tags, fix).PID).To(Equal(read(tags).PID))
		})

		It("matches regardless of capitals and extra spaces", func() {
			mf := read(model.RawTags{"ARTIST": {"greatful   DEAD "}}, fix)
			Expect(mf.Artist).To(Equal("Grateful Dead"))
		})

		It("renames one Artist of a multi-artist tag after splitting, keeping the display joiners", func() {
			mf := read(model.RawTags{"ARTIST": {"Greatful Dead feat. Bob Dylan"}, "ALBUMARTISTS": {"Bob Dylan", "Greatful Dead"}}, fix)
			Expect(names(mf, model.RoleArtist)).To(Equal([]string{"Grateful Dead", "Bob Dylan"}))
			Expect(mf.Artist).To(Equal("Grateful Dead feat. Bob Dylan"))
			Expect(names(mf, model.RoleAlbumArtist)).To(Equal([]string{"Bob Dylan", "Grateful Dead"}))
		})

		It("replaces the name only where it stands whole in the display name", func() {
			mf := read(model.RawTags{"ARTIST": {"Joanne & Ann"}, "ARTISTS": {"Joanne", "Ann"}}, rename("c1", "Ann", "Anne"))
			Expect(mf.Artist).To(Equal("Joanne & Anne"))
		})

		It("renames the album artist when it falls back to the Track artist", func() {
			mf := read(model.RawTags{"ARTIST": {"Greatful Dead"}}, fix)
			Expect(names(mf, model.RoleAlbumArtist)).To(Equal([]string{"Grateful Dead"}))
			Expect(mf.AlbumArtist).To(Equal("Grateful Dead"))
		})

		It("does not rename composers or other credits", func() {
			mf := read(model.RawTags{"ARTIST": {"Someone"}, "COMPOSER": {"Greatful Dead"}}, fix)
			Expect(names(mf, model.RoleComposer)).To(Equal([]string{"Greatful Dead"}))
			Expect(mf.CorrectionIDs).To(BeEmpty())
		})

		It("drops the old sort name, which is likely as wrong as the name", func() {
			mf := read(model.RawTags{"ARTIST": {"Greatful Dead"}, "ARTISTSORT": {"Greatful Dead, The"}}, fix)
			Expect(mf.Participants.First(model.RoleArtist).SortArtistName).To(BeEmpty())
		})

		It("follows a renamed Artist renamed again", func() {
			mf := read(model.RawTags{"ARTIST": {"Greatful Dead"}}, fix, rename("c2", "Grateful Dead", "The Grateful Dead"))
			Expect(mf.Artist).To(Equal("The Grateful Dead"))
			Expect(mf.CorrectionIDs).To(ConsistOf("c1", "c2"))
		})

		It("ignores a Correction being undone", func() {
			undoing := fix
			undoing.Undoing = true
			mf := read(model.RawTags{"ARTIST": {"Greatful Dead"}}, undoing)
			Expect(mf.Artist).To(Equal("Greatful Dead"))
		})
	})

	Describe("album Correction", func() {
		album := func(id string, fix model.Correction) model.Correction {
			fix.ID, fix.Kind, fix.MatchKey = id, model.CorrectionAlbum, "folderID"
			return fix
		}
		tagged := model.RawTags{"ARTIST": {"Greatful Dead"}, "ALBUMARTIST": {"Greatful Dead"}, "ALBUM": {"Liv"},
			"TITLE": {"Ripple"}, "DATE": {"1970"}, "ALBUMSORT": {"Liv"}}
		fix := album("a1", model.Correction{AlbumName: "Live", AlbumArtist: "Grateful Dead", Year: 1971})

		It("reads the File as if tagged with the corrected title, album artist and year", func() {
			mf := read(tagged, fix)
			want := read(model.RawTags{"ARTIST": {"Greatful Dead"}, "ALBUMARTIST": {"Grateful Dead"}, "ALBUM": {"Live"},
				"TITLE": {"Ripple"}, "DATE": {"1971"}})

			Expect(mf.Album).To(Equal("Live"))
			Expect(mf.SortAlbumName).To(BeEmpty())
			Expect(mf.AlbumArtist).To(Equal("Grateful Dead"))
			Expect(mf.AlbumArtistID).To(Equal(want.AlbumArtistID))
			Expect(mf.Participants[model.RoleAlbumArtist]).To(Equal(want.Participants[model.RoleAlbumArtist]))
			Expect(mf.Year).To(Equal(1971))
			Expect(mf.Date).To(Equal("1971"))
			Expect(mf.AlbumID).To(Equal(want.AlbumID))
			Expect(mf.Artist).To(Equal("Greatful Dead"), "the Track artist is no album value")
			Expect(mf.CorrectionIDs).To(Equal([]string{"a1"}))
		})

		It("keeps the persistent ID of the uncorrected tags, so the Track is still the same Track", func() {
			Expect(read(tagged, fix).PID).To(Equal(read(tagged).PID))
		})

		It("leaves what it does not correct as tagged", func() {
			mf := read(tagged, album("a1", model.Correction{Year: 1971}))
			Expect(mf.Album).To(Equal("Liv"))
			Expect(mf.AlbumArtist).To(Equal("Greatful Dead"))
			Expect(mf.Year).To(Equal(1971))
		})

		It("applies only to Files in the album's folders", func() {
			other := fix
			other.MatchKey = "otherFolder"
			Expect(read(tagged, other).Album).To(Equal("Liv"))

			c := metadata.NewCorrections(model.Corrections{other})
			c.MatchAlbumFolders("otherFolder", []string{"otherFolder", "folderID"})
			props.Tags = tagged
			mf := metadata.New("Greatful Dead/Live/01.mp3", props).ToCorrectedMediaFile(1, "folderID", c)
			Expect(mf.Album).To(Equal("Live"), "the album's other folders (e.g. its other discs) match too")
		})

		It("ignores a Correction being undone", func() {
			undoing := fix
			undoing.Undoing = true
			Expect(read(tagged, undoing).Album).To(Equal("Liv"))
		})

		Describe("year", func() {
			year := album("a1", model.Correction{Year: 1971})

			It("sets the year of every date the File has, dropping a month and day of another year", func() {
				mf := read(model.RawTags{"ALBUM": {"Live"}, "DATE": {"1970-05-01"}, "ORIGINALDATE": {"1969"},
					"RELEASEDATE": {"1971-02-03"}}, year)
				Expect(mf.Date).To(Equal("1971"))
				Expect(mf.OriginalYear).To(Equal(1971))
				Expect(mf.OriginalDate).To(Equal("1971"))
				Expect(mf.ReleaseDate).To(Equal("1971-02-03"), "a date already in that year is kept whole")
			})

			It("gives a File with no date the year", func() {
				mf := read(model.RawTags{"ALBUM": {"Live"}}, year)
				Expect(mf.Year).To(Equal(1971))
				Expect(mf.OriginalDate).To(BeEmpty())
				Expect(mf.ReleaseDate).To(BeEmpty())
			})

			It("feeds the corrected release date to the album ID", func() {
				mf := read(model.RawTags{"ALBUM": {"Live"}, "RELEASEDATE": {"1970"}}, year)
				Expect(mf.AlbumID).To(Equal(read(model.RawTags{"ALBUM": {"Live"}, "RELEASEDATE": {"1971"}}).AlbumID))
			})
		})

		Describe("and artist renames", func() {
			It("wins over a rename of the tagged album artist, which still renames the Track artist", func() {
				ours := album("a1", model.Correction{AlbumArtist: "The Dead"})
				mf := read(tagged, ours, rename("c1", "Greatful Dead", "Grateful Dead"))
				Expect(mf.AlbumArtist).To(Equal("The Dead"))
				Expect(mf.Artist).To(Equal("Grateful Dead"))
				Expect(mf.CorrectionIDs).To(Equal([]string{"a1", "c1"}))
			})

			It("follows a rename of the album artist it sets", func() {
				ours := album("a1", model.Correction{AlbumArtist: "The Dead"})
				mf := read(model.RawTags{"ARTIST": {"Someone"}, "ALBUM": {"Live"}}, ours, rename("c1", "The Dead", "Grateful Dead"))
				Expect(mf.AlbumArtist).To(Equal("Grateful Dead"))
				Expect(mf.AlbumArtistID).To(Equal(metadata.ArtistID("Grateful Dead")))
				Expect(mf.CorrectionIDs).To(Equal([]string{"a1", "c1"}))
			})
		})
	})

	Describe("ArtistIDMoves", func() {
		It("maps the ID of each renamed Artist to the ID of its new name, and back for one being undone", func() {
			undoing := rename("c2", "Jimi Hendrix", "Jimi Hendrix Experience")
			undoing.Undoing = true
			c := metadata.NewCorrections(model.Corrections{rename("c1", "Greatful Dead", "Grateful Dead"), undoing})

			Expect(c.ArtistIDMoves()).To(ConsistOf(
				metadata.IDMove{CorrectionID: "c1", From: metadata.ArtistID("Greatful Dead"), To: metadata.ArtistID("Grateful Dead")},
				metadata.IDMove{CorrectionID: "c2", From: metadata.ArtistID("Jimi Hendrix Experience"), To: metadata.ArtistID("Jimi Hendrix"), Undo: true},
			))
		})

		It("applies an artist merge like a rename, and marks its moves as merges", func() {
			merge := rename("c1", "The Grateful Dead", "Grateful Dead")
			merge.Kind = model.CorrectionArtistMerge
			c := metadata.NewCorrections(model.Corrections{merge})

			Expect(read(model.RawTags{"ARTIST": {"the grateful dead"}}, merge).Artist).To(Equal("Grateful Dead"))
			Expect(c.ArtistIDMoves()).To(ConsistOf(
				metadata.IDMove{CorrectionID: "c1", From: metadata.ArtistID("The Grateful Dead"), To: metadata.ArtistID("Grateful Dead"), Merge: true},
			))
		})
	})
})
