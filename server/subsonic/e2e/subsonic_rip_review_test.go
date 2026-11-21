package e2e

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"github.com/Masterminds/squirrel"
	_ "github.com/navidrome/navidrome/adapters/gotaglib" // reads the real Files below
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/core/metrics"
	"github.com/navidrome/navidrome/core/playlists"
	"github.com/navidrome/navidrome/core/rip"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/scanner"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/server/events"
	"github.com/navidrome/navidrome/server/ripapi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// inboxScanner is the real scanner, run at production's minimum connection pool (see the
// note in subsonic_rips_test.go), as the Ripper runs it after a review or Discard.
type inboxScanner struct{ s model.Scanner }

func (i inboxScanner) ScanFolders(ctx context.Context, full bool, targets []model.ScanTarget) ([]string, error) {
	db.Db().SetMaxOpenConns(4)
	defer db.Db().SetMaxOpenConns(1)
	return i.s.ScanFolders(ctx, full, targets)
}

// Reviewing a Rip (#33) writes the admin's tags into the Files and Discard deletes them; either
// way the Tracks follow after the Inbox is rescanned. These specs drive the Rip API against a
// real database, real Files (tests/fixtures/rips) and the real scanner.
var _ = Describe("Rip review and Discard", func() {
	const youTubeURL = "https://www.youtube.com/watch?v=dQw4w9WgXcQ"

	var (
		inbox   string
		inboxID int
		api     http.Handler
		admin   model.User
		job     *model.RipJob
		single  *model.RipJob
	)

	BeforeEach(func() {
		setupTestDB()
		conf.Server.Ripping.Enabled = true

		inbox = GinkgoT().TempDir()
		Expect(os.Mkdir(filepath.Join(inbox, "Rip Album"), 0o755)).To(Succeed())
		copyFixture := func(name, rel string) {
			data, err := os.ReadFile(filepath.Join("tests", "fixtures", "rips", name))
			Expect(err).ToNot(HaveOccurred())
			Expect(os.WriteFile(filepath.Join(inbox, rel), data, 0o644)).To(Succeed())
		}
		copyFixture("ripped.opus", "Rip Album/01.opus")
		copyFixture("ripped.m4a", "Rip Album/02.m4a")
		copyFixture("ripped.mp3", "Rip Album/03.mp3")
		copyFixture("untagged.opus", "Single.opus")
		Expect(ensureInbox(inbox)).To(BeTrue())
		lib, err := core.FindInboxLibrary(ctx, ds)
		Expect(err).ToNot(HaveOccurred())
		inboxID = lib.ID

		s := inboxScanner{scanner.New(ctx, ds, events.NoopBroker(),
			playlists.NewPlaylists(ds, artwork.NewUploader(ds)), metrics.NewNoopInstance())}
		_, err = s.ScanFolders(ctx, false, []model.ScanTarget{{LibraryID: inboxID}})
		Expect(err).ToNot(HaveOccurred())

		save := func(folder string, paths ...string) *model.RipJob {
			j := &model.RipJob{UserID: adminUser.ID, URL: youTubeURL, Status: model.RipReady, Folder: folder, Source: "YouTube"}
			Expect(ds.RipJob(ctx).Put(j)).To(Succeed())
			for i, p := range paths {
				it := model.RipItem{JobID: j.ID, Position: i + 1, URL: youTubeURL, Status: model.RipReady, Path: p}
				Expect(ds.RipItem(ctx).Put(&it)).To(Succeed())
				j.Items = append(j.Items, it)
			}
			return j
		}
		job = save("Rip Album", "Rip Album/01.opus", "Rip Album/02.m4a", "Rip Album/03.mp3")
		single = save("", "Single.opus")

		loaded, err := ds.User(ctx).FindByUsername(adminUser.UserName)
		Expect(err).ToNot(HaveOccurred())
		admin = *loaded
		api = server.JWTVerifier(ripapi.New(ds, rip.New(ds, nil, s, rip.Options{})))
	})

	call := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		token, err := auth.CreateToken(&admin)
		Expect(err).ToNot(HaveOccurred())
		req.Header.Set(consts.UIAuthorizationHeader, "Bearer "+token)
		w := httptest.NewRecorder()
		api.ServeHTTP(w, req)
		return w
	}

	// tracksAt returns the Inbox's Tracks (missing ones included) scanned from the given File.
	tracksAt := func(path string) func() model.MediaFiles {
		return func() model.MediaFiles {
			mfs, err := ds.MediaFile(ctx).GetAll(model.QueryOptions{Filters: squirrel.Eq{
				"media_file.library_id": inboxID, "media_file.path": path}})
			Expect(err).ToNot(HaveOccurred())
			return mfs
		}
	}
	titleAt := func(path string) func() string {
		return func() string {
			mfs := tracksAt(path)()
			if len(mfs) != 1 {
				return ""
			}
			return mfs[0].Title
		}
	}

	It("starts with every File scanned in as a Track", func() {
		Expect(titleAt("Rip Album/01.opus")()).To(Equal("Ripped Opus"))
		Expect(titleAt("Single.opus")()).To(Equal("Not Ripped"))
	})

	It("writes the saved tags into the Files, and the Tracks update after the rescan", func() {
		body := `{"album":"Reviewed Album","albumArtist":"Rip Band","year":"2004","tracks":[` +
			`{"itemId":"` + job.Items[0].ID + `","title":"First Song","trackNumber":"1"},` +
			`{"itemId":"` + job.Items[1].ID + `","title":"Second Song","artist":"Guest","trackNumber":"2"}]}`
		w := call(http.MethodPut, "/"+job.ID+"/tags", body)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())

		Eventually(titleAt("Rip Album/01.opus")).Should(Equal("First Song"))
		Eventually(titleAt("Rip Album/02.m4a")).Should(Equal("Second Song"))
		Eventually(scanner.IsScanning).Should(BeFalse())

		first := tracksAt("Rip Album/01.opus")()[0]
		Expect(first.Album).To(Equal("Reviewed Album"))
		Expect(first.AlbumArtist).To(Equal("Rip Band"))
		Expect(first.Year).To(Equal(2004))
		Expect(first.TrackNumber).To(Equal(1))
		Expect(first.Artist).To(Equal("Rip Artist"))
		// The Ripped badge's tags survive the edit.
		Expect(first.Source).To(Equal("YouTube"))
		Expect(first.SourceURL).To(Equal(youTubeURL))

		second := tracksAt("Rip Album/02.m4a")()[0]
		Expect(second.Artist).To(Equal("Guest"))
		Expect(second.SourceURL).To(Equal(youTubeURL))
		third := tracksAt("Rip Album/03.mp3")()[0]
		Expect(third.Title).To(Equal("Ripped MP3"))
		Expect(third.Album).To(Equal("Reviewed Album"))
		Expect(third.Source).To(Equal("YouTube"))
		Expect(first.AlbumID).To(Equal(third.AlbumID))
	})

	It("discards one Track: its File is deleted and its Track is gone after the rescan", func() {
		mf := tracksAt("Rip Album/02.m4a")()[0]

		w := call(http.MethodDelete, "/"+job.ID+"/items/"+job.Items[1].ID, "")
		Expect(w.Code).To(Equal(http.StatusNoContent), w.Body.String())

		Expect(filepath.Join(inbox, "Rip Album", "02.m4a")).ToNot(BeAnExistingFile())
		Eventually(tracksAt("Rip Album/02.m4a")).Should(BeEmpty())
		_, err := ds.MediaFile(ctx).Get(mf.ID)
		Expect(err).To(MatchError(model.ErrNotFound))
		Expect(titleAt("Rip Album/01.opus")()).To(Equal("Ripped Opus"))
		items, err := ds.RipItem(ctx).ForJobs(job.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(items).To(HaveLen(2))
	})

	It("discards a whole job: Files, folder, rows and Tracks all go, and other Rips stay", func() {
		w := call(http.MethodDelete, "/"+job.ID, "")
		Expect(w.Code).To(Equal(http.StatusNoContent), w.Body.String())

		Expect(filepath.Join(inbox, "Rip Album")).ToNot(BeADirectory())
		for _, p := range []string{"Rip Album/01.opus", "Rip Album/02.m4a", "Rip Album/03.mp3"} {
			Eventually(tracksAt(p)).Should(BeEmpty(), p)
		}
		_, err := ds.RipJob(ctx).Get(job.ID)
		Expect(err).To(MatchError(model.ErrNotFound))
		Expect(ds.RipItem(ctx).ForJobs(job.ID)).To(BeEmpty())

		Expect(titleAt("Single.opus")()).To(Equal("Not Ripped"))
		Expect(ds.RipJob(ctx).Get(single.ID)).ToNot(BeNil())
		Expect(filepath.Join(inbox, "Single.opus")).To(BeAnExistingFile())
	})
})
