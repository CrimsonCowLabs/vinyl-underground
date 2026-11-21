package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"mime/multipart"
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
	"github.com/navidrome/navidrome/core/metrics"
	"github.com/navidrome/navidrome/core/playlists"
	"github.com/navidrome/navidrome/core/rip"
	"github.com/navidrome/navidrome/core/rip/match"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/scanner"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/server/events"
	"github.com/navidrome/navidrome/server/ripapi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.senan.xyz/taglib"
)

// Uploaded and dropped Files (#35) get the same review card as Rips: review, Discard and Promote
// all work on them, with ripping off. These specs run the real database, real Files, the real
// scanner and the real "already have" matcher.
var _ = Describe("Uploads and dropped Files", func() {
	const settle = 20 * time.Millisecond

	var (
		inbox, nasDir string
		inboxID       int
		nas           model.Library
		admin         model.User
		s             inboxScanner
		ripper        *rip.Ripper
		api           http.Handler
	)

	scanInbox := func() {
		Eventually(scanner.IsScanning).Should(BeFalse())
		var err error
		Eventually(func() bool {
			_, err = s.ScanFolders(ctx, false, []model.ScanTarget{{LibraryID: inboxID}})
			return errors.Is(err, scanner.ErrAlreadyScanning)
		}).Should(BeFalse())
		Expect(err).ToNot(HaveOccurred())
	}
	reconcile := func() {
		_, err := ripper.ReconcileDrops(ctx)
		Expect(err).ToNot(HaveOccurred())
		time.Sleep(settle + 10*time.Millisecond)
		_, err = ripper.ReconcileDrops(ctx)
		Expect(err).ToNot(HaveOccurred())
	}
	inboxTrack := func(path string) func() *model.MediaFile {
		return func() *model.MediaFile {
			mfs, err := ds.MediaFile(ctx).GetAll(model.QueryOptions{Filters: squirrel.Eq{
				"media_file.library_id": inboxID, "media_file.path": path, "media_file.missing": false}})
			Expect(err).ToNot(HaveOccurred())
			if len(mfs) != 1 {
				return nil
			}
			return &mfs[0]
		}
	}
	jobs := func() model.RipJobs {
		js, err := ripper.Jobs(ctx)
		Expect(err).ToNot(HaveOccurred())
		return js
	}
	call := func(method, path string, body *bytes.Buffer, contentType string) *httptest.ResponseRecorder {
		if body == nil {
			body = &bytes.Buffer{}
		}
		req := httptest.NewRequest(method, path, body)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		token, err := auth.CreateToken(&admin)
		Expect(err).ToNot(HaveOccurred())
		req.Header.Set(consts.UIAuthorizationHeader, "Bearer "+token)
		w := httptest.NewRecorder()
		api.ServeHTTP(w, req)
		return w
	}
	review := func(jobID string) rip.Review {
		w := call(http.MethodGet, "/"+jobID+"/tags", nil, "")
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var rev rip.Review
		Expect(json.Unmarshal(w.Body.Bytes(), &rev)).To(Succeed())
		return rev
	}
	fixture := func(name string) []byte {
		data, err := os.ReadFile(filepath.Join("tests", "fixtures", name))
		Expect(err).ToNot(HaveOccurred())
		return data
	}
	drop := func(fixtureName, rel string) {
		full := filepath.Join(inbox, filepath.FromSlash(rel))
		Expect(os.MkdirAll(filepath.Dir(full), 0o755)).To(Succeed())
		Expect(os.WriteFile(full, fixture(fixtureName), 0o644)).To(Succeed())
	}

	BeforeEach(func() {
		setupTestDB()
		conf.Server.Ripping.Enabled = false // uploads and drops aren't Rips

		nasDir = GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(nasDir, "Vinyl Underground"), 0o755)).To(Succeed())
		nas = model.Library{Name: "NAS", Path: nasDir}
		Expect(ds.Library(ctx).Put(&nas)).To(Succeed())

		inbox = GinkgoT().TempDir()
		conf.Server.Inbox.Path = inbox
		Expect(ensureInbox(inbox)).To(BeTrue())
		lib, err := core.FindInboxLibrary(ctx, ds)
		Expect(err).ToNot(HaveOccurred())
		inboxID = lib.ID

		s = inboxScanner{scanner.New(ctx, ds, events.NoopBroker(),
			playlists.NewPlaylists(ds, artwork.NewUploader(ds)), metrics.NewNoopInstance())}
		ripper = rip.New(ds, nil, s, rip.Options{
			DropSettle: settle, ScanRetryDelay: 10 * time.Millisecond, UploadScanDelay: time.Millisecond,
			Matcher: match.New(ds), PromoteRoot: filepath.Join(nasDir, "Vinyl Underground"),
		})
		loaded, err := ds.User(ctx).FindByUsername(adminUser.UserName)
		Expect(err).ToNot(HaveOccurred())
		admin = *loaded
		api = server.JWTVerifier(ripapi.New(ds, ripper))
	})

	It("reviews and promotes uploaded Files, and never adopts them as dropped", func() {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		for _, f := range []struct{ name, fixture string }{
			{"first.opus", "rips/ripped.opus"}, {"second.mp3", "test.mp3"}, {"bad.mp3", "test.txt"},
		} {
			part, err := mw.CreateFormFile("file", f.name)
			Expect(err).ToNot(HaveOccurred())
			_, _ = part.Write(fixture(f.fixture))
		}
		Expect(mw.Close()).To(Succeed())
		w := call(http.MethodPost, "/upload", &body, mw.FormDataContentType())
		Expect(w.Code).To(Equal(http.StatusCreated), w.Body.String())
		var res struct {
			Job   model.RipJob `json:"job"`
			Files []struct {
				Path  string `json:"path"`
				Error string `json:"error"`
			} `json:"files"`
		}
		Expect(json.Unmarshal(w.Body.Bytes(), &res)).To(Succeed())
		Expect(res.Files[2].Error).To(ContainSubstring("isn't MP3 audio"))
		job := res.Job

		// The upload's own scan brings in its Tracks.
		Eventually(inboxTrack(res.Files[0].Path), 5*time.Second).ShouldNot(BeNil())
		Eventually(inboxTrack(res.Files[1].Path), 5*time.Second).ShouldNot(BeNil())
		Eventually(scanner.IsScanning).Should(BeFalse())
		reconcile()
		Expect(jobs()).To(HaveLen(1), "the uploaded Files are not adopted as dropped ones")

		rev := review(job.ID)
		Expect(rev.Tracks).To(HaveLen(2))
		Expect(rev.Tracks[0].Title).To(Equal("Ripped Opus"))
		Expect(rev.Tracks[1].Title).To(Equal("Song"))

		w = call(http.MethodPost, "/"+job.ID+"/promote", nil, "")
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var promoted rip.PromoteResult
		Expect(json.Unmarshal(w.Body.Bytes(), &promoted)).To(Succeed())
		Expect(promoted.Failed).To(BeEmpty())
		Expect(promoted.Promoted).To(HaveLen(2))
		Expect(jobs()).To(BeEmpty())
		Expect(filepath.Join(inbox, job.Folder)).ToNot(BeADirectory())
		for _, p := range promoted.Promoted {
			Expect(filepath.Join(nasDir, p.Path)).To(BeAnExistingFile())
		}
	})

	It("groups a dropped folder into one card that can be reviewed, discarded and promoted", func() {
		drop("rips/ripped.opus", "Dropped Album/01.opus")
		drop("rips/ripped.m4a", "Dropped Album/02.m4a")
		drop("rips/untagged.opus", "Dropped Album/03.opus")
		drop("test.mp3", "Loose Song.mp3")
		Expect(os.Chtimes(filepath.Join(inbox, "Dropped Album"), time.Now().Add(-time.Hour), time.Now().Add(-time.Hour))).To(Succeed())
		scanInbox()
		reconcile()
		scanInbox() // a rescan adopts nothing twice
		reconcile()

		js := jobs()
		Expect(js).To(HaveLen(2))
		var album, loose model.RipJob
		for _, j := range js {
			Expect(j.Kind).To(Equal(model.RipKindDrop))
			if j.Folder == "Dropped Album" {
				album = j
			} else {
				loose = j
			}
		}
		Expect(album.Items).To(HaveLen(3))
		Expect(loose.Items).To(HaveLen(1))
		Expect(loose.Folder).To(BeEmpty())

		// The File's own Inbox Track is never its own "already have".
		own := inboxTrack("Dropped Album/01.opus")()
		Expect(own).ToNot(BeNil())
		for _, it := range album.Items {
			for _, m := range it.AlreadyHave {
				Expect(m.ID).ToNot(Equal(inboxTrack(it.Path)().ID))
			}
		}

		rev := review(album.ID)
		Expect(rev.Tracks).To(HaveLen(3))
		Expect(rev.Tracks[0].MediaFileID).To(Equal(own.ID))

		// Discarding a dropped File deletes it from disk.
		w := call(http.MethodDelete, "/"+album.ID+"/items/"+album.Items[2].ID, nil, "")
		Expect(w.Code).To(Equal(http.StatusNoContent), w.Body.String())
		Expect(filepath.Join(inbox, "Dropped Album", "03.opus")).ToNot(BeAnExistingFile())

		// Saving tags writes no SOURCE into a File that had none.
		w = call(http.MethodPut, "/"+loose.ID+"/tags", bytes.NewBufferString(`{"album":"Loose"}`), "application/json")
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		tags, err := taglib.ReadTags(filepath.Join(inbox, "Loose Song.mp3"))
		Expect(err).ToNot(HaveOccurred())
		Expect(tags).ToNot(HaveKey(rip.TagSource))
		Expect(tags).ToNot(HaveKey(rip.TagSourceURL))
		Expect(call(http.MethodDelete, "/"+loose.ID, nil, "").Code).To(Equal(http.StatusNoContent))
		Expect(filepath.Join(inbox, "Loose Song.mp3")).ToNot(BeAnExistingFile())

		Eventually(scanner.IsScanning).Should(BeFalse())
		w = call(http.MethodPost, "/"+album.ID+"/promote", nil, "")
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var promoted rip.PromoteResult
		Expect(json.Unmarshal(w.Body.Bytes(), &promoted)).To(Succeed())
		Expect(promoted.Failed).To(BeEmpty())
		Expect(promoted.Promoted).To(HaveLen(2))
		Expect(filepath.Join(inbox, "Dropped Album")).ToNot(BeADirectory())
		Expect(jobs()).To(BeEmpty())

		// Once promoted, the Files are nobody's drop any more.
		Eventually(scanner.IsScanning).Should(BeFalse())
		scanInbox()
		reconcile()
		Expect(jobs()).To(BeEmpty())
	})
})
