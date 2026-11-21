package e2e

import (
	"encoding/json"
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
	"github.com/navidrome/navidrome/core/metrics"
	"github.com/navidrome/navidrome/core/playlists"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/scanner"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/server/events"
	"github.com/navidrome/navidrome/server/nativeapi"
	"github.com/navidrome/navidrome/server/subsonic/responses"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// A Rip carries SOURCE and SOURCEURL tags, and the UI shows it with a "Ripped" badge
// (issue #29). The badge comes only from the File's tags, so these specs drop real,
// hand-tagged Files (tests/fixtures/rips) into a real Inbox folder and scan them.
var _ = Describe("Rips", Ordered, func() {
	const youTubeURL = "https://www.youtube.com/watch?v=dQw4w9WgXcQ"

	var admin model.User
	var native http.Handler

	// Rescanning albums that are already in the database deadlocks the album-refresh phase
	// at the harness's MaxOpenConns=1 (its cursor holds the only connection while its workers
	// wait for one; see subsonic_artwork_test.go). Scan with production's minimum pool instead.
	scanAll := func(fullScan bool) {
		db.Db().SetMaxOpenConns(4)
		defer db.Db().SetMaxOpenConns(1)
		s := scanner.New(ctx, ds, events.NoopBroker(),
			playlists.NewPlaylists(ds, artwork.NewUploader(ds)), metrics.NewNoopInstance())
		_, err := s.ScanAll(ctx, fullScan)
		Expect(err).ToNot(HaveOccurred())
	}

	trackTitled := func(title string) model.MediaFile {
		mfs, err := ds.MediaFile(ctx).GetAll(model.QueryOptions{Filters: squirrel.Eq{"media_file.title": title}})
		Expect(err).ToNot(HaveOccurred())
		Expect(mfs).To(HaveLen(1), "Track %q", title)
		return mfs[0]
	}

	// nativeGet calls the native API the web UI uses, as the admin, and decodes a JSON list.
	nativeGet := func(path string) []map[string]any {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		token, err := auth.CreateToken(&admin)
		Expect(err).ToNot(HaveOccurred())
		req.Header.Set(consts.UIAuthorizationHeader, "Bearer "+token)
		w := httptest.NewRecorder()
		native.ServeHTTP(w, req)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var items []map[string]any
		Expect(json.Unmarshal(w.Body.Bytes(), &items)).To(Succeed())
		return items
	}

	byTitle := func(items []map[string]any, title string) map[string]any {
		for _, it := range items {
			if it["title"] == title {
				return it
			}
		}
		Fail("no item titled " + title)
		return nil
	}

	BeforeAll(func() {
		setupTestDB()

		inbox := GinkgoT().TempDir()
		for _, name := range []string{"ripped.opus", "ripped.m4a", "ripped.mp3", "untagged.opus"} {
			data, err := os.ReadFile(filepath.Join("tests", "fixtures", "rips", name))
			Expect(err).ToNot(HaveOccurred())
			Expect(os.WriteFile(filepath.Join(inbox, name), data, 0600)).To(Succeed())
		}
		Expect(ensureInbox(inbox)).To(BeTrue())
		scanAll(false)

		loaded, err := ds.User(ctx).FindByUsername(adminUser.UserName)
		Expect(err).ToNot(HaveOccurred())
		admin = *loaded

		native = server.JWTVerifier(nativeapi.New(ds, core.NewShare(ds), playlists.NewPlaylists(ds, artwork.NewUploader(ds)), nil,
			tests.NewMockLibraryService(), tests.NewMockUserService(), nil, nil, nil, nil, nil))
	})

	DescribeTable("a hand-tagged File dropped into the Inbox scans in with its source",
		func(title string) {
			mf := trackTitled(title)
			Expect(mf.Source).To(Equal("YouTube"))
			Expect(mf.SourceURL).To(Equal(youTubeURL))
		},
		Entry("opus", "Ripped Opus"),
		Entry("m4a", "Ripped M4A"),
		Entry("mp3", "Ripped MP3"),
	)

	It("gives a File without source tags no source", func() {
		mf := trackTitled("Not Ripped")
		Expect(mf.Source).To(BeEmpty())
		Expect(mf.SourceURL).To(BeEmpty())
	})

	It("shows the source on the songs the UI lists", func() {
		songs := nativeGet("/song?_start=0&_end=100")

		rip := byTitle(songs, "Ripped Opus")
		Expect(rip).To(HaveKeyWithValue("source", "YouTube"))
		Expect(rip).To(HaveKeyWithValue("sourceUrl", youTubeURL))
		Expect(byTitle(songs, "Not Ripped")).ToNot(HaveKey("source"))
	})

	It("shows the source on a Playlist's tracks", func() {
		resp := doReqWithUser(admin, "createPlaylist", "name", "Rips To Replace",
			"songId", trackTitled("Ripped M4A").ID, "songId", trackTitled("Not Ripped").ID)
		Expect(resp.Status).To(Equal(responses.StatusOK))

		tracks := nativeGet("/playlist/" + resp.Playlist.Id + "/tracks?_start=0&_end=100")
		Expect(byTitle(tracks, "Ripped M4A")).To(HaveKeyWithValue("source", "YouTube"))
		Expect(byTitle(tracks, "Not Ripped")).ToNot(HaveKey("source"))
	})

	It("keeps the source through a rescan", func() {
		scanAll(false)
		Expect(trackTitled("Ripped Opus").Source).To(Equal("YouTube"))

		scanAll(true)
		Expect(trackTitled("Ripped Opus").Source).To(Equal("YouTube"))
		Expect(trackTitled("Ripped Opus").SourceURL).To(Equal(youTubeURL))
	})

	// What an existing install looks like after the migration adds the columns: the Files
	// were read before the tags were mapped. The migration forces this full rescan.
	It("reads the source of existing Files again on a full rescan", func() {
		_, err := db.Db().ExecContext(ctx, `UPDATE media_file SET source = '', source_url = ''`)
		Expect(err).ToNot(HaveOccurred())

		scanAll(true)

		Expect(trackTitled("Ripped MP3").Source).To(Equal("YouTube"))
		Expect(trackTitled("Ripped MP3").SourceURL).To(Equal(youTubeURL))
		Expect(trackTitled("Not Ripped").Source).To(BeEmpty())
	})
})
