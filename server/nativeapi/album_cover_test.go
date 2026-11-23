package nativeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/core/corrections"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakeCoverPicker struct {
	candidates map[artwork.CoverSource]*artwork.CoverCandidates
	err        error
	stored     []string
	preview    string // when set, what every preview answers
}

func (f *fakeCoverPicker) Candidates(_ context.Context, albumID string, source artwork.CoverSource) (*artwork.CoverCandidates, error) {
	if f.err != nil {
		return nil, f.err
	}
	if c, ok := f.candidates[source]; ok && albumID == "al-1" {
		return c, nil
	}
	return nil, model.ErrNotFound
}

func (f *fakeCoverPicker) Preview(_ context.Context, albumID, candidateID string) (io.ReadCloser, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.preview != "" {
		return io.NopCloser(strings.NewReader(f.preview)), nil
	}
	return io.NopCloser(strings.NewReader("\x89PNG\r\n\x1a\n" + albumID + "/" + candidateID)), nil
}

func (f *fakeCoverPicker) Store(_ context.Context, albumID, candidateID string) (artwork.StoredCover, artwork.CoverCandidate, error) {
	if f.err != nil {
		return artwork.StoredCover{}, artwork.CoverCandidate{}, f.err
	}
	f.stored = append(f.stored, albumID+"/"+candidateID)
	stored, err := artwork.StoreChosenCover(testPNG())
	return stored, artwork.CoverCandidate{ID: candidateID, Source: "folder", Label: "cover.png"}, err
}

// fakeFolderRepo has the Library's folders, for finding an album's own folder.
type fakeFolderRepo struct {
	model.FolderRepository
	folders []model.Folder
}

func (f fakeFolderRepo) GetAll(...model.QueryOptions) ([]model.Folder, error) { return f.folders, nil }

func testPNG() []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// fakeCoverKind stands in for the cover write step: it records the steps it runs.
type fakeCoverKind struct {
	mu    sync.Mutex
	steps []model.EditJobStep
}

func (f *fakeCoverKind) Check(_ context.Context, step *model.EditJobStep) error {
	step.Path = "Band/Record/cover.jpg"
	return nil
}

func (f *fakeCoverKind) Run(_ context.Context, _ string, step *model.EditJobStep) (editjob.Outcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.steps = append(f.steps, *step)
	return editjob.Outcome{Changed: true, EntryID: "entry-1"}, nil
}

func (f *fakeCoverKind) Code(err error) string { return tagedit.Code(err) }

func (f *fakeCoverKind) ran() []model.EditJobStep {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.steps)
}

