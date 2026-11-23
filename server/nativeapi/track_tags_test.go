package nativeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("/api/trackTags", func() {
	var (
		router          http.Handler
		api             *Router
		queue           *editjob.Queue
		admin, listener model.User
		path            string
		// runQueue starts the edit job queue, until the test ends.
		runQueue func()
	)

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.EnableSharing = false
		ds := &tests.MockDataStore{}
		auth.Init(ds)

		libDir := GinkgoT().TempDir()
		path = filepath.Join(libDir, "01 Song.mp3")
		data, err := os.ReadFile(filepath.Join("tests", "fixtures", "test.mp3"))
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(path, data, 0o644)).To(Succeed())
		info, err := os.Stat(path)
		Expect(err).ToNot(HaveOccurred())
		ds.MockedLibrary = &tests.MockLibraryRepo{}
		ds.MockedLibrary.(*tests.MockLibraryRepo).SetData(model.Libraries{{ID: 1, Name: "Music", Path: libDir}})
		mfs := tests.CreateMockMediaFileRepo()
		mfs.SetData(model.MediaFiles{{ID: "t1", LibraryID: 1, Path: "01 Song.mp3", Title: "Song", Size: info.Size(), UpdatedAt: info.ModTime()}})
		ds.MockedMediaFile = mfs

		editor := tagedit.New(ds, nil)
		api = New(ds, nil, nil, nil, tests.NewMockLibraryService(), tests.NewMockUserService(),
			nil, nil, nil, nil, nil, nil, nil, editor)
		queue = editjob.New(ds, nil, editjob.Kinds(editor))
		api.SetEditQueue(queue)
		router = server.JWTVerifier(api)
		runQueue = func() {
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { queue.Run(ctx); close(done) }()
			DeferCleanup(func() { cancel(); Eventually(done).Should(BeClosed()) })
		}
		admin = model.User{ID: "admin-1", UserName: "admin", IsAdmin: true, NewPassword: "pass"}
		listener = model.User{ID: "user-1", UserName: "listener", NewPassword: "pass"}
		Expect(ds.User(context.TODO()).Put(&admin)).To(Succeed())
		Expect(ds.User(context.TODO()).Put(&listener)).To(Succeed())

		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Name: "Music", Writable: true}}})
		DeferCleanup(core.SetLibraryEditing, core.LibraryEditing{})
	})

	doAt := func(user model.User, method, url string, body any) *httptest.ResponseRecorder {
		GinkgoHelper()
		token, err := auth.CreateToken(&user)
		Expect(err).ToNot(HaveOccurred())
		var b []byte
		if body != nil {
			b, err = json.Marshal(body)
			Expect(err).ToNot(HaveOccurred())
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, createAuthenticatedRequest(method, url, bytes.NewBuffer(b), token))
		return w
	}
	do := func(user model.User, method string, body any) *httptest.ResponseRecorder {
		GinkgoHelper()
		return doAt(user, method, "/trackTags/t1", body)
	}
	errorCode := func(w *httptest.ResponseRecorder) string {
		GinkgoHelper()
		var got map[string]string
		Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
		return got["error"]
	}

	It("shows the admin the Track's tags from its File", func() {
		w := do(admin, http.MethodGet, nil)
		Expect(w.Code).To(Equal(http.StatusOK))
		var got tagedit.TrackTags
		Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
		Expect(got.Values).To(HaveKeyWithValue("title", []string{"Song"}))
		Expect(got.Fields).ToNot(BeEmpty())
	})

	It("writes the admin's edit into the File, through the edit job queue", func() {
		runQueue()
		w := do(admin, http.MethodPut, map[string]any{"values": map[string][]string{"title": {"Song (Live)"}}})
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var got tagedit.Result
		Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
		Expect(got.Changed).To(BeTrue())
		Expect(got.Entry).ToNot(BeEmpty())
		w = do(admin, http.MethodGet, nil)
		Expect(w.Body.String()).To(ContainSubstring("Song (Live)"))

		jobs, err := queue.Jobs(context.Background(), 0)
		Expect(err).ToNot(HaveOccurred())
		Expect(jobs).To(HaveLen(1))
		Expect(jobs[0].UserID).To(Equal(admin.ID))
	})

	It("answers that the edit runs in the background when its job doesn't end soon", func() {
		api.editWait = 50 * time.Millisecond
		// The queue isn't running yet: the job waits.
		w := do(admin, http.MethodPut, map[string]any{"values": map[string][]string{"title": {"Song (Live)"}}})
		Expect(w.Code).To(Equal(http.StatusAccepted), w.Body.String())
		var got struct {
			JobID      string `json:"jobId"`
			Background bool   `json:"background"`
		}
		Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
		Expect(got.Background).To(BeTrue())
		Expect(got.JobID).ToNot(BeEmpty())

		runQueue()
		Eventually(func() string { return do(admin, http.MethodGet, nil).Body.String() }).Should(ContainSubstring("Song (Live)"))
		w = doAt(admin, http.MethodGet, "/editJob/"+got.JobID, nil)
		Expect(w.Code).To(Equal(http.StatusOK))
		var job model.EditJob
		Expect(json.Unmarshal(w.Body.Bytes(), &job)).To(Succeed())
		Expect(job.State).To(Equal(model.EditJobDone))
		Expect(job.Steps).To(HaveLen(1))
		Expect(job.Steps[0].State).To(Equal(model.EditStepWritten))
	})

	It("lists the edit jobs, with the Files they skipped and why", func() {
		runQueue()
		later := time.Now().Add(time.Hour)
		Expect(os.Chtimes(path, later, later)).To(Succeed())
		Expect(do(admin, http.MethodPut, map[string]any{"values": map[string][]string{"title": {"x"}}}).Code).To(Equal(http.StatusConflict))

		w := doAt(admin, http.MethodGet, "/editJob", nil)
		Expect(w.Code).To(Equal(http.StatusOK))
		var jobs []model.EditJob
		Expect(json.Unmarshal(w.Body.Bytes(), &jobs)).To(Succeed())
		Expect(jobs).To(HaveLen(1))
		Expect(jobs[0].Skipped).To(Equal(1))
		Expect(jobs[0].Steps).To(HaveLen(1))
		Expect(jobs[0].Steps[0].Code).To(Equal("changedOnDisk"))
		Expect(jobs[0].Steps[0].Path).To(Equal("01 Song.mp3"))

		Expect(doAt(listener, http.MethodGet, "/editJob", nil).Code).To(Equal(http.StatusForbidden))
		Expect(doAt(admin, http.MethodGet, "/editJob/nope", nil).Code).To(Equal(http.StatusNotFound))
	})

	It("refuses a File that changed on disk", func() {
		runQueue()
		later := time.Now().Add(time.Hour)
		Expect(os.Chtimes(path, later, later)).To(Succeed())
		w := do(admin, http.MethodPut, map[string]any{"values": map[string][]string{"title": {"x"}}})
		Expect(w.Code).To(Equal(http.StatusConflict))
		Expect(errorCode(w)).To(Equal("changedOnDisk"))
	})

	It("refuses an invalid value", func() {
		runQueue()
		w := do(admin, http.MethodPut, map[string]any{"values": map[string][]string{"tracknumber": {"seven"}}})
		Expect(w.Code).To(Equal(http.StatusBadRequest))
		Expect(errorCode(w)).To(Equal("invalid"))
	})

	It("is off while editing is off", func() {
		core.SetLibraryEditing(core.LibraryEditing{Libraries: []core.LibraryWritable{{ID: 1, Name: "Music"}}})
		Expect(errorCode(do(admin, http.MethodGet, nil))).To(Equal("editingOff"))
		w := do(admin, http.MethodPut, map[string]any{"values": map[string][]string{"title": {"x"}}})
		Expect(w.Code).To(Equal(http.StatusForbidden))
		Expect(errorCode(w)).To(Equal("editingOff"))
	})

	It("is admin only", func() {
		Expect(do(listener, http.MethodGet, nil).Code).To(Equal(http.StatusForbidden))
		w := do(listener, http.MethodPut, map[string]any{"values": map[string][]string{"title": {"x"}}})
		Expect(w.Code).To(Equal(http.StatusForbidden))
		Expect(do(admin, http.MethodGet, nil).Body.String()).ToNot(ContainSubstring(`"x"`))
	})
})
