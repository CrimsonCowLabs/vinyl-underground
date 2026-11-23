package corrections_test

import (
	"context"
	"time"

	"github.com/navidrome/navidrome/core/corrections"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/persistence"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// What stays app data once the overlay is gone (#165): the images marked wrong for an album.
var _ = Describe("Corrections", func() {
	var ctx context.Context
	var ds model.DataStore
	var svc corrections.Corrections

	BeforeEach(func() {
		ctx = request.WithUser(context.Background(), model.User{ID: "admin-id", UserName: "admin", IsAdmin: true})
		ds = persistence.New(db.Db())
		for _, table := range []string{"correction", "media_file", "folder", "library_artist", "artist"} {
			_, err := db.Db().Exec("delete from " + table)
			Expect(err).ToNot(HaveOccurred())
		}
		svc = corrections.NewWithOptions(ds, corrections.Options{})
	})

	Describe("album covers", func() {
		addAlbum := func(id string, folderIDs ...string) model.Album {
			GinkgoHelper()
			al := model.Album{ID: id, Name: "Record", AlbumArtist: "Band", LibraryID: 1, FolderIDs: folderIDs}
			Expect(ds.Album(ctx).Put(&al)).To(Succeed())
			DeferCleanup(func() { _, _ = db.Db().Exec("delete from album where id = ?", id) })
			return al
		}
		queued := func(albumID string) bool {
			_, err := ds.ArtworkQueue(ctx).Get(model.KindAlbumArtwork, albumID, model.ImageTypePrimary)
			return err == nil
		}
		clearQueue := func() {
			_, err := db.Db().Exec("delete from artwork_queue")
			Expect(err).ToNot(HaveOccurred())
		}

		addTrack := func(id, albumID string) {
			GinkgoHelper()
			mf := model.MediaFile{ID: id, AlbumID: albumID, LibraryID: 1, Path: id + ".mp3", HasCoverArt: true}
			Expect(ds.MediaFile(ctx).Put(&mf)).To(Succeed())
		}
		trackQueued := func(id string) bool {
			_, err := ds.ArtworkQueue(ctx).Get(model.KindMediaFileArtwork, id, model.ImageTypePrimary)
			return err == nil
		}

		BeforeEach(clearQueue)

		Describe("BlockAlbumCover", func() {
			const shown = "1111111111111111"
			showing := func(albumID, hash, source, path string) {
				GinkgoHelper()
				Expect(ds.Artwork(ctx).PutImage(&model.Artwork{Hash: hash, Mime: "image/jpeg"})).To(Succeed())
				Expect(ds.Artwork(ctx).PutItemArtwork(&model.ItemArtwork{ItemKind: model.KindAlbumArtwork.Prefix(),
					ItemID: albumID, ImageType: model.ImageTypePrimary, Hash: hash, Source: source, SourcePath: path,
					AttemptedAt: time.Now()})).To(Succeed())
				DeferCleanup(func() { _, _ = db.Db().Exec("delete from item_artwork") })
			}

			It("blocks the image the album shows, and refreshes the album and its tracks' art", func() {
				al := addAlbum("al-1", "folder-cd2", "folder-cd1")
				addTrack("t-1", al.ID)
				showing(al.ID, shown, "folder", "/music/Band/Record/cover.jpg")

				res, err := svc.BlockAlbumCover(ctx, al.ID, shown)
				Expect(err).ToNot(HaveOccurred())
				Expect(res.Done).To(BeTrue())
				Expect(res.Correction.Kind).To(Equal(model.CorrectionAlbumCoverBlock))
				Expect(res.Correction.From).To(Equal("Band – Record"))
				Expect(res.Correction.To).To(Equal("cover.jpg"))
				Expect(res.Correction.CreatedBy).To(Equal("admin"))

				blocks, err := model.AlbumCorrections(ds.Correction(ctx), model.CorrectionAlbumCoverBlock, al)
				Expect(err).ToNot(HaveOccurred())
				Expect(blocks).To(HaveLen(1))
				Expect(blocks[0].MatchKey).To(Equal("folder-cd1"))
				Expect(blocks[0].ImageHash).To(Equal(shown))
				Expect(queued(al.ID)).To(BeTrue())
				Expect(trackQueued("t-1")).To(BeTrue())
			})

			It("refreshes every album sharing the folder, as the block applies to them all", func() {
				al := addAlbum("al-1", "folder-cd1")
				other := addAlbum("al-2", "folder-cd1")
				showing(al.ID, shown, "folder", "/music/Singles/folder.jpg")

				_, err := svc.BlockAlbumCover(ctx, al.ID, shown)
				Expect(err).ToNot(HaveOccurred())
				Expect(queued(other.ID)).To(BeTrue())
			})

			It("keeps several blocks per album", func() {
				al := addAlbum("al-1", "folder-cd1")
				showing(al.ID, shown, "embedded", "/music/Band/Record/01.mp3")
				_, err := svc.BlockAlbumCover(ctx, al.ID, shown)
				Expect(err).ToNot(HaveOccurred())
				showing(al.ID, "2222222222222222", "external:lastfm", "")
				res, err := svc.BlockAlbumCover(ctx, al.ID, "2222222222222222")
				Expect(err).ToNot(HaveOccurred())
				Expect(res.Correction.To).To(Equal("lastfm"))

				blocks, err := model.AlbumCorrections(ds.Correction(ctx), model.CorrectionAlbumCoverBlock, al)
				Expect(err).ToNot(HaveOccurred())
				Expect(blocks).To(HaveLen(2))
			})

			It("refuses when the album's cover is no longer the one shown, or there is none", func() {
				al := addAlbum("al-1", "folder-cd1")
				_, err := svc.BlockAlbumCover(ctx, al.ID, shown)
				Expect(err).To(MatchError(corrections.ErrCoverChanged))

				showing(al.ID, "2222222222222222", "folder", "/music/cover.jpg")
				_, err = svc.BlockAlbumCover(ctx, al.ID, shown)
				Expect(err).To(MatchError(corrections.ErrCoverChanged))

				_, err = svc.BlockAlbumCover(ctx, "nope", shown)
				Expect(err).To(MatchError(model.ErrNotFound))
				all, err := svc.GetAll(ctx)
				Expect(err).ToNot(HaveOccurred())
				Expect(all).To(BeEmpty())
			})

			It("undoes a block at once, refreshing the album and its tracks' art", func() {
				al := addAlbum("al-1", "folder-cd1")
				addTrack("t-1", al.ID)
				showing(al.ID, shown, "folder", "/music/cover.jpg")
				res, err := svc.BlockAlbumCover(ctx, al.ID, shown)
				Expect(err).ToNot(HaveOccurred())
				clearQueue()

				undone, err := svc.Undo(ctx, res.Correction.ID)
				Expect(err).ToNot(HaveOccurred())
				Expect(undone.Done).To(BeTrue())
				_, err = ds.Correction(ctx).Get(res.Correction.ID)
				Expect(err).To(MatchError(model.ErrNotFound))
				Expect(queued(al.ID)).To(BeTrue())
				Expect(trackQueued("t-1")).To(BeTrue())
			})
		})
	})

	Describe("Undo", func() {
		It("refuses a Correction the migration writes into the Files", func() {
			cr := model.Correction{Kind: model.CorrectionArtistRename, MatchKey: "greatful dead", From: "Greatful Dead", To: "Grateful Dead"}
			Expect(ds.Correction(ctx).Add(&cr)).To(Succeed())
			_, err := svc.Undo(ctx, cr.ID)
			Expect(err).To(MatchError(corrections.ErrMigrateFirst))
			Expect(ds.Correction(ctx).Get(cr.ID)).ToNot(BeNil())
		})

		It("refuses a picked MusicBrainz release, which the migration writes into the Files too (#170)", func() {
			cr := model.Correction{Kind: model.CorrectionAlbum, MatchKey: "folder", From: "Record", To: "MusicBrainz: Record",
				MbzAlbumID: "a7f8ed5e-1111-4a1b-9c1d-123456789abc", MbzReleaseGroupID: "b7f8ed5e-1111-4a1b-9c1d-123456789abc"}
			Expect(ds.Correction(ctx).Add(&cr)).To(Succeed())
			_, err := svc.Undo(ctx, cr.ID)
			Expect(err).To(MatchError(corrections.ErrMigrateFirst))
		})

		It("returns not found for an unknown Correction", func() {
			_, err := svc.Undo(ctx, "nope")
			Expect(err).To(MatchError(model.ErrNotFound))
		})
	})
})
