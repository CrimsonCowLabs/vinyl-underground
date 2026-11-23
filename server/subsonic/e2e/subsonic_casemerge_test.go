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
	"strconv"

	"github.com/Masterminds/squirrel"
	_ "github.com/navidrome/navidrome/adapters/gotaglib" // reads the real Files below
	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/metrics"
	"github.com/navidrome/navidrome/core/organize"
	"github.com/navidrome/navidrome/core/playlists"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
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

// The case merge (#169) renames and moves folders on the NAS while their Tracks keep their
// identity: a Track's row is found by its File's path, so a rename that didn't carry the rows
// along would have the next scan make new Tracks, and lose everything attached to the old ones.
// These specs run the real database, real Files and the real scanner.
var _ = Describe("The case merge keeps each Track's identity", func() {
	const (
		kept    = "Music/Dire Straits/Brothers in Arms/01 So Far Away.mp3"
		renamed = "Music/DIRE STRAITS/Brothers in Arms/02 Money for Nothing.mp3"
		merged  = "Music/Dire Straits/Brothers in Arms/02 Money for Nothing.mp3"
	)

	var (
		nas        model.Library
		nasDir     string
		s          inboxScanner
		admin      model.User
		ids        map[string]string // by original path
		playlistID string
	)

	scan := func(full bool) {
		Eventually(scanner.IsScanning).Should(BeFalse())
		var err error
		Eventually(func() bool {
			_, err = s.ScanFolders(ctx, full, []model.ScanTarget{{LibraryID: nas.ID}})
			return errors.Is(err, scanner.ErrAlreadyScanning)
		}).Should(BeFalse())
		Expect(err).ToNot(HaveOccurred())
	}
	tracks := func() model.MediaFiles {
		mfs, err := ds.MediaFile(ctx).GetAll(model.QueryOptions{Filters: squirrel.Eq{"media_file.library_id": nas.ID, "media_file.missing": false}})
		Expect(err).ToNot(HaveOccurred())
		return mfs
	}
	exists := func(rel string) bool {
		_, err := os.Stat(filepath.Join(nasDir, filepath.FromSlash(rel)))
		return err == nil
	}
	song := func(rel, title string, track int) {
		abs := filepath.Join(nasDir, filepath.FromSlash(rel))
		Expect(os.MkdirAll(filepath.Dir(abs), 0o755)).To(Succeed())
		data, err := os.ReadFile(filepath.Join("tests", "fixtures", "test.mp3"))
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(abs, data, 0o644)).To(Succeed())
		Expect(taglib.WriteTags(abs, map[string][]string{"TITLE": {title}, "ALBUM": {"Brothers in Arms"},
			"ARTIST": {"Dire Straits"}, "ALBUMARTIST": {"Dire Straits"}, "TRACKNUMBER": {strconv.Itoa(track)}}, 0)).To(Succeed())
	}

	BeforeEach(func() {
		setupTestDB()

		nasDir = GinkgoT().TempDir()
		song(kept, "So Far Away", 1)
		song(renamed, "Money for Nothing", 2)
		nas = model.Library{Name: "NAS", Path: nasDir}
		Expect(ds.Library(ctx).Put(&nas)).To(Succeed())
		Expect(ds.User(ctx).SetUserLibraries(adminUser.ID, []int{lib.ID, nas.ID})).To(Succeed())
		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: nas.ID, Name: nas.Name, Writable: true}}})
		DeferCleanup(core.SetLibraryEditing, core.LibraryEditing{})

		s = inboxScanner{scanner.New(ctx, ds, events.NoopBroker(),
			playlists.NewPlaylists(ds, artwork.NewUploader(ds)), metrics.NewNoopInstance())}
		scan(false)
		ids = map[string]string{}
		for _, mf := range tracks() {
			ids[mf.Path] = mf.ID
		}
		Expect(ids).To(HaveKey(kept))
		Expect(ids).To(HaveKey(renamed))

		loaded, err := ds.User(ctx).FindByUsername(adminUser.UserName)
		Expect(err).ToNot(HaveOccurred())
		admin = *loaded

		for _, id := range ids {
			Expect(doReq("star", "id", id).Status).To(Equal(responses.StatusOK))
			Expect(doReq("setRating", "id", id, "rating", "4").Status).To(Equal(responses.StatusOK))
			Expect(doReq("scrobble", "id", id, "submission", "true").Status).To(Equal(responses.StatusOK))
		}
		resp := doReq("createPlaylist", "name", "Merged", "songId", ids[kept], "songId", ids[renamed])
		Expect(resp.Status).To(Equal(responses.StatusOK))
		playlistID = resp.Playlist.Id
	})

	call := func(method, url string, body any) *httptest.ResponseRecorder {
		editor := tagedit.New(ds, s)
		router := nativeapi.New(ds, core.NewShare(ds), playlists.NewPlaylists(ds, artwork.NewUploader(ds)), nil, tests.NewMockLibraryService(), tests.NewMockUserService(),
			nil, nil, nil, nil, nil, nil, nil, editor)
		queue := editjob.New(ds, nil, editjob.Kinds(ds, editor))
		for kind, k := range organize.JobKinds(ds, editor) {
			queue.Handle(kind, k)
		}
		router.SetEditQueue(queue)
		qctx, stop := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() { queue.Run(qctx); close(done) }()
		defer func() { stop(); <-done }()
		b, err := json.Marshal(body)
		Expect(err).ToNot(HaveOccurred())
		req := httptest.NewRequest(method, url, bytes.NewBuffer(b))
		token, err := auth.CreateToken(&admin)
		Expect(err).ToNot(HaveOccurred())
		req.Header.Set(consts.UIAuthorizationHeader, "Bearer "+token)
		w := httptest.NewRecorder()
		server.JWTVerifier(router).ServeHTTP(w, req)
		return w
	}
	type casePlan struct {
		Token   string               `json:"token"`
		Phase   string               `json:"phase"`
		Clashes []organize.CaseClash `json:"clashes"`
		Steps   []organize.CaseStep  `json:"steps"`
	}
	plan := func() casePlan {
		GinkgoHelper()
		w := call(http.MethodPost, "/organize/case/plan", nil)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var p casePlan
		Expect(json.Unmarshal(w.Body.Bytes(), &p)).To(Succeed())
		return p
	}
	run := func(p casePlan) model.EditJob {
		GinkgoHelper()
		w := call(http.MethodPost, "/organize/case/run", map[string]any{"token": p.Token})
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var job model.EditJob
		Expect(json.Unmarshal(w.Body.Bytes(), &job)).To(Succeed())
		Expect(job.Skipped).To(BeZero(), w.Body.String())
		return job
	}
	// attached checks that the two Tracks are the same Tracks, at paths, with what Listeners
	// attached to them.
	attached := func(paths map[string]string) {
		GinkgoHelper()
		all := tracks()
		Expect(all).To(HaveLen(2), "no other Track for a renamed File")
		got := map[string]string{}
		for _, mf := range all {
			got[mf.ID] = mf.Path
		}
		want := map[string]string{}
		for orig, now := range paths {
			want[ids[orig]] = now
		}
		Expect(got).To(Equal(want))
		for _, id := range ids {
			sng := doReq("getSong", "id", id)
			Expect(sng.Status).To(Equal(responses.StatusOK))
			Expect(sng.Song.Starred).ToNot(BeNil())
			Expect(sng.Song.UserRating).To(Equal(int32(4)))
			Expect(sng.Song.PlayCount).To(Equal(int64(1)))
		}
		pl := doReq("getPlaylist", "id", playlistID)
		Expect(pl.Playlist.Entry).To(HaveLen(2))
	}

	It("renames, merges, and undoes both phases; after each rescan the Tracks are the same", func() {
		p := plan()
		Expect(p.Phase).To(Equal(organize.PhaseRename))
		Expect(p.Clashes).To(ConsistOf(organize.CaseClash{LibraryID: nas.ID, Parent: "Music", Names: []string{"DIRE STRAITS", "Dire Straits"}}))
		renameJob := run(p)
		Expect(renameJob.Kind).To(Equal(model.EditCaseRename))
		tagged := "Music/DIRE STRAITS [case-merge]/Brothers in Arms/02 Money for Nothing.mp3"
		Expect(exists(tagged)).To(BeTrue())
		attached(map[string]string{kept: kept, renamed: tagged})
		scan(true)
		attached(map[string]string{kept: kept, renamed: tagged})

		p = plan()
		Expect(p.Phase).To(Equal(organize.PhaseMerge))
		Expect(p.Clashes).To(BeEmpty())
		mergeJob := run(p)
		Expect(mergeJob.Kind).To(Equal(model.EditCaseMerge))
		Expect(exists(merged)).To(BeTrue())
		Expect(exists("Music/DIRE STRAITS [case-merge]")).To(BeFalse(), "the emptied folders are removed")
		attached(map[string]string{kept: kept, renamed: merged})
		scan(true)
		attached(map[string]string{kept: kept, renamed: merged})
		Expect(plan().Phase).To(BeEmpty(), "a scan finds every File, and no clash is left")

		// Both phases' undos, newest first, from the Edit history.
		for _, job := range []model.EditJob{mergeJob, renameJob} {
			w := call(http.MethodPost, "/editJob/"+job.ID+"/undo", nil)
			Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		}
		Expect(exists(renamed)).To(BeTrue())
		attached(map[string]string{kept: kept, renamed: renamed})
		scan(true)
		attached(map[string]string{kept: kept, renamed: renamed})
	})

	It("sends a Duplicate to Quarantine and keeps its Track there, while the kept File's Track takes its path", func() {
		// The ALL CAPS side holds more Files, so it wins, and takes back the other's name.
		dup := "Music/DIRE STRAITS/Brothers in Arms/01 So Far Away.mp3"
		data, err := os.ReadFile(filepath.Join(nasDir, filepath.FromSlash(kept)))
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(nasDir, filepath.FromSlash(dup)), data, 0o644)).To(Succeed())
		scan(false)
		var dupID string
		for _, mf := range tracks() {
			if mf.Path == dup {
				dupID = mf.ID
			}
		}
		Expect(dupID).ToNot(BeEmpty())

		run(plan())
		p := plan()
		Expect(p.Phase).To(Equal(organize.PhaseMerge))
		run(p)
		Expect(exists("_Quarantine/Music/DIRE STRAITS [case-merge]/Brothers in Arms/01 So Far Away.mp3")).To(BeTrue())
		Expect(exists(kept)).To(BeTrue())
		Expect(exists(merged)).To(BeTrue())

		// The losing side's copy went to Quarantine: its Track waits there, missing, at the path
		// it left (for a restore), with what is attached to it; the winning side's copy, and its
		// Track, are at that path now. A full scan reads it into that Track, not the other.
		for range 2 {
			got := map[string]string{}
			for _, mf := range tracks() {
				got[mf.ID] = mf.Path
			}
			Expect(got).To(Equal(map[string]string{dupID: kept, ids[renamed]: merged}))
			quarantined, err := ds.MediaFile(ctx).Get(ids[kept])
			Expect(err).ToNot(HaveOccurred())
			Expect(quarantined.Quarantined).To(BeTrue())
			Expect(quarantined.Missing).To(BeTrue())
			Expect(quarantined.Path).To(Equal(kept))
			Expect(quarantined.Starred).To(BeTrue())
			scan(true)
		}
	})

	It("refuses a run of a plan that isn't the one reviewed", func() {
		w := call(http.MethodPost, "/organize/case/run", map[string]any{"token": "stale"})
		Expect(w.Code).To(Equal(http.StatusConflict), w.Body.String())
		Expect(exists(renamed)).To(BeTrue())
	})
})
