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

	"github.com/Masterminds/squirrel"
	_ "github.com/navidrome/navidrome/adapters/gotaglib" // reads the real Files below
	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/core/corrections"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/metrics"
	"github.com/navidrome/navidrome/core/playlists"
	"github.com/navidrome/navidrome/core/tagedit"
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

// The album Edit dialog, Track order and artist rename and merge write to the Files (#163), each
// as one edit job, on the real database, real Files and the real scanner: Tracks keep their IDs,
// albums and Artists keep their stars and play counts under their new IDs, no Correction is
// saved, an existing Correction overriding what was written is removed, and undoing the job puts
// all of it back.
var _ = Describe("Corrections written to the Files", func() {
	var (
		nas    model.Library
		nasDir string
		s      inboxScanner
		admin  model.User
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

	BeforeEach(func() {
		setupTestDB()
		nasDir = GinkgoT().TempDir()
		dead := map[string][]string{"ARTIST": {"Greatful Dead"}, "ALBUMARTIST": {"Greatful Dead"}, "ALBUM": {"Live"}, "DATE": {"1977"}}
		with := func(m map[string][]string, kv ...string) map[string][]string {
			res := map[string][]string{}
			for k, v := range m {
				res[k] = v
			}
			for i := 0; i < len(kv); i += 2 {
				res[kv[i]] = []string{kv[i+1]}
			}
			return res
		}
		writeFile(live1, with(dead, "TITLE", "One", "TRACKNUMBER", "1"))
		writeFile(live2, with(dead, "TITLE", "Two", "TRACKNUMBER", "2"))
		writeFile(studio, map[string][]string{"ARTIST": {"Grateful Dead"}, "ALBUMARTIST": {"Grateful Dead"}, "ALBUM": {"Studio"},
			"DATE": {"1970"}, "TITLE": {"Three"}, "TRACKNUMBER": {"1"}})
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

	// call runs one request against the native API, with its edit job queue running, as at
	// startup (cmd/root.go).
	call := func(method, url string, body any) *httptest.ResponseRecorder {
		GinkgoHelper()
		editor := tagedit.New(ds, s)
		router := nativeapi.New(ds, core.NewShare(ds), playlists.NewPlaylists(ds, artwork.NewUploader(ds)), nil, tests.NewMockLibraryService(), tests.NewMockUserService(),
			nil, nil, nil, nil, nil, nil, nil, editor)
		queue := editjob.New(ds, nil, editjob.Kinds(ds, editor))
		for kind, k := range corrections.JobKinds(ds, s) {
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
	type saved struct {
		JobID    string         `json:"jobId"`
		Done     bool           `json:"done"`
		AlbumID  string         `json:"albumId"`
		ArtistID string         `json:"artistId"`
		Job      *model.EditJob `json:"job"`
	}
	save := func(url string, body any) saved {
		GinkgoHelper()
		w := call(http.MethodPost, url, body)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var res saved
		Expect(json.Unmarshal(w.Body.Bytes(), &res)).To(Succeed())
		Expect(res.Done).To(BeTrue())
		Expect(res.Job.Skipped).To(BeZero())
		return res
	}
	undoJob := func(jobID string) {
		GinkgoHelper()
		w := call(http.MethodPost, "/editJob/"+jobID+"/undo", nil)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var job model.EditJob
		Expect(json.Unmarshal(w.Body.Bytes(), &job)).To(Succeed())
		Expect(job.Skipped).To(BeZero())
	}
	jobs := func() model.EditJobs {
		GinkgoHelper()
		all, err := ds.EditJob(ctx).GetAll(0)
		Expect(err).ToNot(HaveOccurred())
		return all
	}

	It("writes the album Edit dialog into every File of the album, keeps its Tracks and its star, and replaces the Correction it makes stale", func() {
		one, two := track(live1), track(live2)
		Expect(doReq("star", "albumId", one.AlbumID).Status).To(Equal(responses.StatusOK))
		// An earlier Correction renames the album.
		al, err := ds.Album(ctx).Get(one.AlbumID)
		Expect(err).ToNot(HaveOccurred())
		old := model.Correction{Kind: model.CorrectionAlbum, MatchKey: model.AlbumFolderKey(*al), From: "Greatful Dead – Live", To: "Greatful Dead – Live!", AlbumName: "Live!"}
		Expect(ds.Correction(ctx).Add(&old)).To(Succeed())
		scan(true)
		Expect(track(live1).Album).To(Equal("Live!"))

		res := save("/correction/album", map[string]any{"albumId": track(live1).AlbumID, "name": "Live at Winterland",
			"albumArtist": "Greatful Dead", "year": 1978, "tracks": []map[string]any{{"id": two.ID, "discNumber": 0, "trackNumber": 3}}})

		Expect(tagsOf(live1)).To(HaveKeyWithValue("ALBUM", []string{"Live at Winterland"}))
		Expect(tagsOf(live1)).To(HaveKeyWithValue("DATE", []string{"1978"}))
		Expect(tagsOf(live2)).To(HaveKeyWithValue("ALBUM", []string{"Live at Winterland"}))
		Expect(tagsOf(live2)).To(HaveKeyWithValue("TRACKNUMBER", []string{"3"}))
		// No Correction is left, the Tracks are the same, and the album, under its new ID, keeps its star.
		Expect(ds.Correction(ctx).GetAll()).To(BeEmpty())
		Expect(track(live1).ID).To(Equal(one.ID))
		Expect(track(live2).ID).To(Equal(two.ID))
		Expect(track(live1).Album).To(Equal("Live at Winterland"))
		Expect(track(live2).TrackNumber).To(Equal(3))
		Expect(res.AlbumID).To(Equal(track(live1).AlbumID))
		Expect(res.AlbumID).ToNot(Equal(one.AlbumID))
		renamed, err := ds.Album(ctx).Get(res.AlbumID)
		Expect(err).ToNot(HaveOccurred())
		Expect(renamed.Starred).To(BeTrue())
		Expect(renamed.MaxYear).To(Equal(1978))
		// One job, of the album's two Files.
		Expect(jobs()).To(HaveLen(1))
		Expect(jobs()[0].Kind).To(Equal(corrections.JobAlbumEdit))
		Expect(ds.EditJournal(ctx).ForJob(res.JobID)).To(HaveLen(2))

		// Undone as a whole: the Files, and the Correction, are as they were.
		undoJob(res.JobID)
		Expect(tagsOf(live1)).To(HaveKeyWithValue("ALBUM", []string{"Live"}))
		Expect(tagsOf(live1)).To(HaveKeyWithValue("DATE", []string{"1977"}))
		Expect(tagsOf(live2)).To(HaveKeyWithValue("TRACKNUMBER", []string{"2"}))
		all, err := ds.Correction(ctx).GetAll()
		Expect(err).ToNot(HaveOccurred())
		Expect(all).To(HaveLen(1))
		Expect(all[0].AlbumName).To(Equal("Live!"))
		Expect(track(live1).Album).To(Equal("Live!"))
		Expect(track(live1).ID).To(Equal(one.ID))
		back, err := ds.Album(ctx).Get(track(live1).AlbumID)
		Expect(err).ToNot(HaveOccurred())
		Expect(back.Starred).To(BeTrue())
	})

	It("writes Track order into each File, as one job, keeping the Tracks", func() {
		one, two := track(live1), track(live2)
		res := save("/correction/trackNumbers", map[string]any{"albumId": one.AlbumID, "tracks": []map[string]any{
			{"id": one.ID, "discNumber": 0, "trackNumber": 2}, {"id": two.ID, "discNumber": 0, "trackNumber": 1}}})
		Expect(tagsOf(live1)).To(HaveKeyWithValue("TRACKNUMBER", []string{"2"}))
		Expect(tagsOf(live2)).To(HaveKeyWithValue("TRACKNUMBER", []string{"1"}))
		Expect(track(live1).ID).To(Equal(one.ID))
		Expect(track(live1).TrackNumber).To(Equal(2))
		Expect(res.AlbumID).To(Equal(one.AlbumID))
		Expect(jobs()).To(HaveLen(1))
		Expect(jobs()[0].Kind).To(Equal(corrections.JobTrackNumbers))
		Expect(ds.Correction(ctx).GetAll()).To(BeEmpty())
	})

	It("renames an Artist in its Files, and its star goes with it", func() {
		deadID := metadata.ArtistID("Greatful Dead")
		Expect(doReq("star", "artistId", deadID).Status).To(Equal(responses.StatusOK))
		one := track(live1)

		res := save("/correction/artistRename", map[string]any{"artistId": deadID, "name": "The Dead"})
		Expect(tagsOf(live1)).To(HaveKeyWithValue("ARTIST", []string{"The Dead"}))
		Expect(tagsOf(live2)).To(HaveKeyWithValue("ALBUMARTIST", []string{"The Dead"}))
		Expect(res.ArtistID).To(Equal(metadata.ArtistID("The Dead")))
		renamed, err := ds.Artist(ctx).Get(res.ArtistID)
		Expect(err).ToNot(HaveOccurred())
		Expect(renamed.Starred).To(BeTrue())
		Expect(ds.Artist(ctx).Get(deadID)).Error().To(MatchError(model.ErrNotFound))
		Expect(track(live1).ID).To(Equal(one.ID))
		Expect(ds.Correction(ctx).GetAll()).To(BeEmpty())
		Expect(jobs()[0].Kind).To(Equal(corrections.JobArtistRename))
	})

	It("merges two Artists in their Files, joining their stars and plays, and undoing it splits them again", func() {
		deadID, gratefulID := metadata.ArtistID("Greatful Dead"), metadata.ArtistID("Grateful Dead")
		Expect(doReq("star", "artistId", deadID).Status).To(Equal(responses.StatusOK))
		Expect(doReq("setRating", "id", deadID, "rating", "4").Status).To(Equal(responses.StatusOK))
		Expect(doReq("scrobble", "id", track(live1).ID, "submission", "true").Status).To(Equal(responses.StatusOK))
		Expect(doReq("scrobble", "id", track(studio).ID, "submission", "true").Status).To(Equal(responses.StatusOK))
		Expect(doReq("scrobble", "id", track(studio).ID, "submission", "true").Status).To(Equal(responses.StatusOK))
		one := track(live1)

		res := save("/correction/artistMerge", map[string]any{"artistId": deadID, "intoId": gratefulID})
		Expect(tagsOf(live1)).To(HaveKeyWithValue("ARTIST", []string{"Grateful Dead"}))
		Expect(tagsOf(live2)).To(HaveKeyWithValue("ALBUMARTIST", []string{"Grateful Dead"}))
		Expect(tagsOf(studio)).To(HaveKeyWithValue("ARTIST", []string{"Grateful Dead"}))
		Expect(res.ArtistID).To(Equal(gratefulID))
		merged, err := ds.Artist(ctx).Get(gratefulID)
		Expect(err).ToNot(HaveOccurred())
		Expect(merged.Starred).To(BeTrue())
		Expect(merged.Rating).To(Equal(4))
		Expect(merged.PlayCount).To(Equal(int64(3)))
		Expect(ds.Artist(ctx).Get(deadID)).Error().To(MatchError(model.ErrNotFound))
		Expect(track(live1).ID).To(Equal(one.ID))
		Expect(ds.Correction(ctx).GetAll()).To(BeEmpty())
		Expect(jobs()[0].Kind).To(Equal(corrections.JobArtistMerge))

		// Undone: two Artists again, each with its own.
		undoJob(res.JobID)
		Expect(tagsOf(live1)).To(HaveKeyWithValue("ARTIST", []string{"Greatful Dead"}))
		Expect(tagsOf(live2)).To(HaveKeyWithValue("ALBUMARTIST", []string{"Greatful Dead"}))
		dead, err := ds.Artist(ctx).Get(deadID)
		Expect(err).ToNot(HaveOccurred())
		Expect(dead.Starred).To(BeTrue())
		Expect(dead.Rating).To(Equal(4))
		Expect(dead.PlayCount).To(Equal(int64(1)))
		grateful, err := ds.Artist(ctx).Get(gratefulID)
		Expect(err).ToNot(HaveOccurred())
		Expect(grateful.Starred).To(BeFalse())
		Expect(grateful.Rating).To(BeZero())
		Expect(grateful.PlayCount).To(Equal(int64(2)))
		Expect(track(live1).ID).To(Equal(one.ID))
		Expect(track(live1).Artist).To(Equal("Greatful Dead"))
	})
})
