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

	Describe("ArtistIDMoves", func() {
		It("maps the ID of each renamed Artist to the ID of its new name, and back for one being undone", func() {
			undoing := rename("c2", "Jimi Hendrix", "Jimi Hendrix Experience")
			undoing.Undoing = true
			c := metadata.NewCorrections(model.Corrections{rename("c1", "Greatful Dead", "Grateful Dead"), undoing})

			Expect(c.ArtistIDMoves()).To(ConsistOf(
				metadata.IDMove{From: metadata.ArtistID("Greatful Dead"), To: metadata.ArtistID("Grateful Dead")},
				metadata.IDMove{From: metadata.ArtistID("Jimi Hendrix Experience"), To: metadata.ArtistID("Jimi Hendrix")},
			))
		})
	})
})
