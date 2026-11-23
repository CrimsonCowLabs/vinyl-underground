package e2e

import (
	"bytes"
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
	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/core/corrections"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/metrics"
	"github.com/navidrome/navidrome/core/playlists"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/metadata"
	"github.com/navidrome/navidrome/scanner"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/server/events"
	"github.com/navidrome/navidrome/server/nativeapi"
	"github.com/navidrome/navidrome/server/subsonic/responses"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.senan.xyz/taglib"
)

// The Correction migration (#165), on the real database, real Files and the real scanner, with
// the overlay retired: every Correction kept before ADR-0011 is written into the Files as one
// edit job, after which the app shows, read straight from the Files, the titles, artists,
// numbers and cover the overlay showed; Tracks keep their IDs, the album and the merged Artist
// their stars; and undoing the job from the Edit history puts the Files and the Corrections back.
var _ = Describe("Migrating Corrections into the Files", func() {
	var (
		nas    model.Library
		nasDir string
		s      inboxScanner
		admin  model.User
		cover  []byte
	)
	const (
		live1  = "Greatful Dead/Live/01 One.mp3"
		live2  = "Greatful Dead/Live/02 Two.mp3"
		studio = "Grateful Dead/Studio/01 Three.mp3"
	)

	scan := func(full bool) {
		GinkgoHelper()
		Eventually(scanner.IsScanning).Should(BeFalse())
		var err error
		Eventually(func() bool {
			_, err = s.ScanFolders(ctx, full, []model.ScanTarget{{LibraryID: nas.ID}})
			return errors.Is(err, scanner.ErrAlreadyScanning)
		}).Should(BeFalse())
		Expect(err).ToNot(HaveOccurred())
	}
	track := func(rel string) model.MediaFile {
		GinkgoHelper()
		mfs, err := ds.MediaFile(ctx).GetAll(model.QueryOptions{Filters: squirrel.Eq{"media_file.library_id": nas.ID, "media_file.path": rel}})
		Expect(err).ToNot(HaveOccurred())
		Expect(mfs).To(HaveLen(1))
		return mfs[0]
	}
	tagsOf := func(rel string) map[string][]string {
		GinkgoHelper()
		tags, err := taglib.ReadTags(filepath.Join(nasDir, filepath.FromSlash(rel)))
		Expect(err).ToNot(HaveOccurred())
		return tags
	}
	writeFile := func(rel string, tags map[string][]string) {
		GinkgoHelper()
		p := filepath.Join(nasDir, filepath.FromSlash(rel))
		Expect(os.MkdirAll(filepath.Dir(p), 0o755)).To(Succeed())
		data, err := os.ReadFile(filepath.Join("tests", "fixtures", "test.mp3"))
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(p, data, 0o644)).To(Succeed())
		Expect(taglib.WriteTags(p, tags, taglib.Clear)).To(Succeed())
	}
	call := func(method, url string, body any) *httptest.ResponseRecorder {
		GinkgoHelper()
		editor := tagedit.New(ds, s)
		router := nativeapi.New(ds, core.NewShare(ds), playlists.NewPlaylists(ds, artwork.NewUploader(ds)), nil, tests.NewMockLibraryService(), tests.NewMockUserService(),
			nil, nil, nil, nil, nil, nil, nil, editor)
		queue := editjob.New(ds, nil, editjob.Kinds(ds, editor))
		for kind, k := range corrections.JobKinds(ds) {
			queue.Handle(kind, k)
		}
		router.SetEditQueue(queue)
		qctx, stop := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() { queue.Run(qctx); close(done) }()
		defer func() { stop(); <-done }()
		var buf bytes.Buffer
		if body != nil {
			Expect(json.NewEncoder(&buf).Encode(body)).To(Succeed())
		}
		req := httptest.NewRequest(method, url, &buf)
		token, err := auth.CreateToken(&admin)
		Expect(err).ToNot(HaveOccurred())
		req.Header.Set(consts.UIAuthorizationHeader, "Bearer "+token)
		w := httptest.NewRecorder()
		server.JWTVerifier(router).ServeHTTP(w, req)
		return w
	}
	artistStarred := func(name string) bool {
		GinkgoHelper()
		var n int
		Expect(db.Db().QueryRow(`select count(*) from annotation where user_id = ? and item_type = 'artist'
			and item_id = ? and starred`, admin.ID, metadata.ArtistID(name)).Scan(&n)).To(Succeed())
		return n > 0
	}
	addCorrection := func(cr model.Correction) model.Correction {
		GinkgoHelper()
		Expect(ds.Correction(ctx).Add(&cr)).To(Succeed())
		return cr
	}

	BeforeEach(func() {
		setupTestDB()
		DeferCleanup(func(dir conf.Dir) { conf.Server.DataFolder = dir }, conf.Server.DataFolder)
		conf.Server.DataFolder = conf.NewDir(GinkgoT().TempDir())
		nasDir = GinkgoT().TempDir()
		dead := map[string][]string{"ARTIST": {"Greatful Dead"}, "ALBUMARTIST": {"Greatful Dead"}, "ALBUM": {"Live"}, "DATE": {"1977-05-08"}}
		with := func(kv ...string) map[string][]string {
			res := map[string][]string{}
			for k, v := range dead {
				res[k] = v
			}
			for i := 0; i < len(kv); i += 2 {
				res[kv[i]] = []string{kv[i+1]}
			}
			return res
		}
		writeFile(live1, with("TITLE", "One", "TRACKNUMBER", "1"))
		writeFile(live2, with("TITLE", "Two", "TRACKNUMBER", "3"))
		writeFile(studio, map[string][]string{"ARTIST": {"Grateful Dead"}, "ALBUMARTIST": {"Grateful Dead"}, "ALBUM": {"Studio"},
			"DATE": {"1970"}, "TITLE": {"Three"}, "TRACKNUMBER": {"1"}})
		var err error
		cover, err = os.ReadFile(filepath.Join("tests", "fixtures", "artist", "an-album", "cover.jpg"))
		Expect(err).ToNot(HaveOccurred())
		nas = model.Library{Name: "NAS", Path: nasDir}
		Expect(ds.Library(ctx).Put(&nas)).To(Succeed())
		Expect(ds.User(ctx).SetUserLibraries(adminUser.ID, []int{lib.ID, nas.ID})).To(Succeed())
		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: nas.ID, Name: nas.Name, Writable: true}}})
		DeferCleanup(core.SetLibraryEditing, core.LibraryEditing{})

		s = inboxScanner{scanner.New(ctx, ds, events.NoopBroker(),
			playlists.NewPlaylists(ds, artwork.NewUploader(ds)), metrics.NewNoopInstance())}
		scan(false)
		loaded, err := ds.User(ctx).FindByUsername(adminUser.UserName)
		Expect(err).ToNot(HaveOccurred())
		admin = *loaded
	})

	It("writes every kind of Correction into the Files as one job, after which the app shows the same values, and undoes it", func() {
		one, two, three := track(live1), track(live2), track(studio)
		liveAlbum, err := ds.Album(ctx).Get(one.AlbumID)
		Expect(err).ToNot(HaveOccurred())
		folder := model.AlbumFolderKey(*liveAlbum)

		// The Corrections the overlay applied: "Greatful Dead" merged into "Grateful Dead" (which
		// took its star), the album retitled and dated, Two numbered 2, and an uploaded cover.
		merge := addCorrection(model.Correction{Kind: model.CorrectionArtistMerge, MatchKey: model.ArtistNameKey("Greatful Dead"),
			From: "Greatful Dead", To: "Grateful Dead"})
		addCorrection(model.Correction{Kind: model.CorrectionAlbum, MatchKey: folder, From: "Greatful Dead – Live (1977)",
			To: "Greatful Dead – Live! (1978)", AlbumName: "Live!", Year: 1978})
		addCorrection(model.Correction{Kind: model.CorrectionTrackNumbers, MatchKey: folder, From: "Live", To: "2 Two",
			TrackNumbers: model.TrackNumberFixes{{LibraryID: nas.ID, Path: live2, Title: "Two", FromTrack: 3, Track: 2}}})
		stored, err := artwork.StoreChosenCover(cover)
		Expect(err).ToNot(HaveOccurred())
		addCorrection(model.Correction{Kind: model.CorrectionAlbumCover, MatchKey: folder, From: "Live", To: "Upload",
			ImageHash: stored.Hash, ImageMime: stored.Mime})
		Expect(doReq("star", "albumId", one.AlbumID).Status).To(Equal(responses.StatusOK))
		Expect(doReq("star", "id", one.ID).Status).To(Equal(responses.StatusOK))
		at := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
		_, err = db.Db().Exec(`insert into annotation (user_id, item_id, item_type, starred, starred_at) values (?, ?, 'artist', true, ?)`,
			admin.ID, metadata.ArtistID("Grateful Dead"), at)
		Expect(err).ToNot(HaveOccurred())
		_, err = db.Db().Exec(`insert into correction_annotation (correction_id, user_id, starred, starred_at, star_taken)
			values (?, ?, true, ?, true)`, merge.ID, admin.ID, at)
		Expect(err).ToNot(HaveOccurred())

		// The migration page lists all four, with what writing each changes.
		w := call(http.MethodGet, "/correction/migration", nil)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var plan corrections.Migration
		Expect(json.Unmarshal(w.Body.Bytes(), &plan)).To(Succeed())
		Expect(plan.Items).To(HaveLen(4))
		Expect(plan.Files).To(Equal(3), "the album's two Files and its cover")

		// Approved: one job.
		w = call(http.MethodPost, "/correction/migration", nil)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var res struct {
			JobID string         `json:"jobId"`
			Done  bool           `json:"done"`
			Job   *model.EditJob `json:"job"`
		}
		Expect(json.Unmarshal(w.Body.Bytes(), &res)).To(Succeed())
		Expect(res.Done).To(BeTrue())
		Expect(res.Job.Kind).To(Equal(corrections.JobMigrate))
		Expect(res.Job.Skipped).To(BeZero())
		Expect(ds.Correction(ctx).GetAll()).To(BeEmpty())

		// What the overlay showed, read straight from the Files, now and after a full rescan.
		check := func() {
			GinkgoHelper()
			for _, rel := range []string{live1, live2} {
				mf := track(rel)
				Expect(mf.Artist).To(Equal("Grateful Dead"))
				Expect(mf.AlbumArtist).To(Equal("Grateful Dead"))
				Expect(mf.Album).To(Equal("Live!"))
				Expect(mf.Year).To(Equal(1978))
			}
			Expect(track(live1).TrackNumber).To(Equal(1))
			Expect(track(live2).TrackNumber).To(Equal(2))
			Expect(track(live1).ID).To(Equal(one.ID))
			Expect(track(live2).ID).To(Equal(two.ID))
			Expect(track(studio).ID).To(Equal(three.ID))
			Expect(track(live1).Starred).To(BeTrue())
			al, err := ds.Album(ctx).Get(track(live1).AlbumID)
			Expect(err).ToNot(HaveOccurred())
			Expect(al.Name).To(Equal("Live!"))
			Expect(al.Starred).To(BeTrue(), "the album's star follows it to its new ID")
			Expect(artistStarred("Grateful Dead")).To(BeTrue())
			src, err := artwork.NewTracingResolver(ds, nil, tests.NewMockFFmpeg(""), &artwork.ChainTrace{}, false).
				Resolve(ctx, model.KindAlbumArtwork, al.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(src).To(Equal("folder"), "the uploaded cover is the folder's cover.jpg")
		}
		check()
		written, err := os.ReadFile(filepath.Join(nasDir, "Greatful Dead", "Live", "cover.jpg"))
		Expect(err).ToNot(HaveOccurred())
		Expect(written).To(Equal(cover))
		Expect(tagsOf(live2)).To(HaveKeyWithValue("TRACKNUMBER", []string{"2"}))
		Expect(tagsOf(live1)).To(HaveKeyWithValue("DATE", []string{"1978"}))
		scan(true)
		check()

		// Undone from the Edit history as a whole: the Files and the Corrections are as they were,
		// and the old name gets back the star the merge gave the other Artist.
		w = call(http.MethodPost, "/editJob/"+res.JobID+"/undo", nil)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var undone model.EditJob
		Expect(json.Unmarshal(w.Body.Bytes(), &undone)).To(Succeed())
		Expect(undone.Skipped).To(BeZero())
		Expect(tagsOf(live1)).To(HaveKeyWithValue("ARTIST", []string{"Greatful Dead"}))
		Expect(tagsOf(live1)).To(HaveKeyWithValue("ALBUM", []string{"Live"}))
		Expect(tagsOf(live2)).To(HaveKeyWithValue("TRACKNUMBER", []string{"3"}))
		Expect(filepath.Join(nasDir, "Greatful Dead", "Live", "cover.jpg")).ToNot(BeAnExistingFile())
		Expect(ds.Correction(ctx).GetAll()).To(HaveLen(4))
		Expect(track(live1).ID).To(Equal(one.ID))
		Expect(track(live1).Artist).To(Equal("Greatful Dead"))
		Expect(artistStarred("Greatful Dead")).To(BeTrue())
		Expect(artistStarred("Grateful Dead")).To(BeFalse())
	})
})
