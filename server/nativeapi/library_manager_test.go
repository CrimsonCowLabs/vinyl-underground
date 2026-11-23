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
	"github.com/navidrome/navidrome/core/librarymanager"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.senan.xyz/taglib"
)

var _ = Describe("Library manager API", func() {
	var (
		router          http.Handler
		api             *Router
		queue           *editjob.Queue
		queueStopped    bool
		admin, listener model.User
		path            string
	)

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.EnableSharing = false
		ds := &tests.MockDataStore{MockedEditJob: &tests.MockEditJobRepo{}}
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
		mfs.SetData(model.MediaFiles{{ID: "t1", LibraryID: 1, Path: "01 Song.mp3", Title: "Song", Artist: "The Band",
			Album: "Old Album", Suffix: "mp3", Size: info.Size(), UpdatedAt: info.ModTime()}})
		ds.MockedMediaFile = mfs
		ds.MockedEditJournal = &tests.MockEditJournalRepo{}

		editor := tagedit.New(ds, nil)
		api = New(ds, nil, nil, nil, tests.NewMockLibraryService(), tests.NewMockUserService(),
			nil, nil, nil, nil, nil, nil, nil, editor)
		queue = editjob.New(ds, nil, editjob.Kinds(ds, editor))
		api.SetEditQueue(queue)
		queueStopped = false
		router = server.JWTVerifier(api)
		admin = model.User{ID: "admin-1", UserName: "admin", IsAdmin: true, NewPassword: "pass"}
		listener = model.User{ID: "user-1", UserName: "listener", NewPassword: "pass"}
		Expect(ds.User(context.TODO()).Put(&admin)).To(Succeed())
		Expect(ds.User(context.TODO()).Put(&listener)).To(Succeed())

		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Name: "Music", Writable: true}}})
		DeferCleanup(core.SetLibraryEditing, core.LibraryEditing{})
	})

	JustBeforeEach(func() {
		if queueStopped {
			return
		}
		qctx, stop := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { queue.Run(qctx); close(done) }()
		DeferCleanup(func() { stop(); Eventually(done).Should(BeClosed()) })
	})

	do := func(user model.User, method, url string, body any) *httptest.ResponseRecorder {
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
	errorCode := func(w *httptest.ResponseRecorder) string {
		GinkgoHelper()
		var got map[string]string
		Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
		return got["error"]
	}
	setAlbum := map[string]any{"ids": []string{"t1"}, "field": "album", "values": []string{"New Album"}}

	It("lists the Tracks for the admin only", func() {
		w := do(admin, http.MethodGet, "/libraryTrack?_start=0&_end=25", nil)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		Expect(w.Body.String()).To(ContainSubstring(`"path":"01 Song.mp3"`))
		Expect(w.Header().Get("X-Total-Count")).To(Equal("1"))
		Expect(do(listener, http.MethodGet, "/libraryTrack?_start=0&_end=25", nil).Code).To(Equal(http.StatusForbidden))
	})

	It("lists the fields a bulk edit can set", func() {
		w := do(admin, http.MethodGet, "/libraryManager/fields", nil)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var got []tagedit.Field
		Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
		Expect(got).To(ContainElement(tagedit.Field{Key: "genre", Type: tagedit.TypeText, Multi: true}))
		Expect(got).ToNot(ContainElement(HaveField("Key", "title")))
	})

	It("previews a bulk edit, old to new", func() {
		w := do(admin, http.MethodPost, "/libraryManager/preview?_start=0&_end=50", setAlbum)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var got librarymanager.Preview
		Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
		Expect(got.Total).To(Equal(1))
		Expect(got.Changes).To(Equal(1))
		Expect(got.Rows).To(ConsistOf(HaveField("Old", []string{"Old Album"})))
		Expect(got.Rows[0].New).To(Equal([]string{"New Album"}))
	})

	It("runs a bulk edit as one job, and answers it once done", func() {
		w := do(admin, http.MethodPost, "/libraryManager/edit", setAlbum)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var job model.EditJob
		Expect(json.Unmarshal(w.Body.Bytes(), &job)).To(Succeed())
		Expect(job.State).To(Equal(model.EditJobDone))
		Expect(job.Total).To(Equal(1))
		Expect(job.Done).To(Equal(1))
		tags, err := taglib.ReadTags(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(tags["ALBUM"]).To(Equal([]string{"New Album"}))
	})

	When("the job takes longer than the wait", func() {
		BeforeEach(func() { queueStopped, api.editWait = true, 100*time.Millisecond })

		It("answers 202, and the job runs in the background", func() {
			w := do(admin, http.MethodPost, "/libraryManager/edit", setAlbum)
			Expect(w.Code).To(Equal(http.StatusAccepted), w.Body.String())
			var job model.EditJob
			Expect(json.Unmarshal(w.Body.Bytes(), &job)).To(Succeed())
			Expect(job.ID).ToNot(BeEmpty())
			Expect(job.State).To(Equal(model.EditJobQueued))
		})
	})

	It("refuses an edit no File can take,a field that isn't edited in bulk, and no selection", func() {
		// Its only Track's library is read-only now.
		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Name: "Music", Writable: false}}})
		w := do(admin, http.MethodPost, "/libraryManager/edit", setAlbum)
		Expect(w.Code).To(Equal(http.StatusUnprocessableEntity), w.Body.String())
		Expect(errorCode(w)).To(Equal("nothingToChange"))
		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Name: "Music", Writable: true}}})

		w = do(admin, http.MethodPost, "/libraryManager/preview", map[string]any{"ids": []string{"t1"}, "field": "title", "values": []string{"X"}})
		Expect(w.Code).To(Equal(http.StatusBadRequest), w.Body.String())
		Expect(errorCode(w)).To(Equal("invalid"))

		w = do(admin, http.MethodPost, "/libraryManager/preview", map[string]any{"field": "album", "values": []string{"X"}})
		Expect(w.Code).To(Equal(http.StatusBadRequest), w.Body.String())
		Expect(errorCode(w)).To(Equal("noSelection"))
	})

	It("is admin only, and answers editingOff with editing off", func() {
		Expect(do(listener, http.MethodPost, "/libraryManager/preview", setAlbum).Code).To(Equal(http.StatusForbidden))
		Expect(do(listener, http.MethodPost, "/libraryManager/edit", setAlbum).Code).To(Equal(http.StatusForbidden))
		api.SetEditQueue(nil)
		w := do(admin, http.MethodPost, "/libraryManager/edit", setAlbum)
		Expect(w.Code).To(Equal(http.StatusForbidden))
		Expect(errorCode(w)).To(Equal("editingOff"))
	})
})
