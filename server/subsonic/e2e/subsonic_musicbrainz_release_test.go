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
	"strings"
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
	"github.com/navidrome/navidrome/core/musicbrainz"
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

// Find on MusicBrainz writes the picked release into the album's Files (#170), on the real
// database, real Files and the real scanner, with MusicBrainz answered by a test server: the
// album's ID changes with its musicbrainz_albumid, and its star and play count follow it, the
// Tracks keep their IDs, stars, play counts and Playlist entries, and undoing the job puts the
// Files and the album back. A File that can't be written puts the others back, so the album is
// never left split.
var _ = Describe("A MusicBrainz release written into the Files", func() {
	const (
		relID   = "8a7f5c4e-0d5b-4f1e-9d3c-3b2a1f0e9d8c"
		groupID = "1b2c3d4e-5f60-4718-9a0b-c1d2e3f40516"
		bandID  = "6faa7ca7-0d99-4a5e-bfa6-1fd5037520c6"
		rec1    = "11111111-1111-4111-8111-111111111111"
		rec2    = "22222222-2222-4222-8222-222222222222"
		rt1     = "aaaaaaaa-1111-4111-8111-111111111111"
		rt2     = "aaaaaaaa-2222-4222-8222-222222222222"
		live1   = "Grateful Dead/Live/01 One.mp3"
		live2   = "Grateful Dead/Live/02 Two.mp3"
	)
	var (
		nas    model.Library
		nasDir string
		s      inboxScanner
		admin  model.User
		mbz    *httptest.Server
		asked  []string
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
		DeferCleanup(func(v bool) { conf.Server.EnableExternalServices = v }, conf.Server.EnableExternalServices)
		conf.Server.EnableExternalServices = true

		// MusicBrainz, as Client.Release asks it: "Grateful Dead – Live" (1977), two tracks.
		credit := musicbrainz.ArtistCredits{{Name: "Grateful Dead"}}
		credit[0].Artist.ID, credit[0].Artist.Name = bandID, "Grateful Dead"
		rel := musicbrainz.Release{ID: relID, Title: "Live", Date: "1977-05-08", Country: "US", ArtistCredit: credit,
			ReleaseGroup: musicbrainz.ReleaseGroup{ID: groupID, Title: "Live"},
			Media: []musicbrainz.Media{{Format: "CD", Position: 1, TrackCount: 2, Tracks: []musicbrainz.Track{
				{ID: rt1, Position: 1, Title: "One", Recording: musicbrainz.Recording{ID: rec1}},
				{ID: rt2, Position: 2, Title: "Two", Recording: musicbrainz.Recording{ID: rec2}},
			}}}}
		body, err := json.Marshal(rel)
		Expect(err).ToNot(HaveOccurred())
		asked = nil
		mbz = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			asked = append(asked, r.URL.Path)
			if r.URL.Path != "/release/"+relID {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(body)
		}))
		DeferCleanup(mbz.Close)

		nasDir = GinkgoT().TempDir()
		dead := func(title, n string) map[string][]string {
			return map[string][]string{"ARTIST": {"Grateful Dead"}, "ALBUMARTIST": {"Grateful Dead"}, "ALBUM": {"Live"},
				"DATE": {"1977"}, "TITLE": {title}, "TRACKNUMBER": {n}}
		}
		writeFile(live1, dead("One", "1"))
		writeFile(live2, dead("Two", "2"))
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

	// call runs one request against the native API, with its edit job queue running as at startup
	// (cmd/root.go), and MusicBrainz answered by the test server.
	call := func(method, url string, body any) *httptest.ResponseRecorder {
		GinkgoHelper()
		editor := tagedit.New(ds, s)
		router := nativeapi.New(ds, core.NewShare(ds), playlists.NewPlaylists(ds, artwork.NewUploader(ds)), nil, tests.NewMockLibraryService(), tests.NewMockUserService(),
			nil, nil, nil, nil, nil, nil, nil, editor)
		router.SetMusicBrainz(musicbrainz.New(musicbrainz.Options{BaseURL: mbz.URL + "/",
			Limiter: musicbrainz.NewLimiter(time.Millisecond, nil)}))
		queue := editjob.New(ds, nil, editjob.Kinds(ds, editor))
		for kind, k := range corrections.JobKinds(ds) {
			queue.Handle(kind, k)
		}
		queue.Handle(corrections.JobRelease, corrections.ReleaseJobKind(ds, queue))
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
		JobID        string         `json:"jobId"`
		Done         bool           `json:"done"`
		AlbumID      string         `json:"albumId"`
		Job          *model.EditJob `json:"job"`
		RolledBack   bool           `json:"rolledBack"`
		RollbackDone bool           `json:"rollbackDone"`
	}
	save := func(body any) saved {
		GinkgoHelper()
		w := call(http.MethodPost, "/correction/album", body)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var res saved
		Expect(json.Unmarshal(w.Body.Bytes(), &res)).To(Succeed())
		Expect(res.Done).To(BeTrue())
		return res
	}
	pick := func(albumID string) map[string]any {
		return map[string]any{"albumId": albumID, "name": "Live", "albumArtist": "Grateful Dead", "year": 1977,
			"mbzAlbumId": relID, "canonical": map[string]any{"date": true}}
	}

	It("pairs the album with the release, then writes its IDs as one job, keeping the album's and Tracks' annotations, and undoes it", func() {
		one, two := track(live1), track(live2)
		Expect(doReq("star", "albumId", one.AlbumID).Status).To(Equal(responses.StatusOK))
		Expect(doReq("star", "id", one.ID).Status).To(Equal(responses.StatusOK))
		Expect(doReq("scrobble", "id", one.ID, "submission", "true").Status).To(Equal(responses.StatusOK))
		pl := doReq("createPlaylist", "name", "Live picks", "songId", two.ID)
		Expect(pl.Status).To(Equal(responses.StatusOK))

		// The dialog's pairing, before saving: it writes nothing.
		w := call(http.MethodGet, "/musicbrainz/release/"+relID+"/match?albumId="+one.AlbumID, nil)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var m corrections.ReleaseMatch
		Expect(json.Unmarshal(w.Body.Bytes(), &m)).To(Succeed())
		Expect(m.Holds).To(BeTrue())
		Expect(m.Tracks).To(HaveLen(2))
		for _, t := range m.Tracks {
			Expect(t.Paired).ToNot(BeNil())
		}
		Expect(m.Canonical.Date).To(Equal(corrections.CanonicalChange{From: "1977", To: "1977-05-08"}))
		Expect(tagsOf(live1)).ToNot(HaveKey("MUSICBRAINZ_ALBUMID"))

		res := save(pick(one.AlbumID))
		Expect(res.Job.Skipped).To(BeZero())
		Expect(res.Job.Kind).To(Equal(corrections.JobRelease))
		Expect(ds.EditJournal(ctx).ForJob(res.JobID)).To(HaveLen(2), "one job, of the album's two Files")
		Expect(asked).ToNot(BeEmpty())
		Expect(asked).To(HaveEach(Equal("/release/" + relID)))

		for rel, ids := range map[string][2]string{live1: {rec1, rt1}, live2: {rec2, rt2}} {
			tags := tagsOf(rel)
			Expect(tags).To(HaveKeyWithValue("MUSICBRAINZ_ALBUMID", []string{relID}))
			Expect(tags).To(HaveKeyWithValue("MUSICBRAINZ_RELEASEGROUPID", []string{groupID}))
			Expect(tags).To(HaveKeyWithValue("MUSICBRAINZ_ALBUMARTISTID", []string{bandID}))
			Expect(tags).To(HaveKeyWithValue("MUSICBRAINZ_TRACKID", []string{ids[0]}))
			Expect(tags).To(HaveKeyWithValue("MUSICBRAINZ_RELEASETRACKID", []string{ids[1]}))
			Expect(tags).To(HaveKeyWithValue("DATE", []string{"1977-05-08"}), "the date the admin accepted")
			Expect(tags).To(HaveKeyWithValue("ALBUM", []string{"Live"}))
		}

		// The Tracks are the same rows, with their star, play count and Playlist entry.
		Expect(track(live1).ID).To(Equal(one.ID))
		Expect(track(live2).ID).To(Equal(two.ID))
		Expect(track(live1).MbzAlbumID).To(Equal(relID))
		Expect(track(live1).MbzRecordingID).To(Equal(rec1))
		song := doReq("getSong", "id", one.ID)
		Expect(song.Song.Starred).ToNot(BeNil())
		Expect(song.Song.PlayCount).To(Equal(int64(1)))
		got := doReq("getPlaylist", "id", pl.Playlist.Id)
		Expect(got.Playlist.Entry).To(ConsistOf(HaveField("Id", two.ID)))

		// The album's ID follows its musicbrainz_albumid, and its star and play count follow it.
		Expect(res.AlbumID).To(Equal(track(live1).AlbumID))
		Expect(res.AlbumID).ToNot(Equal(one.AlbumID))
		Expect(track(live2).AlbumID).To(Equal(res.AlbumID), "one album, not split")
		al, err := ds.Album(ctx).Get(res.AlbumID)
		Expect(err).ToNot(HaveOccurred())
		Expect(al.Starred).To(BeTrue())
		Expect(al.PlayCount).To(Equal(int64(1)))
		Expect(al.MbzAlbumID).To(Equal(relID))

		// Undone as a whole: the Files hold their old tags, and the album is back, starred.
		w = call(http.MethodPost, "/editJob/"+res.JobID+"/undo", nil)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		for _, rel := range []string{live1, live2} {
			tags := tagsOf(rel)
			Expect(tags).ToNot(HaveKey("MUSICBRAINZ_ALBUMID"))
			Expect(tags).ToNot(HaveKey("MUSICBRAINZ_TRACKID"))
			Expect(tags).To(HaveKeyWithValue("DATE", []string{"1977"}))
		}
		Expect(track(live1).ID).To(Equal(one.ID))
		Expect(track(live1).AlbumID).To(Equal(one.AlbumID))
		back, err := ds.Album(ctx).Get(one.AlbumID)
		Expect(err).ToNot(HaveOccurred())
		Expect(back.Starred).To(BeTrue())
		Expect(back.PlayCount).To(Equal(int64(1)))
	})

	It("puts the written Files back when one can't be written, so the album stays whole with its star", func() {
		one := track(live1)
		Expect(doReq("star", "albumId", one.AlbumID).Status).To(Equal(responses.StatusOK))
		// Two has a second name: the write path refuses it, as a copy would split it from it.
		other := filepath.Join(GinkgoT().TempDir(), "elsewhere.mp3")
		Expect(os.Link(filepath.Join(nasDir, filepath.FromSlash(live2)), other)).To(Succeed())

		res := save(pick(one.AlbumID))
		Expect(res.Job.Skipped).To(Equal(1))
		Expect(res.Job.Steps).To(ConsistOf(HaveField("Path", live2)))
		Expect(res.RolledBack).To(BeTrue())
		Expect(res.RollbackDone).To(BeTrue())
		Expect(res.AlbumID).To(BeEmpty())

		for _, rel := range []string{live1, live2} {
			Expect(tagsOf(rel)).ToNot(HaveKey("MUSICBRAINZ_ALBUMID"), rel)
		}
		Expect(track(live1).AlbumID).To(Equal(one.AlbumID))
		Expect(track(live2).AlbumID).To(Equal(one.AlbumID), "not split")
		al, err := ds.Album(ctx).Get(one.AlbumID)
		Expect(err).ToNot(HaveOccurred())
		Expect(al.Starred).To(BeTrue())

		// Once the other name is gone, saving again writes the release.
		Expect(os.Remove(other)).To(Succeed())
		res = save(pick(one.AlbumID))
		Expect(res.RolledBack).To(BeFalse())
		Expect(res.Job.Skipped).To(BeZero())
		Expect(strings.Join(tagsOf(live2)["MUSICBRAINZ_ALBUMID"], "")).To(Equal(relID))
		al, err = ds.Album(ctx).Get(res.AlbumID)
		Expect(err).ToNot(HaveOccurred())
		Expect(al.Starred).To(BeTrue())
	})
})
