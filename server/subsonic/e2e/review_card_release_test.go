package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/Masterminds/squirrel"
	_ "github.com/navidrome/navidrome/adapters/gotaglib" // reads the real Files below
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/corrections"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/metrics"
	"github.com/navidrome/navidrome/core/musicbrainz"
	"github.com/navidrome/navidrome/core/playlists"
	"github.com/navidrome/navidrome/core/rip"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/scanner"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/server/events"
	"github.com/navidrome/navidrome/server/nativeapi"
	"github.com/navidrome/navidrome/server/ripapi"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.senan.xyz/taglib"
)

// A review card applies a MusicBrainz release to its album before Promote (#219): the release's
// IDs and its cover go into the Inbox Files as one edit job, undoable from the Edit history, and
// once promoted the album arrives in the Library with them. These specs run the real database,
// real Files and the real scanner, with MusicBrainz and the Cover Art Archive answered by the test.
var _ = Describe("A MusicBrainz release applied from a review card", func() {
	const (
		relID   = "8a7f5c4e-0d5b-4f1e-9d3c-3b2a1f0e9d8c"
		groupID = "1b2c3d4e-5f60-4718-9a0b-c1d2e3f40516"
		rec1    = "11111111-1111-4111-8111-111111111111"
		rec2    = "22222222-2222-4222-8222-222222222222"
		one     = "Live/01 One.mp3"
		two     = "Live/02 Two.mp3"
	)
	var (
		inboxDir, nasDir string
		inboxID          int
		nas              model.Library
		admin            model.User
		s                inboxScanner
		ripper           *rip.Ripper
		job              *model.RipJob
		mbz              *httptest.Server
		cover            []byte
		nativeAPI        http.Handler
	)

	scanAll := func() {
		GinkgoHelper()
		Eventually(scanner.IsScanning).Should(BeFalse())
		var err error
		Eventually(func() bool {
			_, err = s.ScanFolders(ctx, false, []model.ScanTarget{{LibraryID: inboxID}, {LibraryID: nas.ID}})
			return errors.Is(err, scanner.ErrAlreadyScanning)
		}).Should(BeFalse())
		Expect(err).ToNot(HaveOccurred())
	}
	tagsOf := func(dir, rel string) map[string][]string {
		GinkgoHelper()
		tags, err := taglib.ReadTags(filepath.Join(dir, filepath.FromSlash(rel)))
		Expect(err).ToNot(HaveOccurred())
		return tags
	}

	BeforeEach(func() {
		setupTestDB()
		conf.Server.Ripping.Enabled = true
		conf.Server.EnableExternalServices = true
		conf.Server.DataFolder = conf.NewDir(GinkgoT().TempDir())

		// MusicBrainz, as Client.Release asks it: "Grateful Dead – Live" (1977), two tracks.
		credit := musicbrainz.ArtistCredits{{Name: "Grateful Dead"}}
		credit[0].Artist.ID, credit[0].Artist.Name = "6faa7ca7-0d99-4a5e-bfa6-1fd5037520c6", "Grateful Dead"
		rel := musicbrainz.Release{ID: relID, Title: "Live", Date: "1977-05-08", Country: "US", ArtistCredit: credit,
			ReleaseGroup: musicbrainz.ReleaseGroup{ID: groupID, Title: "Live"},
			Media: []musicbrainz.Media{{Format: "CD", Position: 1, TrackCount: 2, Tracks: []musicbrainz.Track{
				{ID: "aaaaaaaa-1111-4111-8111-111111111111", Position: 1, Title: "One", Recording: musicbrainz.Recording{ID: rec1}},
				{ID: "aaaaaaaa-2222-4222-8222-222222222222", Position: 2, Title: "Two", Recording: musicbrainz.Recording{ID: rec2}},
			}}}}
		body, err := json.Marshal(rel)
		Expect(err).ToNot(HaveOccurred())
		mbz = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/release/"+relID {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(body)
		}))
		DeferCleanup(mbz.Close)
		cover, err = os.ReadFile(filepath.Join("tests", "fixtures", "artist", "an-album", "cover.jpg"))
		Expect(err).ToNot(HaveOccurred())

		// The music share, writable, and the Inbox with a card of two Tracks.
		nasDir = GinkgoT().TempDir()
		nas = model.Library{Name: "NAS", Path: nasDir}
		Expect(ds.Library(ctx).Put(&nas)).To(Succeed())
		inboxDir = GinkgoT().TempDir()
		conf.Server.Inbox.Path = inboxDir
		data, err := os.ReadFile(filepath.Join("tests", "fixtures", "test.mp3"))
		Expect(err).ToNot(HaveOccurred())
		for rel, tags := range map[string][2]string{one: {"One", "1"}, two: {"Two", "2"}} {
			p := filepath.Join(inboxDir, filepath.FromSlash(rel))
			Expect(os.MkdirAll(filepath.Dir(p), 0o755)).To(Succeed())
			Expect(os.WriteFile(p, data, 0o644)).To(Succeed())
			Expect(taglib.WriteTags(p, map[string][]string{"ARTIST": {"Grateful Dead"}, "ALBUMARTIST": {"Grateful Dead"},
				"ALBUM": {"Live"}, "DATE": {"1977"}, "TITLE": {tags[0]}, "TRACKNUMBER": {tags[1]}}, taglib.Clear)).To(Succeed())
		}
		Expect(ensureInbox(inboxDir)).To(BeTrue())
		inboxLib, err := core.FindInboxLibrary(ctx, ds)
		Expect(err).ToNot(HaveOccurred())
		inboxID = inboxLib.ID

		s = inboxScanner{scanner.New(ctx, ds, events.NoopBroker(),
			playlists.NewPlaylists(ds, artwork.NewUploader(ds)), metrics.NewNoopInstance())}
		scanAll()
		job = &model.RipJob{UserID: adminUser.ID, Status: model.RipReady, Folder: "Live"}
		Expect(ds.RipJob(ctx).Put(job)).To(Succeed())
		for i, rel := range []string{one, two} {
			Expect(ds.RipItem(ctx).Put(&model.RipItem{JobID: job.ID, Position: i + 1, Status: model.RipReady, Path: rel})).To(Succeed())
		}
		loaded, err := ds.User(ctx).FindByUsername(adminUser.UserName)
		Expect(err).ToNot(HaveOccurred())
		admin = *loaded

		// As at startup (cmd/root.go): one edit queue for Promote and the release writes, and the
		// native API guarded by the Ripper.
		ripper = rip.New(ds, nil, s, rip.Options{ScanRetryDelay: 10 * time.Millisecond, DropSettle: 10 * time.Millisecond})
		var editor *tagedit.Editor
		var queue *editjob.Queue
		queue, editor = startPromote(ripper, s, nas, func(q *editjob.Queue, _ *tagedit.Editor) {
			for kind, k := range corrections.JobKinds(ds) {
				q.Handle(kind, k)
			}
			q.Handle(corrections.JobRelease, corrections.ReleaseJobKind(ds, q))
		})
		api := nativeapi.New(ds, core.NewShare(ds), playlists.NewPlaylists(ds, artwork.NewUploader(ds)), nil, tests.NewMockLibraryService(), tests.NewMockUserService(),
			nil, nil, nil, nil, nil, nil, nil, editor)
		api.SetMusicBrainz(musicbrainz.New(musicbrainz.Options{BaseURL: mbz.URL + "/",
			Limiter: musicbrainz.NewLimiter(time.Millisecond, nil)}))
		api.SetCorrections(corrections.NewWithOptions(ds, corrections.Options{
			PickedCover: func(context.Context, string, string) (artwork.StoredCover, error) {
				return artwork.StoreChosenCover(cover)
			}}))
		api.SetEditQueue(queue)
		api.SetPromoteGuard(ripper)
		nativeAPI = server.JWTVerifier(api)
	})

	ripAPI := func() http.Handler { return server.JWTVerifier(ripapi.New(ds, ripper)) }
	// cardAlbum is the album of the card's Tracks, from its review, as the card takes it.
	cardAlbum := func() string {
		GinkgoHelper()
		w := adminCall(ripAPI(), admin, http.MethodGet, "/"+job.ID+"/tags", nil)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var rev rip.Review
		Expect(json.Unmarshal(w.Body.Bytes(), &rev)).To(Succeed())
		Expect(rev.Tracks).To(HaveLen(2))
		Expect(rev.Tracks[0].AlbumID).ToNot(BeEmpty())
		Expect(rev.Tracks[1].AlbumID).To(Equal(rev.Tracks[0].AlbumID), "one album")
		return rev.Tracks[0].AlbumID
	}
	type applied struct {
		JobID     string                `json:"jobId"`
		Done      bool                  `json:"done"`
		AlbumID   string                `json:"albumId"`
		PickCover corrections.PickCover `json:"pickCover"`
		Job       *model.EditJob        `json:"job"`
	}
	// apply sends what the card does: the paired release, with cover and genres ticked, and none
	// of the card's values (its Files hold them).
	apply := func(albumID string) applied {
		GinkgoHelper()
		w := adminCall(nativeAPI, admin, http.MethodPost, "/correction/album", map[string]any{
			"albumId": albumID, "mbzAlbumId": relID, "cover": true, "genres": true})
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var res applied
		Expect(json.Unmarshal(w.Body.Bytes(), &res)).To(Succeed())
		Expect(res.Done).To(BeTrue())
		Expect(res.Job.Skipped).To(BeZero())
		return res
	}

	It("writes the release's IDs and cover into the Inbox Files as one job, which the Edit history undoes", func() {
		before := cardAlbum()
		res := apply(before)
		Expect(res.PickCover).To(Equal(corrections.PickCoverFound))

		for rel, rec := range map[string]string{one: rec1, two: rec2} {
			tags := tagsOf(inboxDir, rel)
			Expect(tags).To(HaveKeyWithValue("MUSICBRAINZ_ALBUMID", []string{relID}))
			Expect(tags).To(HaveKeyWithValue("MUSICBRAINZ_TRACKID", []string{rec}))
			Expect(tags).To(HaveKeyWithValue("ALBUM", []string{"Live"}), "the album's own values are kept")
			Expect(tags).To(HaveKeyWithValue("DATE", []string{"1977"}))
		}
		got, err := os.ReadFile(filepath.Join(inboxDir, "Live", "cover.jpg"))
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(Equal(cover))
		entries, err := ds.EditJournal(ctx).ForJob(res.JobID)
		Expect(err).ToNot(HaveOccurred())
		Expect(entries).To(HaveLen(3), "the two Files and the cover, in one job")

		// The card reloads on the album's new ID, which follows its musicbrainz_albumid.
		after := cardAlbum()
		Expect(after).ToNot(Equal(before))
		Expect(after).To(Equal(res.AlbumID))

		w := adminCall(nativeAPI, admin, http.MethodPost, "/editJob/"+res.JobID+"/undo", nil)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		Expect(tagsOf(inboxDir, one)).ToNot(HaveKey("MUSICBRAINZ_ALBUMID"))
		Expect(tagsOf(inboxDir, two)).ToNot(HaveKey("MUSICBRAINZ_ALBUMID"))
		Expect(filepath.Join(inboxDir, "Live", "cover.jpg")).ToNot(BeAnExistingFile())
	})

	It("is refused while editing is off", func() {
		albumID := cardAlbum()
		core.SetLibraryEditing(core.LibraryEditing{InboxID: inboxID})
		w := adminCall(nativeAPI, admin, http.MethodPost, "/correction/album", map[string]any{
			"albumId": albumID, "mbzAlbumId": relID, "cover": true})
		Expect(w.Code).To(Equal(http.StatusForbidden), w.Body.String())
		Expect(tagsOf(inboxDir, one)).ToNot(HaveKey("MUSICBRAINZ_ALBUMID"))
		Expect(filepath.Join(inboxDir, "Live", "cover.jpg")).ToNot(BeAnExistingFile())
	})

	It("brings the album into the Library with its cover and MusicBrainz IDs once promoted", func() {
		apply(cardAlbum())
		_, promoted := promoteJob(ripAPI(), admin, job.ID)
		Expect(promoted.Skipped).To(BeZero())
		scanAll()

		mfs, err := ds.MediaFile(ctx).GetAll(model.QueryOptions{Filters: squirrel.Eq{
			"media_file.library_id": nas.ID, "media_file.missing": false}})
		Expect(err).ToNot(HaveOccurred())
		Expect(mfs).To(HaveLen(2))
		for _, mf := range mfs {
			Expect(mf.MbzAlbumID).To(Equal(relID))
		}
		dir := filepath.Join(nasDir, filepath.FromSlash(filepath.ToSlash(filepath.Dir(mfs[0].Path))))
		got, err := os.ReadFile(filepath.Join(dir, "cover.jpg"))
		Expect(err).ToNot(HaveOccurred(), "the cover came along")
		Expect(got).To(Equal(cover))
		al, err := ds.Album(ctx).Get(mfs[0].AlbumID)
		Expect(err).ToNot(HaveOccurred())
		Expect(al.MbzAlbumID).To(Equal(relID))
		Expect(al.LibraryID).To(Equal(nas.ID))
	})
})
