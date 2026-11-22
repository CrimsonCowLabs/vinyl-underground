package corrections_test

import (
	"context"
	"time"

	"github.com/navidrome/navidrome/core/corrections"
	"github.com/navidrome/navidrome/core/refresh"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/persistence"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CorrectAlbum", func() {
	var ctx context.Context
	var ds model.DataStore
	var refresher *fakeRefresher
	var svc corrections.Corrections
	var al model.Album
	var cd1, cd2 *model.Folder
	var targets []model.ScanTarget

	BeforeEach(func() {
		ctx = request.WithUser(context.Background(), model.User{ID: "admin-id", UserName: "admin", IsAdmin: true})
		ds = persistence.New(db.Db())
		for _, table := range []string{"correction", "media_file", "folder", "album", "library_artist", "artist"} {
			_, err := db.Db().Exec("delete from " + table)
			Expect(err).ToNot(HaveOccurred())
		}
		refresher = &fakeRefresher{targets: map[string][]model.ScanTarget{}}
		svc = corrections.NewWithOptions(ds, refresher, corrections.Options{Wait: time.Second})

		cd1 = model.NewFolder(model.Library{ID: 1}, "Band/Record/CD1")
		cd2 = model.NewFolder(model.Library{ID: 1}, "Band/Record/CD2")
		for _, f := range []*model.Folder{cd1, cd2} {
			Expect(ds.Folder(ctx).Put(f)).To(Succeed())
		}
		al = model.Album{ID: "al-1", Name: "Recrod", AlbumArtist: "Band", MaxYear: 1998, LibraryID: 1,
			FolderIDs: []string{cd1.ID, cd2.ID}}
		Expect(ds.Album(ctx).Put(&al)).To(Succeed())
		addTrack("mf-1", al.ID, cd1)
		addTrack("mf-2", al.ID, cd2)
		targets = []model.ScanTarget{{LibraryID: 1, FolderPath: "Band/Record/CD1"}, {LibraryID: 1, FolderPath: "Band/Record/CD2"}}
		refresher.targets[al.ID] = targets
	})

	key := func() string { return model.AlbumFolderKey(al) }
	fix := corrections.AlbumFix{Name: "Record", AlbumArtist: "Band", Year: 1999}

	// The scan moves the album's Tracks to the corrected album's ID
	rekeyTracks := func(albumID string) func() {
		return func() {
			_, err := db.Db().Exec("update media_file set album_id = ?", albumID)
			Expect(err).ToNot(HaveOccurred())
		}
	}

	It("saves what differs from the album, keyed by its folder, and rescans and refreshes the album", func() {
		refresher.onScan = rekeyTracks("al-2")

		res, err := svc.CorrectAlbum(ctx, al.ID, fix)
		Expect(err).ToNot(HaveOccurred())
		Expect(res.Done).To(BeTrue())
		Expect(res.AlbumID).To(Equal("al-2"))
		Expect(res.Correction.Kind).To(Equal(model.CorrectionAlbum))
		Expect(res.Correction.From).To(Equal("Band – Recrod (1998)"))
		Expect(res.Correction.To).To(Equal("Band – Record (1999)"))
		Expect(res.Correction.CreatedBy).To(Equal("admin"))
		Expect(refresher.scanned()).To(Equal([][]model.ScanTarget{targets}))
		Expect(refresher.items).To(Equal([][]refresh.Item{{{Kind: model.KindAlbumArtwork, ID: al.ID}}}))

		got, err := ds.Correction(ctx).GetByMatchKey(model.CorrectionAlbum, key())
		Expect(err).ToNot(HaveOccurred())
		Expect(got.AlbumName).To(Equal("Record"))
		Expect(got.AlbumArtist).To(BeEmpty(), "the album artist is unchanged, so later Artist renames still apply")
		Expect(got.Year).To(Equal(1999))
	})

	It("uses the album's original year as its year, as clients show it", func() {
		al.MaxOriginalYear = 1999
		Expect(ds.Album(ctx).Put(&al)).To(Succeed())
		_, err := svc.CorrectAlbum(ctx, al.ID, corrections.AlbumFix{Name: "Record", AlbumArtist: "Band", Year: 1999})
		Expect(err).ToNot(HaveOccurred())
		got, err := ds.Correction(ctx).GetByMatchKey(model.CorrectionAlbum, key())
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Year).To(BeZero())
	})

	It("replaces the album's earlier Correction, keeping what that corrected", func() {
		_, err := svc.CorrectAlbum(ctx, al.ID, corrections.AlbumFix{Name: "Record", AlbumArtist: "Band", Year: 1998})
		Expect(err).ToNot(HaveOccurred())
		al.Name = "Record" // as the rescan reads it now
		Expect(ds.Album(ctx).Put(&al)).To(Succeed())

		res, err := svc.CorrectAlbum(ctx, al.ID, corrections.AlbumFix{Name: "Record", AlbumArtist: "The Band", Year: 1998})
		Expect(err).ToNot(HaveOccurred())
		Expect(res.Correction.From).To(Equal("Band – Recrod (1998)"))
		Expect(res.Correction.To).To(Equal("The Band – Record (1998)"))

		all, err := svc.GetAll(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(all).To(HaveLen(1))
		Expect(all[0].AlbumName).To(Equal("Record"))
		Expect(all[0].AlbumArtist).To(Equal("The Band"))
	})

	It("rejects an unchanged album, an empty title or album artist, and a year that is no year", func() {
		for _, bad := range []corrections.AlbumFix{
			{Name: "Recrod", AlbumArtist: "Band", Year: 1998},
			{Name: " ", AlbumArtist: "Band", Year: 1999},
			{Name: "Record", AlbumArtist: "", Year: 1999},
			{Name: "Record", AlbumArtist: "Band", Year: 99},
		} {
			_, err := svc.CorrectAlbum(ctx, al.ID, bad)
			Expect(err).To(MatchError(model.ErrValidation), "%+v", bad)
		}
		_, err := svc.CorrectAlbum(ctx, "nope", fix)
		Expect(err).To(MatchError(model.ErrNotFound))
		Expect(refresher.scanned()).To(BeEmpty())
	})

	It("rejects an album sharing a folder with another album, which the Correction would change too", func() {
		addTrack("mf-3", "al-other", cd2)
		_, err := svc.CorrectAlbum(ctx, al.ID, fix)
		Expect(err).To(MatchError(corrections.ErrSharedFolder))
	})

	It("rejects an album artist a Correction renames away", func() {
		Expect(ds.Correction(ctx).Add(&model.Correction{Kind: model.CorrectionArtistRename,
			MatchKey: model.ArtistNameKey("The Band"), From: "The Band", To: "Band"})).To(Succeed())
		_, err := svc.CorrectAlbum(ctx, al.ID, corrections.AlbumFix{Name: "Recrod", AlbumArtist: "the band", Year: 1998})
		Expect(err).To(MatchError(corrections.ErrRenamedAway))
	})

	Describe("MusicBrainz release (Find on MusicBrainz)", func() {
		const release, group = "8a7f5c4e-0d5b-4f1e-9d3c-3b2a1f0e9d8c", "1b2c3d4e-5f60-4718-9a0b-c1d2e3f40516"
		picked := corrections.AlbumFix{Name: "Recrod", AlbumArtist: "Band", Year: 1998,
			MbzAlbumID: release, MbzReleaseGroupID: group, MbzRelease: "Record (GB, CD, 1999)"}

		It("saves the release and release group IDs alone, and rescans and refreshes the album", func() {
			refresher.onScan = rekeyTracks("al-mbz")
			res, err := svc.CorrectAlbum(ctx, al.ID, picked)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.AlbumID).To(Equal("al-mbz"))
			Expect(res.Correction.From).To(Equal("Band – Recrod (1998)"))
			Expect(res.Correction.To).To(Equal("Band – Recrod (1998) · MusicBrainz: Record (GB, CD, 1999)"))
			Expect(refresher.scanned()).To(Equal([][]model.ScanTarget{targets}))

			got, err := ds.Correction(ctx).GetByMatchKey(model.CorrectionAlbum, key())
			Expect(err).ToNot(HaveOccurred())
			Expect(got.MbzAlbumID).To(Equal(release))
			Expect(got.MbzReleaseGroupID).To(Equal(group))
			Expect(got.AlbumName).To(BeEmpty())
			Expect(got.Year).To(BeZero())
		})

		It("saves it along with a corrected title", func() {
			fix := picked
			fix.Name = "Record"
			res, err := svc.CorrectAlbum(ctx, al.ID, fix)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Correction.To).To(Equal("Band – Record (1998) · MusicBrainz: Record (GB, CD, 1999)"))
			Expect(res.Correction.AlbumName).To(Equal("Record"))
			Expect(res.Correction.MbzAlbumID).To(Equal(release))
		})

		It("keeps the release when the album is edited again without picking one", func() {
			_, err := svc.CorrectAlbum(ctx, al.ID, picked)
			Expect(err).ToNot(HaveOccurred())
			al.MbzAlbumID, al.MbzReleaseGroupID = release, group // as the rescan reads it now
			Expect(ds.Album(ctx).Put(&al)).To(Succeed())

			res, err := svc.CorrectAlbum(ctx, al.ID, corrections.AlbumFix{Name: "Record", AlbumArtist: "Band", Year: 1998})
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Correction.MbzAlbumID).To(Equal(release))
			Expect(res.Correction.MbzReleaseGroupID).To(Equal(group))
			Expect(res.Correction.To).To(Equal("Band – Record (1998) · MusicBrainz: Record (GB, CD, 1999)"))
		})

		It("replaces a release picked before", func() {
			_, err := svc.CorrectAlbum(ctx, al.ID, picked)
			Expect(err).ToNot(HaveOccurred())
			other := picked
			other.MbzAlbumID, other.MbzRelease = "00000000-0000-4000-8000-000000000001", "Record (US, 12\" Vinyl, 1999)"
			res, err := svc.CorrectAlbum(ctx, al.ID, other)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Correction.MbzAlbumID).To(Equal("00000000-0000-4000-8000-000000000001"))
			Expect(res.Correction.To).To(HaveSuffix(`MusicBrainz: Record (US, 12" Vinyl, 1999)`))
		})

		It("rejects the release the album is already tagged with, and IDs that are no MusicBrainz IDs", func() {
			al.MbzAlbumID, al.MbzReleaseGroupID = release, group
			Expect(ds.Album(ctx).Put(&al)).To(Succeed())
			_, err := svc.CorrectAlbum(ctx, al.ID, picked)
			Expect(err).To(MatchError(ContainSubstring("nothing changed")))

			for _, bad := range []corrections.AlbumFix{
				{Name: "Recrod", AlbumArtist: "Band", MbzAlbumID: "not-an-id"},
				{Name: "Recrod", AlbumArtist: "Band", MbzAlbumID: release, MbzReleaseGroupID: "nope"},
				{Name: "Recrod", AlbumArtist: "Band", MbzReleaseGroupID: group},
			} {
				_, err := svc.CorrectAlbum(ctx, al.ID, bad)
				Expect(err).To(MatchError(model.ErrValidation), "%+v", bad)
			}
			Expect(refresher.scanned()).To(BeEmpty())
		})
	})

	Describe("Undo", func() {
		var cr model.Correction

		BeforeEach(func() {
			res, err := svc.CorrectAlbum(ctx, al.ID, fix)
			Expect(err).ToNot(HaveOccurred())
			cr = res.Correction
			refresher.scans, refresher.items = nil, nil
			_, err = db.Db().Exec("update media_file set correction_ids = ?", `["`+cr.ID+`"]`)
			Expect(err).ToNot(HaveOccurred())
		})

		It("marks the Correction undone while rescanning the album's folders, then deletes it", func() {
			var undoingDuringScan bool
			refresher.onScan = func() {
				c, err := ds.Correction(ctx).Get(cr.ID)
				undoingDuringScan = err == nil && c.Undoing
				rekeyTracks("al-3")()
			}

			res, err := svc.Undo(ctx, cr.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Done).To(BeTrue())
			Expect(res.AlbumID).To(Equal("al-3"))
			Expect(undoingDuringScan).To(BeTrue())
			Expect(refresher.scanned()).To(Equal([][]model.ScanTarget{targets}))
			Expect(refresher.items).To(Equal([][]refresh.Item{{{Kind: model.KindAlbumArtwork, ID: al.ID}}}))
			_, err = ds.Correction(ctx).Get(cr.ID)
			Expect(err).To(MatchError(model.ErrNotFound))
		})

		It("deletes it straight away when its folders are gone", func() {
			for _, table := range []string{"media_file", "folder", "album"} {
				_, err := db.Db().Exec("delete from " + table)
				Expect(err).ToNot(HaveOccurred())
			}
			res, err := svc.Undo(ctx, cr.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Done).To(BeTrue())
			Expect(refresher.scanned()).To(BeEmpty())
			_, err = ds.Correction(ctx).Get(cr.ID)
			Expect(err).To(MatchError(model.ErrNotFound))
		})
	})

	It("keeps an Artist rename from being undone while an album Correction sets its new name", func() {
		rename := model.Correction{Kind: model.CorrectionArtistRename, MatchKey: model.ArtistNameKey("Bnad"),
			From: "Bnad", To: "The Band"}
		Expect(ds.Correction(ctx).Add(&rename)).To(Succeed())
		ours, err := svc.CorrectAlbum(ctx, al.ID, corrections.AlbumFix{Name: "Recrod", AlbumArtist: "the band", Year: 1998})
		Expect(err).ToNot(HaveOccurred())

		_, err = svc.Undo(ctx, rename.ID)
		Expect(err).To(MatchError(corrections.ErrUndoLaterFirst))
		Expect(svc.Undo(ctx, ours.Correction.ID)).ToNot(BeNil())
		Expect(svc.Undo(ctx, rename.ID)).ToNot(BeNil())
	})
})

func addTrack(id, albumID string, f *model.Folder) {
	GinkgoHelper()
	ctx := request.WithUser(context.Background(), model.User{ID: "admin-id", UserName: "admin", IsAdmin: true})
	mf := model.MediaFile{ID: id, LibraryID: 1, FolderID: f.ID, AlbumID: albumID, Path: f.Path + "/" + f.Name + "/" + id + ".mp3",
		Participants: model.Participants{}}
	Expect(persistence.New(db.Db()).MediaFile(ctx).Put(&mf)).To(Succeed())
}