var _ = Describe("Album cover API", func() {
	var svc *fakeCorrections
	var picker *fakeCoverPicker
	var router http.Handler
	var adminToken, userToken string
	var ds *tests.MockDataStore
	var editor *tagedit.Editor
	var coverKind *fakeCoverKind
	var nativeRouter *Router

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.EnableSharing = false
		conf.Server.DataFolder = conf.NewDir(GinkgoT().TempDir())
		ds = &tests.MockDataStore{MockedEditJob: &tests.MockEditJobRepo{}, MockedEditJournal: &tests.MockEditJournalRepo{}}
		auth.Init(ds)
		picker = &fakeCoverPicker{candidates: map[artwork.CoverSource]*artwork.CoverCandidates{
			artwork.CoverSourceLocal:           {Candidates: []artwork.CoverCandidate{{ID: "cand-1", Source: "folder", Label: "cover.png"}}},
			artwork.CoverSourceCoverArtArchive: {Candidates: []artwork.CoverCandidate{}, Failed: true},
		}}
		// The album's own folder, where its cover goes (CheckCover runs before a write is queued).
		libDir := GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(libDir, "Band", "Record"), 0o755)).To(Succeed())
		ds.MockedLibrary = &tests.MockLibraryRepo{}
		ds.MockedLibrary.(*tests.MockLibraryRepo).SetData(model.Libraries{{ID: 1, Name: "Music", Path: libDir}})
		ds.MockedFolder = fakeFolderRepo{folders: []model.Folder{{ID: "f-1", LibraryID: 1, Path: "Band", Name: "Record", ParentID: "f-band"}}}
		mfs := tests.CreateMockMediaFileRepo()
		mfs.SetCountAll(0) // no other album's Tracks in its folder
		ds.MockedMediaFile = mfs
		ds.Album(context.TODO()).(*tests.MockAlbumRepo).SetData(model.Albums{
			{ID: "al-1", Name: "Record", LibraryID: 1, FolderIDs: []string{"f-1"}}})
		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Name: "Music", Writable: true}}})
		DeferCleanup(core.SetLibraryEditing, core.LibraryEditing{})
		editor = tagedit.New(ds, nil)
		nativeRouter = New(ds, nil, nil, nil, tests.NewMockLibraryService(), tests.NewMockUserService(), nil, nil, nil, nil, nil, nil, picker, editor)
		svc = &fakeCorrections{}
		nativeRouter.corrections = svc
		// The cover write runs as an edit job; its step is a fake here (core/tagedit tests the
		// write itself, on real Files).
		coverKind = &fakeCoverKind{}
		kinds := editjob.Kinds(ds, editor)
		kinds[model.EditCover] = coverKind
		queue := editjob.New(ds, nil, kinds)
		nativeRouter.SetEditQueue(queue)
		qctx, stop := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { queue.Run(qctx); close(done) }()
		DeferCleanup(func() { stop(); Eventually(done).Should(BeClosed()) })
		router = server.JWTVerifier(nativeRouter)

		adminUser := model.User{ID: "admin-1", UserName: "admin", IsAdmin: true, NewPassword: "adminpass"}
		regularUser := model.User{ID: "user-1", UserName: "regular", IsAdmin: false, NewPassword: "userpass"}
		Expect(ds.User(context.TODO()).Put(&adminUser)).To(Succeed())
		Expect(ds.User(context.TODO()).Put(&regularUser)).To(Succeed())
		var err error
		adminToken, err = auth.CreateToken(&adminUser)
		Expect(err).ToNot(HaveOccurred())
		userToken, err = auth.CreateToken(&regularUser)
		Expect(err).ToNot(HaveOccurred())
	})

	do := func(method, path, token string, body any) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		if body != nil {
			Expect(json.NewEncoder(&buf).Encode(body)).To(Succeed())
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, createAuthenticatedRequest(method, path, &buf, token))
		return w
	}

	It("lists one source's candidates", func() {
		w := do("GET", "/albumCover/al-1/candidates?source=local", adminToken, nil)
		Expect(w.Code).To(Equal(http.StatusOK))
		var got artwork.CoverCandidates
		Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
		Expect(got.Candidates).To(HaveLen(1))
		Expect(got.Candidates[0].Label).To(Equal("cover.png"))

		w = do("GET", "/albumCover/al-1/candidates?source=coverartarchive", adminToken, nil)
		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(w.Body.String()).To(ContainSubstring(`"failed":true`))
	})

	It("serves a candidate's preview", func() {
		w := do("GET", "/albumCover/al-1/candidates/cand-1/preview", adminToken, nil)
		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(w.Header().Get("Content-Type")).To(Equal("image/png"))
		Expect(w.Header().Get("X-Content-Type-Options")).To(Equal("nosniff"))
		Expect(w.Body.String()).To(HaveSuffix("al-1/cand-1"))
	})

	It("refuses a preview authenticated only by a token in the URL", func() {
		w := httptest.NewRecorder()
		req := createUnauthenticatedRequest("GET", "/albumCover/al-1/candidates/cand-1/preview?jwt="+adminToken, nil)
		router.ServeHTTP(w, req)
		Expect(w.Code).To(Equal(http.StatusUnauthorized))
		Expect(w.Header().Get("Content-Type")).ToNot(HavePrefix("image/"))
	})

	It("refuses a preview whose URL carries a token, even with the header", func() {
		w := do("GET", "/albumCover/al-1/candidates/cand-1/preview?jwt="+adminToken, adminToken, nil)
		Expect(w.Code).To(Equal(http.StatusUnauthorized))
	})

	It("never serves a preview that isn't an image", func() {
		picker.preview = "<html><script>alert(1)</script></html>"
		w := do("GET", "/albumCover/al-1/candidates/cand-1/preview", adminToken, nil)
		Expect(w.Code).To(Equal(http.StatusBadGateway))
		Expect(w.Body.String()).ToNot(ContainSubstring("script"))
	})

	// written is the image the one cover write job ran with.
	written := func() []byte {
		GinkgoHelper()
		Eventually(coverKind.ran).Should(HaveLen(1))
		step := coverKind.ran()[0]
		Expect(step.Kind).To(Equal(model.EditCover))
		var args struct{ AlbumID, Sum string }
		Expect(json.Unmarshal([]byte(step.Args), &args)).To(Succeed())
		Expect(args.AlbumID).To(Equal("al-1"))
		img, err := editor.KeptImage(args.Sum)
		Expect(err).ToNot(HaveOccurred())
		return img
	}

	It("chooses a candidate, writing it as the album's cover.jpg through an edit job, with no Correction", func() {
		w := do("POST", "/correction/albumCover", adminToken, map[string]string{"albumId": "al-1", "candidateId": "cand-1"})
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		Expect(picker.stored).To(Equal([]string{"al-1/cand-1"}))
		var res struct {
			Changed bool   `json:"changed"`
			Entry   string `json:"entryId"`
			JobID   string `json:"jobId"`
		}
		Expect(json.Unmarshal(w.Body.Bytes(), &res)).To(Succeed())
		Expect(res.Changed).To(BeTrue())
		Expect(res.Entry).To(Equal("entry-1"))
		Expect(res.JobID).ToNot(BeEmpty())
		Expect(written()[:3]).To(Equal([]byte{0xFF, 0xD8, 0xFF}), "the PNG is written as a JPEG")
		Expect(svc.covers).To(BeEmpty())
	})

	It("answers editingOff, and stores nothing, while editing is off", func() {
		nativeRouter.SetEditQueue(nil)
		w := do("POST", "/correction/albumCover", adminToken, map[string]string{"albumId": "al-1", "candidateId": "cand-1"})
		Expect(w.Code).To(Equal(http.StatusForbidden))
		Expect(w.Body.String()).To(ContainSubstring(`"editingOff"`))
		Expect(picker.stored).To(BeEmpty())
		Expect(svc.covers).To(BeEmpty())
	})

	DescribeTable("answers each failure with a status and a code the UI can show",
		func(err error, status int, code string) {
			picker.err = err
			w := do("POST", "/correction/albumCover", adminToken, map[string]string{"albumId": "al-1", "candidateId": "cand-1"})
			Expect(w.Code).To(Equal(status))
			var res struct{ Error string }
			Expect(json.Unmarshal(w.Body.Bytes(), &res)).To(Succeed())
			Expect(res.Error).To(Equal(code))
			Expect(coverKind.ran()).To(BeEmpty())
		},
		Entry("candidate forgotten", model.ErrNotFound, http.StatusNotFound, "notFound"),
		Entry("not an image", model.ErrValidation, http.StatusBadRequest, "invalid"),
		Entry("source unreachable", errors.New("connection refused"), http.StatusBadGateway, "unavailable"),
	)

	Describe("uploading a cover", func() {
		upload := func(token, albumID, filename string, data []byte) *httptest.ResponseRecorder {
			GinkgoHelper()
			var buf bytes.Buffer
			mw := multipart.NewWriter(&buf)
			Expect(mw.WriteField("albumId", albumID)).To(Succeed())
			fw, err := mw.CreateFormFile("image", filename)
			Expect(err).ToNot(HaveOccurred())
			_, err = fw.Write(data)
			Expect(err).ToNot(HaveOccurred())
			Expect(mw.Close()).To(Succeed())
			req := createAuthenticatedRequest("POST", "/correction/albumCoverUpload", &buf, token)
			req.Header.Set("Content-Type", mw.FormDataContentType())
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			return w
		}
		BeforeEach(func() {
			ds.Album(context.TODO()).(*tests.MockAlbumRepo).SetData(model.Albums{
				{ID: "al-1", Name: "Record", LibraryID: 1, FolderIDs: []string{"f-1"}}})
		})

		It("writes the image as the album's cover.jpg, with no Correction", func() {
			w := upload(adminToken, "al-1", "front.png", testPNG())
			Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
			Expect(written()[:3]).To(Equal([]byte{0xFF, 0xD8, 0xFF}))
			Expect(svc.covers).To(BeEmpty())
		})

		It("keeps nothing for an unknown album", func() {
			w := upload(adminToken, "nope", "front.png", testPNG())
			Expect(w.Code).To(Equal(http.StatusNotFound))
			Expect(coverKind.ran()).To(BeEmpty())
			_, err := os.Stat(filepath.Join(conf.Server.DataFolder.String(), "edit-journal"))
			Expect(err).To(MatchError(os.ErrNotExist))
		})

		It("refuses a file that isn't an image, or is too large", func() {
			w := upload(adminToken, "al-1", "evil.png", []byte("<html>not an image</html>"))
			Expect(w.Code).To(Equal(http.StatusBadRequest))
			Expect(w.Body.String()).To(ContainSubstring(`"invalidImage"`))

			conf.Server.MaxImageUploadSize = "100"
			w = upload(adminToken, "al-1", "big.png", bytes.Repeat([]byte{0}, 1000))
			Expect(w.Code).To(Equal(http.StatusBadRequest))
			Expect(w.Body.String()).To(ContainSubstring(`"tooLarge"`))
			Expect(coverKind.ran()).To(BeEmpty())
		})

		It("is admin only, even when artwork upload is enabled", func() {
			conf.Server.EnableArtworkUpload = true
			Expect(upload(userToken, "al-1", "front.png", testPNG()).Code).To(Equal(http.StatusForbidden))
			Expect(coverKind.ran()).To(BeEmpty())
		})
	})

	Describe("Embed in Files", func() {
		It("answers notFound for an unknown album, and editingOff while editing is off", func() {
			w := do("POST", "/albumCover/nope/embed", adminToken, nil)
			Expect(w.Code).To(Equal(http.StatusNotFound))

			nativeRouter.SetEditQueue(nil)
			w = do("POST", "/albumCover/al-1/embed", adminToken, nil)
			Expect(w.Code).To(Equal(http.StatusForbidden))
			Expect(w.Body.String()).To(ContainSubstring(`"editingOff"`))
		})

		It("is admin only", func() {
			Expect(do("POST", "/albumCover/al-1/embed", userToken, nil).Code).To(Equal(http.StatusForbidden))
		})
	})

	Describe("marking a cover wrong", func() {
		It("blocks the image the admin saw", func() {
			w := do("POST", "/correction/albumCoverBlock", adminToken, map[string]string{"albumId": "al-1", "imageHash": "0123456789abcdef"})
			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(svc.blocks).To(Equal([]string{"al-1-x0123456789abcdef"}))
		})

		It("tells the admin when the cover has changed since", func() {
			svc.err = corrections.ErrCoverChanged
			w := do("POST", "/correction/albumCoverBlock", adminToken, map[string]string{"albumId": "al-1", "imageHash": "0123456789abcdef"})
			Expect(w.Code).To(Equal(http.StatusConflict))
			Expect(w.Body.String()).To(ContainSubstring(`"coverChanged"`))
		})

		It("is admin only", func() {
			w := do("POST", "/correction/albumCoverBlock", userToken, map[string]string{"albumId": "al-1", "imageHash": "0123456789abcdef"})
			Expect(w.Code).To(Equal(http.StatusForbidden))
			Expect(svc.blocks).To(BeEmpty())
		})
	})

	It("denies access to regular users", func() {
		Expect(do("GET", "/albumCover/al-1/candidates?source=local", userToken, nil).Code).To(Equal(http.StatusForbidden))
		Expect(do("GET", "/albumCover/al-1/candidates/cand-1/preview", userToken, nil).Code).To(Equal(http.StatusForbidden))
		Expect(do("POST", "/correction/albumCover", userToken, map[string]string{"albumId": "al-1", "candidateId": "cand-1"}).Code).
			To(Equal(http.StatusForbidden))
		Expect(picker.stored).To(BeEmpty())
	})
})
