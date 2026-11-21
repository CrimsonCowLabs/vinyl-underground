package ripapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/core/rip"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Uploads (#35) through the API: admin only, and there whenever the Inbox is, ripping or not.
var _ = Describe("Upload API", func() {
	var (
		ds       *tests.MockDataStore
		inbox    string
		router   http.Handler
		admin    model.User
		listener model.User
		opts     rip.Options
	)

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.Ripping.Enabled = false
		conf.Server.SessionTimeout = time.Minute
		users := tests.CreateMockUserRepo()
		libs := &tests.MockLibraryRepo{}
		libs.SetData(model.Libraries{{ID: 1, Name: "Music", Path: GinkgoT().TempDir()}})
		libs.PutFn = func(l *model.Library) error {
			if l.ID == 0 {
				l.ID = 2
			}
			libs.Data[l.ID] = *l
			return nil
		}
		ds = &tests.MockDataStore{MockedUser: users, MockedProperty: &tests.MockedPropertyRepo{}, MockedLibrary: libs}
		auth.Init(ds)
		inbox = GinkgoT().TempDir()
		conf.Server.Inbox.Path = inbox
		_, err := core.EnsureInboxLibrary(GinkgoT().Context(), ds, inbox)
		Expect(err).ToNot(HaveOccurred())
		admin = model.User{ID: "admin-1", UserName: "admin", IsAdmin: true, NewPassword: "pw"}
		listener = model.User{ID: "user-1", UserName: "listener", NewPassword: "pw"}
		Expect(users.Put(&admin)).To(Succeed())
		Expect(users.Put(&listener)).To(Succeed())
		opts = rip.Options{UploadScanDelay: time.Millisecond}
	})

	fixture := func(name string) []byte {
		data, err := os.ReadFile(filepath.Join("tests", "fixtures", name))
		Expect(err).ToNot(HaveOccurred())
		return data
	}
	type file struct {
		name string
		data []byte
	}
	post := func(user *model.User, query string, files ...file) *httptest.ResponseRecorder {
		router = server.JWTVerifier(New(ds, rip.New(ds, nil, nil, opts)))
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		for _, f := range files {
			part, err := mw.CreateFormFile("file", f.name)
			Expect(err).ToNot(HaveOccurred())
			_, err = part.Write(f.data)
			Expect(err).ToNot(HaveOccurred())
		}
		Expect(mw.Close()).To(Succeed())
		req := httptest.NewRequest(http.MethodPost, "/upload"+query, &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		if user != nil {
			token, err := auth.CreateToken(user)
			Expect(err).ToNot(HaveOccurred())
			req.Header.Set(consts.UIAuthorizationHeader, "Bearer "+token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	get := func(user *model.User, method, path string) int {
		router = server.JWTVerifier(New(ds, rip.New(ds, nil, nil, opts)))
		req := httptest.NewRequest(method, path, nil)
		token, err := auth.CreateToken(user)
		Expect(err).ToNot(HaveOccurred())
		req.Header.Set(consts.UIAuthorizationHeader, "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w.Code
	}
	type response struct {
		Error string         `json:"error"`
		Job   *model.RipJob  `json:"job"`
		Files []UploadedFile `json:"files"`
	}
	decode := func(w *httptest.ResponseRecorder) response {
		var res response
		Expect(json.Unmarshal(w.Body.Bytes(), &res)).To(Succeed(), w.Body.String())
		return res
	}

	It("takes several Files at once into one upload job, and says which were refused", func() {
		w := post(&admin, "", file{"one.mp3", fixture("test.mp3")}, file{"two.flac", fixture("test.flac")},
			file{"notes.txt", fixture("test.txt")})
		Expect(w.Code).To(Equal(http.StatusCreated), w.Body.String())
		res := decode(w)
		Expect(res.Job.Kind).To(Equal(model.RipKindUpload))
		Expect(res.Job.Status).To(Equal(model.RipReady))
		Expect(res.Files).To(HaveLen(3))
		Expect(res.Files[0].Path).To(Equal(res.Job.Folder + "/one.mp3"))
		Expect(res.Files[1].Path).To(Equal(res.Job.Folder + "/two.flac"))
		Expect(res.Files[2].Path).To(BeEmpty())
		Expect(res.Files[2].Error).To(Equal("notes.txt: only mp3, m4a, opus, ogg, flac, aiff or wav Files can be uploaded"))
		Expect(filepath.Join(inbox, res.Files[0].Path)).To(BeAnExistingFile())
	})

	It("adds a batch sent File by File to one job", func() {
		first := decode(post(&admin, "", file{"one.mp3", fixture("test.mp3")}))
		w := post(&admin, "?job="+first.Job.ID, file{"two.ogg", fixture("test.ogg")})
		Expect(w.Code).To(Equal(http.StatusCreated))
		Expect(decode(w).Job.ID).To(Equal(first.Job.ID))
		jobs, err := ds.RipJob(GinkgoT().Context()).GetAll()
		Expect(err).ToNot(HaveOccurred())
		Expect(jobs).To(HaveLen(1))
	})

	It("refuses a request with no File it can take, with a clear message", func() {
		w := post(&admin, "", file{"fake.mp3", fixture("test.txt")})
		Expect(w.Code).To(Equal(http.StatusUnprocessableEntity))
		res := decode(w)
		Expect(res.Job).To(BeNil())
		Expect(res.Error).To(Equal("fake.mp3: its content isn't MP3 audio; only mp3, m4a, opus, ogg, flac, aiff or wav Files can be uploaded"))
	})

	It("refuses a File over the limit, and reads no further", func() {
		opts.MaxUploadSize = 1024
		w := post(&admin, "", file{"big.mp3", fixture("test.mp3")}, file{"next.mp3", fixture("test.mp3")})
		Expect(w.Code).To(Equal(http.StatusUnprocessableEntity))
		res := decode(w)
		Expect(res.Files).To(HaveLen(1))
		Expect(res.Error).To(ContainSubstring("bigger than"))
		Expect(w.Header().Get("Connection")).To(Equal("close"))
	})

	It("answers 404 for an upload that is gone, and 409 for a job that isn't an upload", func() {
		Expect(post(&admin, "?job=gone", file{"one.mp3", fixture("test.mp3")}).Code).To(Equal(http.StatusNotFound))
		job := &model.RipJob{Kind: model.RipKindRip, Status: model.RipReady, URL: "https://www.youtube.com/watch?v=x"}
		Expect(ds.RipJob(GinkgoT().Context()).Put(job)).To(Succeed())
		Expect(post(&admin, "?job="+job.ID, file{"one.mp3", fixture("test.mp3")}).Code).To(Equal(http.StatusConflict))
	})

	It("wants multipart Files", func() {
		Expect(post(&admin, "").Code).To(Equal(http.StatusBadRequest))
		router = server.JWTVerifier(New(ds, rip.New(ds, nil, nil, opts)))
		req := httptest.NewRequest(http.MethodPost, "/upload", bytes.NewBufferString(`{}`))
		token, _ := auth.CreateToken(&admin)
		req.Header.Set(consts.UIAuthorizationHeader, "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		Expect(w.Code).To(Equal(http.StatusBadRequest))
	})

	It("is for the admin only", func() {
		Expect(post(&listener, "", file{"one.mp3", fixture("test.mp3")}).Code).To(Equal(http.StatusForbidden))
		Expect(post(nil, "", file{"one.mp3", fixture("test.mp3")}).Code).To(Equal(http.StatusUnauthorized))
		Expect(os.ReadDir(inbox)).To(BeEmpty())
	})

	It("works with ripping off, which only turns off the Rips themselves", func() {
		Expect(post(&admin, "", file{"one.mp3", fixture("test.mp3")}).Code).To(Equal(http.StatusCreated))
		Expect(get(&admin, http.MethodGet, "/")).To(Equal(http.StatusOK))
		Expect(get(&admin, http.MethodPost, "/")).To(Equal(http.StatusNotFound))
		Expect(get(&admin, http.MethodGet, "/ytdlp")).To(Equal(http.StatusNotFound))
		Expect(get(&admin, http.MethodPost, "/ytdlp/update")).To(Equal(http.StatusNotFound))
		Expect(get(&listener, http.MethodGet, "/")).To(Equal(http.StatusForbidden))
	})

	It("isn't there without an Inbox, with ripping on or off", func() {
		conf.Server.Inbox.Path = ""
		Expect(post(&admin, "", file{"one.mp3", fixture("test.mp3")}).Code).To(Equal(http.StatusNotFound))
		Expect(get(&admin, http.MethodGet, "/")).To(Equal(http.StatusNotFound))
		conf.Server.Ripping.Enabled = true
		Expect(post(&admin, "", file{"one.mp3", fixture("test.mp3")}).Code).To(Equal(http.StatusNotFound))
		Expect(get(&admin, http.MethodGet, "/")).To(Equal(http.StatusOK))
	})
})
