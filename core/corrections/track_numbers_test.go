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

var _ = Describe("CorrectTrackNumbers", func() {
	var ctx context.Context
	var ds model.DataStore
	var refresher *fakeRefresher
	var svc corrections.Corrections
	var al model.Album
	var cd1, cd2 *model.Folder
	var targets []model.ScanTarget

	// numberTrack gives a Track its title and numbers, as the scanner read them
	numberTrack := func(id, title string, disc, track int) {
		GinkgoHelper()
		_, err := db.Db().Exec("update media_file set title = ?, disc_number = ?, track_number = ? where id = ?",
			title, disc, track, id)
		Expect(err).ToNot(HaveOccurred())
	}

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
		al = model.Album{ID: "al-1", Name: "Record", AlbumArtist: "Band", LibraryID: 1, FolderIDs: []string{cd1.ID, cd2.ID}}
		Expect(ds.Album(ctx).Put(&al)).To(Succeed())
		addTrack("mf-1", al.ID, cd1)
		addTrack("mf-2", al.ID, cd1)
		addTrack("mf-3", al.ID, cd2)
		numberTrack("mf-1", "One", 1, 2)
		numberTrack("mf-2", "Two", 1, 1)
		numberTrack("mf-3", "Three", 1, 3)
		targets = []model.ScanTarget{{LibraryID: 1, FolderPath: "Band/Record/CD1"}, {LibraryID: 1, FolderPath: "Band/Record/CD2"}}
		refresher.targets[al.ID] = targets
	})

	key := func() string { return model.AlbumFolderKey(al) }
	stored := func() *model.Correction {
		GinkgoHelper()
		got, err := ds.Correction(ctx).GetByMatchKey(model.CorrectionTrackNumbers, key())
		Expect(err).ToNot(HaveOccurred())
		return got
	}
	pathOf := func(id string) string {
		GinkgoHelper()
		mf, err := ds.MediaFile(ctx).Get(id)
		Expect(err).ToNot(HaveOccurred())
		return mf.Path
	}
	swap := []corrections.TrackNumber{{ID: "mf-1", DiscNumber: 1, TrackNumber: 1}, {ID: "mf-2", DiscNumber: 1, TrackNumber: 2}}

	It("saves the album's Tracks' numbers together, each keyed by its File, and rescans and refreshes the album", func() {
		res, err := svc.CorrectTrackNumbers(ctx, al.ID, append(swap, corrections.TrackNumber{ID: "mf-3", DiscNumber: 2, TrackNumber: 1}))
		Expect(err).ToNot(HaveOccurred())
		Expect(res.Done).To(BeTrue())
		Expect(res.AlbumID).To(Equal(al.ID), "track numbers are no part of the album's ID")
		Expect(res.Correction.Kind).To(Equal(model.CorrectionTrackNumbers))
		Expect(res.Correction.From).To(Equal("Band – Record"))
		Expect(res.Correction.To).To(Equal("1-1 One, 1-2 Two, 2-1 Three"))
		Expect(res.Correction.CreatedBy).To(Equal("admin"))
		Expect(refresher.scanned()).To(Equal([][]model.ScanTarget{targets}))
		Expect(refresher.items).To(Equal([][]refresh.Item{{{Kind: model.KindAlbumArtwork, ID: al.ID}}}))

		Expect(stored().TrackNumbers).To(Equal(model.TrackNumberFixes{
			{LibraryID: 1, Path: pathOf("mf-1"), Title: "One", FromDisc: 1, FromTrack: 2, Disc: 1, Track: 1},
			{LibraryID: 1, Path: pathOf("mf-2"), Title: "Two", FromDisc: 1, FromTrack: 1, Disc: 1, Track: 2},
			{LibraryID: 1, Path: pathOf("mf-3"), Title: "Three", FromDisc: 1, FromTrack: 3, Disc: 2, Track: 1},
		}))
	})

	It("leaves out the Tracks whose numbers are unchanged", func() {
		_, err := svc.CorrectTrackNumbers(ctx, al.ID, append(swap, corrections.TrackNumber{ID: "mf-3", DiscNumber: 1, TrackNumber: 3}))
		Expect(err).ToNot(HaveOccurred())
		Expect(stored().TrackNumbers).To(HaveLen(2))
	})

	It("saves a duplicate number on a disc, which the dialog only warns about", func() {
		_, err := svc.CorrectTrackNumbers(ctx, al.ID, []corrections.TrackNumber{{ID: "mf-1", DiscNumber: 1, TrackNumber: 1}})
		Expect(err).ToNot(HaveOccurred())
	})

	It("replaces the album's earlier Correction, keeping the Tracks it renumbered and their tagged numbers", func() {
		_, err := svc.CorrectTrackNumbers(ctx, al.ID, swap)
		Expect(err).ToNot(HaveOccurred())
		numberTrack("mf-1", "One", 1, 1) // as the rescan reads them now
		numberTrack("mf-2", "Two", 1, 2)

		res, err := svc.CorrectTrackNumbers(ctx, al.ID, []corrections.TrackNumber{
			{ID: "mf-2", DiscNumber: 1, TrackNumber: 4}, {ID: "mf-3", DiscNumber: 2, TrackNumber: 1}})
		Expect(err).ToNot(HaveOccurred())
		Expect(res.Correction.To).To(Equal("1-1 One, 1-4 Two, 2-1 Three"))

		all, err := svc.GetAll(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(all).To(HaveLen(1))
		Expect(all[0].TrackNumbers).To(ConsistOf(
			model.TrackNumberFix{LibraryID: 1, Path: pathOf("mf-1"), Title: "One", FromDisc: 1, FromTrack: 2, Disc: 1, Track: 1},
			model.TrackNumberFix{LibraryID: 1, Path: pathOf("mf-2"), Title: "Two", FromDisc: 1, FromTrack: 1, Disc: 1, Track: 4},
			model.TrackNumberFix{LibraryID: 1, Path: pathOf("mf-3"), Title: "Three", FromDisc: 1, FromTrack: 3, Disc: 2, Track: 1},
		))
	})

	It("drops a Track set back to its tagged numbers, and the Correction once none is left", func() {
		_, err := svc.CorrectTrackNumbers(ctx, al.ID, swap)
		Expect(err).ToNot(HaveOccurred())
		numberTrack("mf-1", "One", 1, 1)
		numberTrack("mf-2", "Two", 1, 2)

		_, err = svc.CorrectTrackNumbers(ctx, al.ID, []corrections.TrackNumber{{ID: "mf-1", DiscNumber: 1, TrackNumber: 2}})
		Expect(err).ToNot(HaveOccurred())
		Expect(stored().TrackNumbers).To(HaveLen(1))
		numberTrack("mf-1", "One", 1, 2)

		refresher.scans = nil
		var undoingDuringScan bool
		refresher.onScan = func() {
			c, err := ds.Correction(ctx).GetByMatchKey(model.CorrectionTrackNumbers, key())
			undoingDuringScan = err == nil && c.Undoing
		}
		res, err := svc.CorrectTrackNumbers(ctx, al.ID, []corrections.TrackNumber{{ID: "mf-2", DiscNumber: 1, TrackNumber: 1}})
		Expect(err).ToNot(HaveOccurred())
		Expect(res.Done).To(BeTrue())
		Expect(undoingDuringScan).To(BeTrue(), "it is undone like any Correction, so a failed rescan can be retried")
		Expect(refresher.scanned()).To(Equal([][]model.ScanTarget{targets}), "the Tracks are read as tagged again")
		_, err = ds.Correction(ctx).GetByMatchKey(model.CorrectionTrackNumbers, key())
		Expect(err).To(MatchError(model.ErrNotFound))
	})

	It("rejects no change, numbers out of range, Tracks of another album and an unknown album", func() {
		addTrack("mf-9", "al-other", model.NewFolder(model.Library{ID: 1}, "Band/Other"))
		for _, bad := range [][]corrections.TrackNumber{
			nil,
			{{ID: "mf-1", DiscNumber: 1, TrackNumber: 2}},
			{{ID: "mf-1", DiscNumber: 1, TrackNumber: 0}},
			{{ID: "mf-1", DiscNumber: 0, TrackNumber: 2}},
			{{ID: "mf-1", DiscNumber: 1, TrackNumber: 100000}},
			{{ID: "mf-1", DiscNumber: 1, TrackNumber: 1}, {ID: "mf-1", DiscNumber: 1, TrackNumber: 3}},
			{{ID: "mf-9", DiscNumber: 1, TrackNumber: 1}},
			{{ID: "nope", DiscNumber: 1, TrackNumber: 1}},
		} {
			_, err := svc.CorrectTrackNumbers(ctx, al.ID, bad)
			Expect(err).To(MatchError(model.ErrValidation), "%+v", bad)
		}
		_, err := svc.CorrectTrackNumbers(ctx, "nope", swap)
		Expect(err).To(MatchError(model.ErrNotFound))
		Expect(refresher.scanned()).To(BeEmpty())
	})

	It("keeps a Track with no disc number without one when only its track number changes", func() {
		numberTrack("mf-1", "One", 0, 2)
		_, err := svc.CorrectTrackNumbers(ctx, al.ID, []corrections.TrackNumber{{ID: "mf-1", DiscNumber: 0, TrackNumber: 5}})
		Expect(err).ToNot(HaveOccurred())
		Expect(stored().To).To(Equal("5 One"))
	})

	It("rejects an album sharing a folder with another album, as the Correction groups the folder's Tracks", func() {
		addTrack("mf-4", "al-other", cd2)
		_, err := svc.CorrectTrackNumbers(ctx, al.ID, swap)
		Expect(err).To(MatchError(corrections.ErrSharedFolder))
	})

	Describe("Undo", func() {
		var cr model.Correction

		BeforeEach(func() {
			res, err := svc.CorrectTrackNumbers(ctx, al.ID, swap)
			Expect(err).ToNot(HaveOccurred())
			cr = res.Correction
			refresher.scans, refresher.items = nil, nil
			_, err = db.Db().Exec("update media_file set correction_ids = ? where folder_id = ?", `["`+cr.ID+`"]`, cd1.ID)
			Expect(err).ToNot(HaveOccurred())
		})

		It("marks the Correction undone while rescanning the album's folders, then deletes it", func() {
			var undoingDuringScan bool
			refresher.onScan = func() {
				c, err := ds.Correction(ctx).Get(cr.ID)
				undoingDuringScan = err == nil && c.Undoing
			}

			res, err := svc.Undo(ctx, cr.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Done).To(BeTrue())
			Expect(res.AlbumID).To(Equal(al.ID))
			Expect(undoingDuringScan).To(BeTrue())
			Expect(refresher.scanned()).To(Equal([][]model.ScanTarget{targets}))
			_, err = ds.Correction(ctx).Get(cr.ID)
			Expect(err).To(MatchError(model.ErrNotFound))
		})

		It("deletes it straight away when its Files are gone", func() {
			for _, table := range []string{"media_file", "folder", "album"} {
				_, err := db.Db().Exec("delete from " + table)
				Expect(err).ToNot(HaveOccurred())
			}
			_, err := svc.Undo(ctx, cr.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(refresher.scanned()).To(BeEmpty())
			_, err = ds.Correction(ctx).Get(cr.ID)
			Expect(err).To(MatchError(model.ErrNotFound))
		})
	})

	It("is independent of the album Correction: either can be undone first", func() {
		nums, err := svc.CorrectTrackNumbers(ctx, al.ID, swap)
		Expect(err).ToNot(HaveOccurred())
		album, err := svc.CorrectAlbum(ctx, al.ID, corrections.AlbumFix{Name: "Record!", AlbumArtist: "Band"})
		Expect(err).ToNot(HaveOccurred())
		Expect(svc.Undo(ctx, album.Correction.ID)).ToNot(BeNil())
		Expect(svc.Undo(ctx, nums.Correction.ID)).ToNot(BeNil())
	})
})
