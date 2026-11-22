package persistence

import (
	"context"

	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CorrectionRepository", func() {
	var repo model.CorrectionRepository
	var mfRepo model.MediaFileRepository

	rename := func(from, to string) *model.Correction {
		return &model.Correction{Kind: model.CorrectionArtistRename, MatchKey: model.ArtistNameKey(from), From: from, To: to}
	}
	putTrack := func(id, folderID string, missing bool, correctionIDs ...string) {
		t := mf(model.MediaFile{ID: id, Path: "/corrections/" + id + ".mp3", FolderID: folderID, Title: id,
			Missing: missing, CorrectionIDs: correctionIDs})
		Expect(mfRepo.Put(&t)).To(Succeed())
		DeferCleanup(func() { _, _ = GetDBXBuilder().NewQuery("DELETE FROM media_file WHERE id = '" + id + "'").Execute() })
	}

	BeforeEach(func() {
		ctx := request.WithUser(log.NewContext(context.TODO()), adminUser)
		repo = NewCorrectionRepository(ctx, GetDBXBuilder())
		mfRepo = NewMediaFileRepository(ctx, GetDBXBuilder())
		_, err := GetDBXBuilder().NewQuery("DELETE FROM correction").Execute()
		Expect(err).ToNot(HaveOccurred())
	})

	It("adds a Correction, recording who made it and when", func() {
		c := rename("Greatful Dead", "Grateful Dead")
		Expect(repo.Add(c)).To(Succeed())
		Expect(c.ID).ToNot(BeEmpty())

		got, err := repo.Get(c.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Kind).To(Equal(model.CorrectionArtistRename))
		Expect(got.MatchKey).To(Equal("greatful dead"))
		Expect(got.From).To(Equal("Greatful Dead"))
		Expect(got.To).To(Equal("Grateful Dead"))
		Expect(got.CreatedBy).To(Equal(adminUser.UserName))
		Expect(got.CreatedAt).ToNot(BeZero())
		Expect(got.Undoing).To(BeFalse())
	})

	It("rejects a second Correction matching the same thing", func() {
		Expect(repo.Add(rename("Greatful Dead", "Grateful Dead"))).To(Succeed())
		Expect(repo.Add(rename("greatful  dead", "The Grateful Dead"))).To(MatchError(model.ErrAlreadyExists))
		got, err := repo.GetByMatchKey(model.CorrectionArtistRename, "greatful dead")
		Expect(err).ToNot(HaveOccurred())
		Expect(got.To).To(Equal("Grateful Dead"))
		_, err = repo.GetByMatchKey(model.CorrectionArtistRename, "grateful dead")
		Expect(err).To(MatchError(model.ErrNotFound))
	})

	It("lists Corrections newest first, counting the Tracks each applies to now", func() {
		older := rename("Greatful Dead", "Grateful Dead")
		newer := rename("Jimy Hendrix", "Jimi Hendrix")
		Expect(repo.Add(older)).To(Succeed())
		Expect(repo.Add(newer)).To(Succeed())
		_, err := GetDBXBuilder().NewQuery("UPDATE correction SET created_at = '2020-01-01' WHERE id = '" + older.ID + "'").Execute()
		Expect(err).ToNot(HaveOccurred())
		putTrack("cr-1", "folder-a", false, older.ID)
		putTrack("cr-2", "folder-b", false, older.ID, newer.ID)
		putTrack("cr-3", "folder-c", true, newer.ID) // missing Files don't count

		all, err := repo.GetAll()
		Expect(err).ToNot(HaveOccurred())
		Expect(all).To(HaveLen(2))
		Expect(all[0].ID).To(Equal(newer.ID))
		Expect(all[0].TrackCount).To(Equal(1))
		Expect(all[1].TrackCount).To(Equal(2))
	})

	It("reports an orphaned Correction with no Tracks", func() {
		Expect(repo.Add(rename("Nobody", "Somebody"))).To(Succeed())
		all, err := repo.GetAll()
		Expect(err).ToNot(HaveOccurred())
		Expect(all[0].TrackCount).To(BeZero())
	})

	Describe("album cover choices", func() {
		putAlbumTrack := func(id, albumID, folderID string, missing bool) {
			t := mf(model.MediaFile{ID: id, Path: "/corrections/" + id + ".mp3", FolderID: folderID, Title: id,
				AlbumID: albumID, Missing: missing})
			Expect(mfRepo.Put(&t)).To(Succeed())
			DeferCleanup(func() { _, _ = GetDBXBuilder().NewQuery("DELETE FROM media_file WHERE id = '" + id + "'").Execute() })
		}
		putAlbum := func(id string, folderIDs ...string) {
			ctx := request.WithUser(log.NewContext(context.TODO()), adminUser)
			al := model.Album{ID: id, Name: id, LibraryID: 1, FolderIDs: folderIDs}
			Expect(NewAlbumRepository(ctx, GetDBXBuilder()).Put(&al)).To(Succeed())
			DeferCleanup(func() { _, _ = GetDBXBuilder().NewQuery("DELETE FROM album WHERE id = '" + id + "'").Execute() })
		}
		choice := func(folderID string) *model.Correction {
			return &model.Correction{Kind: model.CorrectionAlbumCover, MatchKey: folderID, From: "Album", To: "cover.jpg",
				ImageHash: "0123456789abcdef", ImageMime: "image/jpeg"}
		}

		It("keeps the chosen image", func() {
			c := choice("cover-folder-a")
			Expect(repo.Add(c)).To(Succeed())
			got, err := repo.GetByMatchKey(model.CorrectionAlbumCover, "cover-folder-a")
			Expect(err).ToNot(HaveOccurred())
			Expect(got.ImageHash).To(Equal("0123456789abcdef"))
			Expect(got.ImageMime).To(Equal("image/jpeg"))
		})

		It("counts the Tracks of the album in the chosen folder, all its discs included", func() {
			putAlbum("cover-album", "cover-cd1", "cover-cd2")
			putAlbumTrack("cv-1", "cover-album", "cover-cd1", false)
			putAlbumTrack("cv-2", "cover-album", "cover-cd2", false)
			putAlbumTrack("cv-3", "cover-album", "cover-cd2", true) // missing Files don't count
			Expect(repo.Add(choice("cover-cd1"))).To(Succeed())

			all, err := repo.GetAll()
			Expect(err).ToNot(HaveOccurred())
			Expect(all).To(HaveLen(1))
			Expect(all[0].TrackCount).To(Equal(2))
		})

		It("is orphaned once no album is in the folder", func() {
			putAlbum("cover-album", "cover-elsewhere")
			putAlbumTrack("cv-1", "cover-album", "cover-elsewhere", false)
			Expect(repo.Add(choice("cover-moved-away"))).To(Succeed())

			all, err := repo.GetAll()
			Expect(err).ToNot(HaveOccurred())
			Expect(all[0].TrackCount).To(BeZero())
		})

		It("keeps what an album Correction sets", func() {
			c := &model.Correction{Kind: model.CorrectionAlbum, MatchKey: "album-folder", From: "Band – Recrod",
				To: "The Band – Record (1999)", AlbumName: "Record", AlbumArtist: "The Band", Year: 1999}
			Expect(repo.Add(c)).To(Succeed())
			got, err := repo.GetByMatchKey(model.CorrectionAlbum, "album-folder")
			Expect(err).ToNot(HaveOccurred())
			Expect(got.AlbumName).To(Equal("Record"))
			Expect(got.AlbumArtist).To(Equal("The Band"))
			Expect(got.Year).To(Equal(1999))
		})

		It("returns the folders an album Correction matches: its own and those of the albums in it", func() {
			putAlbum("discs", "disc-cd1", "disc-cd2")
			putAlbum("elsewhere", "disc-other")
			Expect(repo.AlbumFolderIDs("disc-cd2")).To(Equal([]string{"disc-cd1", "disc-cd2"}))
			Expect(repo.AlbumFolderIDs("disc-gone")).To(Equal([]string{"disc-gone"}))
		})
	})

	It("returns the folders of the Tracks a Correction applies to", func() {
		c := rename("Greatful Dead", "Grateful Dead")
		Expect(repo.Add(c)).To(Succeed())
		putTrack("cr-1", "folder-a", false, c.ID)
		putTrack("cr-2", "folder-a", false, c.ID)
		putTrack("cr-3", "folder-b", false, c.ID)
		putTrack("cr-4", "folder-c", false)

		Expect(repo.FolderIDs(c.ID)).To(ConsistOf("folder-a", "folder-b"))
	})

	It("reads back the Corrections applied to a Track", func() {
		putTrack("cr-1", "folder-a", false, "x", "y")
		got, err := mfRepo.Get("cr-1")
		Expect(err).ToNot(HaveOccurred())
		Expect(got.CorrectionIDs).To(Equal([]string{"x", "y"}))
	})

	It("marks a Correction as being undone, and deletes it", func() {
		c := rename("Greatful Dead", "Grateful Dead")
		Expect(repo.Add(c)).To(Succeed())
		Expect(repo.SetUndoing(c.ID)).To(Succeed())
		got, err := repo.Get(c.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Undoing).To(BeTrue())

		Expect(repo.Delete(c.ID)).To(Succeed())
		_, err = repo.Get(c.ID)
		Expect(err).To(MatchError(model.ErrNotFound))
		Expect(repo.Delete(c.ID)).To(MatchError(model.ErrNotFound))
	})
})
