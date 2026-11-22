package scanner_test

import (
	"context"
	"path/filepath"
	"testing/fstest"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/metrics"
	"github.com/navidrome/navidrome/core/playlists"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/metadata"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/persistence"
	"github.com/navidrome/navidrome/scanner"
	"github.com/navidrome/navidrome/server/events"
	"github.com/navidrome/navidrome/tests"
	"github.com/navidrome/navidrome/utils/slice"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Scanning with an Artist merge", Ordered, func() {
	var ctx, otherCtx context.Context
	var ds model.DataStore
	var s model.Scanner
	var findByPath func(string) (*model.MediaFile, error)
	var merge *model.Correction

	const user, other = "123", "456"
	dupeID := metadata.ArtistID("The Grateful Dead")
	targetID := metadata.ArtistID("Grateful Dead")
	targets := []model.ScanTarget{{LibraryID: 1, FolderPath: "The Grateful Dead"}, {LibraryID: 1, FolderPath: "Grateful Dead"}}

	BeforeAll(func() {
		tests.SkipOnWindows("SQLite file lock blocks TempDir cleanup (#TBD-path-sep-scanner)")
		ctx = request.WithUser(GinkgoT().Context(), model.User{ID: user, UserName: "admin", IsAdmin: true})
		otherCtx = request.WithUser(GinkgoT().Context(), model.User{ID: other, UserName: "other"})
		conf.Server.DbPath = filepath.Join(GinkgoT().TempDir(), "test-corrections-merge.db?_journal_mode=WAL")
		db.Db().SetMaxOpenConns(1)
	})

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.MusicFolder = "fake:///music"
		conf.Server.DevExternalScanner = false
		db.Init(ctx)
		DeferCleanup(func() { Expect(tests.ClearDB()).To(Succeed()) })

		ds = persistence.New(db.Db())
		Expect(ds.User(ctx).Put(&model.User{ID: user, UserName: "admin", IsAdmin: true, NewPassword: "pw"})).To(Succeed())
		Expect(ds.User(ctx).Put(&model.User{ID: other, UserName: "other", NewPassword: "pw"})).To(Succeed())
		s = scanner.New(ctx, ds, events.NoopBroker(),
			playlists.NewPlaylists(ds, artwork.NewUploader(ds)), metrics.NewNoopInstance())
		Expect(ds.Library(ctx).Put(&model.Library{ID: 1, Name: "Fake Library", Path: "fake:///music"})).To(Succeed())

		dupe := template(_t{"albumartist": "The Grateful Dead", "artist": "The Grateful Dead", "album": "Live", "year": 1971})
		dead := template(_t{"albumartist": "Grateful Dead", "artist": "Grateful Dead", "album": "Europe 72", "year": 1972})
		createFS(fstest.MapFS{
			"The Grateful Dead/Live/01 - Ripple.mp3":      dupe(track(1, "Ripple")),
			"The Grateful Dead/Live/02 - Casey Jones.mp3": dupe(track(2, "Casey Jones")),
			// A second folder: the merge applies in each of them, in the same scan
			"The Grateful Dead/Studio/01 - Truckin.mp3":   dupe(track(1, "Truckin"), _t{"album": "Studio"}),
			"Grateful Dead/Europe 72/01 - Jack Straw.mp3": dead(track(1, "Jack Straw")),
		})
		_, err := s.ScanAll(ctx, true)
		Expect(err).ToNot(HaveOccurred())
		findByPath = createFindByPath(ctx, ds)
		merge = &model.Correction{Kind: model.CorrectionArtistMerge, MatchKey: model.ArtistNameKey("The Grateful Dead"),
			From: "The Grateful Dead", To: "Grateful Dead"}
	})

	scanFolders := func() {
		GinkgoHelper()
		_, err := s.ScanFolders(ctx, true, targets)
		Expect(err).ToNot(HaveOccurred())
	}
	artist := func(ctx context.Context, id string) *model.Artist {
		GinkgoHelper()
		ar, err := ds.Artist(ctx).Get(id)
		Expect(err).ToNot(HaveOccurred())
		return ar
	}
	albumsOf := func(artistID string) []string {
		GinkgoHelper()
		als, err := ds.Album(ctx).GetAll(model.QueryOptions{Filters: squirrel.Eq{"album_artist_id": artistID}})
		Expect(err).ToNot(HaveOccurred())
		return slice.Map(als, func(al model.Album) string { return al.Name })
	}
	trackIDs := func() map[string]string {
		GinkgoHelper()
		mfs, err := ds.MediaFile(ctx).GetAll()
		Expect(err).ToNot(HaveOccurred())
		return slice.ToMap(mfs, func(mf model.MediaFile) (string, string) { return mf.Path, mf.ID })
	}
	undo := func() {
		GinkgoHelper()
		Expect(ds.Correction(ctx).SetUndoing(merge.ID)).To(Succeed())
		scanFolders()
		Expect(ds.Correction(ctx).Delete(merge.ID)).To(Succeed())
	}

	It("gives one Artist all the albums and Tracks, keeping the Track IDs, and splits them on undo", func() {
		before := trackIDs()
		Expect(ds.Correction(ctx).Add(merge)).To(Succeed())
		scanFolders()

		Expect(trackIDs()).To(Equal(before))
		Expect(albumsOf(targetID)).To(ConsistOf("Live", "Studio", "Europe 72"))
		mf, err := findByPath("The Grateful Dead/Live/01 - Ripple.mp3")
		Expect(err).ToNot(HaveOccurred())
		Expect(mf.Artist).To(Equal("Grateful Dead"))
		Expect(mf.ArtistID).To(Equal(targetID))
		Expect(artist(ctx, targetID).SongCount).To(Equal(4))
		Expect(ds.MediaFile(ctx).CountAll(model.QueryOptions{Filters: squirrel.Eq{"artist_id": dupeID}})).To(BeZero())

		By("undoing")
		undo()
		Expect(albumsOf(targetID)).To(ConsistOf("Europe 72"))
		Expect(albumsOf(dupeID)).To(ConsistOf("Live", "Studio"))
		Expect(trackIDs()).To(Equal(before))
	})

	It("adds the play counts together, keeps the stars and ratings, and moves them back on undo", func() {
		// The admin played, starred and rated both; the other user only the duplicate
		Expect(ds.Artist(ctx).IncPlayCount(dupeID, time.Now())).To(Succeed())
		Expect(ds.Artist(ctx).IncPlayCount(dupeID, time.Now())).To(Succeed())
		Expect(ds.Artist(ctx).SetStar(true, dupeID)).To(Succeed())
		Expect(ds.Artist(ctx).SetRating(2, dupeID)).To(Succeed())
		for range 3 {
			Expect(ds.Artist(ctx).IncPlayCount(targetID, time.Now())).To(Succeed())
		}
		Expect(ds.Artist(ctx).SetRating(5, targetID)).To(Succeed())
		Expect(ds.Artist(otherCtx).IncPlayCount(dupeID, time.Now())).To(Succeed())
		Expect(ds.Artist(otherCtx).SetRating(4, dupeID)).To(Succeed())

		By("merging")
		Expect(ds.Correction(ctx).Add(merge)).To(Succeed())
		scanFolders()

		ar := artist(ctx, targetID)
		Expect(ar.PlayCount).To(Equal(int64(5)))
		Expect(ar.Starred).To(BeTrue()) // only the duplicate was starred: the merged Artist takes its star
		Expect(ar.Rating).To(Equal(5))  // both were rated: the target keeps its own rating
		ar = artist(otherCtx, targetID)
		Expect(ar.PlayCount).To(Equal(int64(1)))
		Expect(ar.Rating).To(Equal(4))
		Expect(ar.AverageRating).To(Equal(4.5))

		By("undoing")
		undo()

		ar = artist(ctx, targetID)
		Expect(ar.PlayCount).To(Equal(int64(3)))
		Expect(ar.Starred).To(BeFalse())
		Expect(ar.Rating).To(Equal(5))
		Expect(ar.AverageRating).To(Equal(5.0))
		ar = artist(ctx, dupeID)
		Expect(ar.PlayCount).To(Equal(int64(2)))
		Expect(ar.Starred).To(BeTrue())
		Expect(ar.Rating).To(Equal(2))
		ar = artist(otherCtx, targetID)
		Expect(ar.PlayCount).To(BeZero())
		Expect(ar.Rating).To(BeZero())
		ar = artist(otherCtx, dupeID)
		Expect(ar.PlayCount).To(Equal(int64(1)))
		Expect(ar.Rating).To(Equal(4))
	})

	It("on undo, keeps on the merged Artist what it was given after the merge", func() {
		Expect(ds.Artist(ctx).SetStar(true, dupeID)).To(Succeed())
		Expect(ds.Correction(ctx).Add(merge)).To(Succeed())
		scanFolders()
		// Starred again after the merge: that star is the merged Artist's own now
		time.Sleep(10 * time.Millisecond)
		Expect(ds.Artist(ctx).SetStar(true, targetID)).To(Succeed())
		Expect(ds.Artist(ctx).IncPlayCount(targetID, time.Now())).To(Succeed())

		undo()

		ar := artist(ctx, targetID)
		Expect(ar.Starred).To(BeTrue())
		Expect(ar.PlayCount).To(Equal(int64(1)))
		Expect(artist(ctx, dupeID).Starred).To(BeTrue())
	})
})
