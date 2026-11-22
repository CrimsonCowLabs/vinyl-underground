package refresh_test

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/navidrome/navidrome/core/refresh"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/persistence"
	scannerpkg "github.com/navidrome/navidrome/scanner"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// journal records the scans and info refreshes in the order they happened.
type journal struct {
	mu      sync.Mutex
	entries []string
}

func (j *journal) add(entry string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.entries = append(j.entries, entry)
}

func (j *journal) all() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return slices.Clone(j.entries)
}

type fakeScanner struct {
	log      *journal
	busyFor  int // number of calls answered with errBusy before scanning
	errBusy  error
	fullScan []bool
	targets  [][]model.ScanTarget
}

func (f *fakeScanner) ScanFolders(_ context.Context, fullScan bool, targets []model.ScanTarget) ([]string, error) {
	if f.busyFor > 0 {
		f.busyFor--
		return nil, f.errBusy
	}
	f.fullScan = append(f.fullScan, fullScan)
	f.targets = append(f.targets, slices.Clone(targets))
	f.log.add("scan")
	return nil, nil
}

type fakeInfo struct{ log *journal }

func (f *fakeInfo) RefreshInfo(_ context.Context, kind model.Kind, id string) error {
	f.log.add("info " + kind.Prefix() + "/" + id)
	return nil
}

