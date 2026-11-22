package corrections_test

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/navidrome/navidrome/core/corrections"
	"github.com/navidrome/navidrome/core/refresh"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/metadata"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/persistence"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeRefresher records the rescans asked for. A rescan waits for release when it is set, and
// fails with err.
type fakeRefresher struct {
	refresh.Refresher
	mu      sync.Mutex
	targets map[string][]model.ScanTarget // ScanTargets answers, by artist ID
	scans   [][]model.ScanTarget
	items   [][]refresh.Item
	release chan struct{}
	err     error
	onScan  func() // runs as the scan would, before it returns
}

func (f *fakeRefresher) ScanTargets(_ context.Context, _ model.Kind, id string) ([]model.ScanTarget, error) {
	return f.targets[id], nil
}

func (f *fakeRefresher) ScanThenRefresh(_ context.Context, targets []model.ScanTarget, items ...refresh.Item) error {
	if f.release != nil {
		<-f.release
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scans = append(f.scans, targets)
	f.items = append(f.items, items)
	if f.onScan != nil {
		f.onScan()
	}
	return f.err
}

func (f *fakeRefresher) scanned() [][]model.ScanTarget {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.scans
}

var _ = Describe("Corrections", func() {
	var ctx context.Context
	var ds model.DataStore
	var refresher *fakeRefresher
	var svc corrections.Corrections
	deadTargets := []model.ScanTarget{{LibraryID: 1, FolderPath: "Greatful Dead/Live"}}

	BeforeEach(func() {
		ctx = request.WithUser(context.Background(), model.User{ID: "admin-id", UserName: "admin", IsAdmin: true})
		ds = persistence.New(db.Db())
		for _, table := range []string{"correction", "media_file", "folder", "library_artist", "artist"} {
			_, err := db.Db().Exec("delete from " + table)
			Expect(err).ToNot(HaveOccurred())
		}
		refresher = &fakeRefresher{targets: map[string][]model.ScanTarget{}}
		svc = corrections.NewWithOptions(ds, refresher, corrections.Options{Wait: time.Second})
	})

	addArtist := func(name string) model.Artist {
		GinkgoHelper()
		ar := model.Artist{ID: metadata.ArtistID(name), Name: name}
		Expect(ds.Artist(ctx).Put(&ar)).To(Succeed())
		Expect(ds.Library(ctx).AddArtist(1, ar.ID)).To(Succeed())
		return ar
	}

	Describe("RenameArtist", func() {
		It("saves a Correction and rescans the Artist's folders, refreshing the renamed Artist", func() {
			dead := addArtist("Greatful Dead")
			refresher.targets[dead.ID] = deadTargets

			res, err := svc.RenameArtist(ctx, dead.ID, "  Grateful Dead ")
			Expect(err).ToNot(HaveOccurred())

			Expect(res.Done).To(BeTrue())
			Expect(res.ArtistID).To(Equal(metadata.ArtistID("Grateful Dead")))
			Expect(res.Correction.Kind).To(Equal(model.CorrectionArtistRename))
			Expect(res.Correction.From).To(Equal("Greatful Dead"))
			Expect(res.Correction.To).To(Equal("Grateful Dead"))
			Expect(res.Correction.CreatedBy).To(Equal("admin"))
			Expect(refresher.scanned()).To(Equal([][]model.ScanTarget{deadTargets}))
			Expect(refresher.items).To(Equal([][]refresh.Item{{{Kind: model.KindArtistArtwork, ID: res.ArtistID}}}))

			all, err := svc.GetAll(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(all).To(HaveLen(1))
			Expect(all[0].MatchKey).To(Equal("greatful dead"))
		})

		It("returns before a long rescan is done, which finishes in the background", func() {
			dead := addArtist("Greatful Dead")
			refresher.targets[dead.ID] = deadTargets
			refresher.release = make(chan struct{})
			svc = corrections.NewWithOptions(ds, refresher, corrections.Options{Wait: 10 * time.Millisecond})
			reqCtx, cancel := context.WithCancel(ctx)

			res, err := svc.RenameArtist(reqCtx, dead.ID, "Grateful Dead")
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Done).To(BeFalse())
			cancel() // the request ends; the rescan must not
			close(refresher.release)
			Eventually(refresher.scanned).Should(HaveLen(1))
		})

		It("allows fixing only capitals or spaces, which keeps the Artist's ID", func() {
			dead := addArtist("grateful dead")
			res, err := svc.RenameArtist(ctx, dead.ID, "Grateful Dead")
			Expect(err).ToNot(HaveOccurred())
			Expect(res.ArtistID).To(Equal(dead.ID))
		})

		It("rejects a name an Artist already has, as merging Artists is not supported yet", func() {
			dead := addArtist("Greatful Dead")
			addArtist("Grateful Dead")
			_, err := svc.RenameArtist(ctx, dead.ID, "grateful dead")
			Expect(err).To(MatchError(corrections.ErrArtistExists))
			Expect(svc.GetAll(ctx)).To(BeEmpty())
			Expect(refresher.scanned()).To(BeEmpty())
		})

		It("rejects an empty or unchanged name, and the placeholder Artists", func() {
			dead := addArtist("Greatful Dead")
			_, err := svc.RenameArtist(ctx, dead.ID, "  ")
			Expect(err).To(MatchError(model.ErrValidation))
			_, err = svc.RenameArtist(ctx, dead.ID, "Greatful Dead")
			Expect(err).To(MatchError(model.ErrValidation))
			unknown := addArtist("[Unknown Artist]")
			_, err = svc.RenameArtist(ctx, unknown.ID, "Somebody")
			Expect(err).To(MatchError(model.ErrValidation))
		})

		It("rejects renaming an Artist back to a name a Correction renamed away", func() {
			dead := addArtist("Greatful Dead")
			_, err := svc.RenameArtist(ctx, dead.ID, "Grateful Dead")
			Expect(err).ToNot(HaveOccurred())
			_, err = db.Db().Exec("delete from artist where id = ?", dead.ID) // as the rescan would
			Expect(err).ToNot(HaveOccurred())
			renamed := addArtist("Grateful Dead")

			_, err = svc.RenameArtist(ctx, renamed.ID, "Greatful Dead")
			Expect(err).To(MatchError(corrections.ErrRenamedAway))
		})

		It("returns not found for an unknown Artist", func() {
			_, err := svc.RenameArtist(ctx, "nope", "Somebody")
			Expect(err).To(MatchError(model.ErrNotFound))
		})
	})

	Describe("Undo", func() {
		var rename model.Correction

		BeforeEach(func() {
			dead := addArtist("Greatful Dead")
			res, err := svc.RenameArtist(ctx, dead.ID, "Grateful Dead")
			Expect(err).ToNot(HaveOccurred())
			rename = res.Correction
			refresher.scans, refresher.items = nil, nil

			f := model.NewFolder(model.Library{ID: 1}, "Greatful Dead/Live")
			Expect(ds.Folder(ctx).Put(f)).To(Succeed())
			mf := model.MediaFile{ID: "mf-1", LibraryID: 1, FolderID: f.ID, Path: "Greatful Dead/Live/01.mp3",
				CorrectionIDs: []string{rename.ID}, Participants: model.Participants{}}
			Expect(ds.MediaFile(ctx).Put(&mf)).To(Succeed())
		})

		It("marks the Correction undone while rescanning the folders it applies to, then deletes it", func() {
			var undoingDuringScan bool
			refresher.onScan = func() {
				c, err := ds.Correction(ctx).Get(rename.ID)
				undoingDuringScan = err == nil && c.Undoing
			}

			res, err := svc.Undo(ctx, rename.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Done).To(BeTrue())
			Expect(res.ArtistID).To(Equal(metadata.ArtistID("Greatful Dead")))

			Expect(undoingDuringScan).To(BeTrue())
			Expect(refresher.scanned()).To(Equal([][]model.ScanTarget{deadTargets}))
			Expect(refresher.items).To(Equal([][]refresh.Item{{{Kind: model.KindArtistArtwork, ID: res.ArtistID}}}))
			_, err = ds.Correction(ctx).Get(rename.ID)
			Expect(err).To(MatchError(model.ErrNotFound))
		})

		It("keeps the Correction, marked undone, when the rescan fails, so undo can be retried", func() {
			refresher.err = errors.New("boom")
			_, err := svc.Undo(ctx, rename.ID)
			Expect(err).ToNot(HaveOccurred())
			c, err := ds.Correction(ctx).Get(rename.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(c.Undoing).To(BeTrue())

			refresher.err = nil
			_, err = svc.Undo(ctx, rename.ID)
			Expect(err).ToNot(HaveOccurred())
			_, err = ds.Correction(ctx).Get(rename.ID)
			Expect(err).To(MatchError(model.ErrNotFound))
		})

		It("deletes an orphaned Correction straight away", func() {
			_, err := db.Db().Exec("delete from media_file")
			Expect(err).ToNot(HaveOccurred())
			res, err := svc.Undo(ctx, rename.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Done).To(BeTrue())
			Expect(refresher.scanned()).To(BeEmpty())
			_, err = ds.Correction(ctx).Get(rename.ID)
			Expect(err).To(MatchError(model.ErrNotFound))
		})

		It("returns not found for an unknown Correction", func() {
			_, err := svc.Undo(ctx, "nope")
			Expect(err).To(MatchError(model.ErrNotFound))
		})
	})
})