var _ = Describe("Refresher", func() {
	var ctx context.Context
	var ds model.DataStore
	var log *journal
	var scanner *fakeScanner
	var refresher refresh.Refresher
	var lib model.Library

	BeforeEach(func() {
		ctx = context.Background()
		ds = persistence.New(db.Db())
		for _, table := range []string{"album_artists", "album", "library_artist", "artist", "folder", "artwork_queue", "item_artwork"} {
			_, err := db.Db().Exec("delete from " + table)
			Expect(err).ToNot(HaveOccurred())
		}
		l, err := ds.Library(ctx).Get(1)
		Expect(err).ToNot(HaveOccurred())
		lib = *l
		log = &journal{}
		scanner = &fakeScanner{log: log, errBusy: scannerpkg.ErrAlreadyScanning}
		refresher = refresh.NewWithOptions(ds, scanner, &fakeInfo{log: log}, refresh.Options{
			ScanRetryDelay: time.Millisecond, ScanAttempts: 3,
		})
	})

	addFolder := func(folderPath string) model.Folder {
		f := model.NewFolder(lib, folderPath)
		Expect(ds.Folder(ctx).Put(f)).To(Succeed())
		return *f
	}

	addArtist := func(id, name string) model.Artist {
		ar := model.Artist{ID: id, Name: name}
		Expect(ds.Artist(ctx).Put(&ar)).To(Succeed())
		Expect(ds.Library(ctx).AddArtist(lib.ID, ar.ID)).To(Succeed())
		return ar
	}

	addAlbum := func(id string, artist model.Artist, folders ...model.Folder) {
		al := model.Album{ID: id, Name: "Album " + id, LibraryID: lib.ID, Participants: model.Participants{}}
		al.Participants.Add(model.RoleAlbumArtist, artist)
		for _, f := range folders {
			al.FolderIDs = append(al.FolderIDs, f.ID)
		}
		Expect(ds.Album(ctx).Put(&al)).To(Succeed())
	}

	queued := func() []string {
		items, err := ds.ArtworkQueue(ctx).DequeueBatch(1000)
		Expect(err).ToNot(HaveOccurred())
		var res []string
		for _, it := range items {
			res = append(res, fmt.Sprintf("%s/%s", it.ItemKind, it.ItemID))
		}
		return res
	}

	Describe("Refresh an album", func() {
		It("fully rescans only the album's folder, then refreshes its artwork and external info", func() {
			radiohead := addArtist("ar-1", "Radiohead")
			kidA := addFolder("Radiohead/Kid A")
			addFolder("Radiohead/Amnesiac")
			addAlbum("al-1", radiohead, kidA)

			Expect(refresher.Refresh(ctx, model.KindAlbumArtwork, "al-1")).To(Succeed())

			Expect(scanner.targets).To(Equal([][]model.ScanTarget{{{LibraryID: lib.ID, FolderPath: "Radiohead/Kid A"}}}))
			Expect(scanner.fullScan).To(Equal([]bool{true}))
			Expect(queued()).To(ConsistOf("al/al-1"))
			Eventually(log.all).Should(Equal([]string{"scan", "info al/al-1"}))
		})
	})

	Describe("Refresh an Artist", func() {
		It("fully rescans the folders of all its albums in one scan, then refreshes the Artist", func() {
			radiohead := addArtist("ar-1", "Radiohead")
			blur := addArtist("ar-2", "Blur")
			addAlbum("al-1", radiohead, addFolder("Radiohead/Kid A"))
			addAlbum("al-2", radiohead, addFolder("Radiohead/OK Computer/CD1"), addFolder("Radiohead/OK Computer/CD2"))
			addAlbum("al-3", blur, addFolder("Blur/Parklife"))

			Expect(refresher.Refresh(ctx, model.KindArtistArtwork, "ar-1")).To(Succeed())

			Expect(scanner.targets).To(HaveLen(1))
			Expect(scanner.targets[0]).To(ConsistOf(
				model.ScanTarget{LibraryID: lib.ID, FolderPath: "Radiohead/Kid A"},
				model.ScanTarget{LibraryID: lib.ID, FolderPath: "Radiohead/OK Computer/CD1"},
				model.ScanTarget{LibraryID: lib.ID, FolderPath: "Radiohead/OK Computer/CD2"},
			))
			Expect(scanner.fullScan).To(Equal([]bool{true}))
			Expect(queued()).To(ConsistOf("ar/ar-1"))
			Eventually(log.all).Should(Equal([]string{"scan", "info ar/ar-1"}))
		})

		It("scans nothing, rather than the whole library, when the Artist has no album folders", func() {
			addArtist("ar-1", "Radiohead")

			Expect(refresher.Refresh(ctx, model.KindArtistArtwork, "ar-1")).To(Succeed())

			Expect(scanner.targets).To(BeEmpty())
			Expect(queued()).To(ConsistOf("ar/ar-1"))
			Eventually(log.all).Should(Equal([]string{"info ar/ar-1"}))
		})

		It("never scans the library root, as that would rescan the whole library", func() {
			radiohead := addArtist("ar-1", "Radiohead")
			addAlbum("al-1", radiohead, addFolder("."), addFolder("Radiohead/Kid A"))

			Expect(refresher.Refresh(ctx, model.KindArtistArtwork, "ar-1")).To(Succeed())

			Expect(scanner.targets).To(Equal([][]model.ScanTarget{{{LibraryID: lib.ID, FolderPath: "Radiohead/Kid A"}}}))
		})

		It("returns ErrNotFound for an unknown Artist, without scanning", func() {
			err := refresher.Refresh(ctx, model.KindArtistArtwork, "nope")

			Expect(err).To(MatchError(model.ErrNotFound))
			Expect(scanner.targets).To(BeEmpty())
		})
	})

	Describe("ScanThenRefresh", func() {
		targets := []model.ScanTarget{{LibraryID: 1, FolderPath: "Radiohead/Kid A"}}

		BeforeEach(func() {
			addAlbum("al-1", addArtist("ar-1", "Radiohead"), addFolder("Radiohead/Kid A"))
		})

		It("scans the folders, then refreshes every item", func() {
			Expect(refresher.ScanThenRefresh(ctx, targets,
				refresh.Item{Kind: model.KindArtistArtwork, ID: "ar-1"},
				refresh.Item{Kind: model.KindAlbumArtwork, ID: "al-1"},
			)).To(Succeed())

			Expect(scanner.targets).To(Equal([][]model.ScanTarget{targets}))
			Expect(queued()).To(ConsistOf("ar/ar-1", "al/al-1"))
			Eventually(log.all).Should(ConsistOf("scan", "info ar/ar-1", "info al/al-1"))
			Expect(log.all()[0]).To(Equal("scan"))
		})

		It("refreshes what the folders now hold in place of an album or Artist the scan renamed", func() {
			Expect(refresher.ScanThenRefresh(ctx, targets,
				refresh.Item{Kind: model.KindArtistArtwork, ID: "ar-old"},
				refresh.Item{Kind: model.KindAlbumArtwork, ID: "al-old"},
			)).To(Succeed())

			Expect(queued()).To(ConsistOf("ar/ar-1", "al/al-1"))
		})

		It("waits for a scan already running to finish", func() {
			scanner.busyFor = 2

			Expect(refresher.ScanThenRefresh(ctx, targets, refresh.Item{Kind: model.KindAlbumArtwork, ID: "al-1"})).To(Succeed())

			Expect(scanner.targets).To(Equal([][]model.ScanTarget{targets}))
			Expect(queued()).To(ConsistOf("al/al-1"))
		})

		It("gives up, refreshing nothing, when the other scan outlasts its attempts", func() {
			scanner.busyFor = 3

			err := refresher.ScanThenRefresh(ctx, targets, refresh.Item{Kind: model.KindAlbumArtwork, ID: "al-1"})

			Expect(err).To(MatchError(scannerpkg.ErrAlreadyScanning))
			Expect(queued()).To(BeEmpty())
			Consistently(log.all).Should(BeEmpty())
		})
	})
})
