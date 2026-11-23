package ripapi

import (
	"bytes"
	"encoding/json"
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
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/rip"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The Promote API's gates and refusals. Planning and running a Promote need the real database
// (Organize's plan); the e2e suite covers them (subsonic_promote_test.go).
var _ = Describe("Promote API", func() {
	var (
		ds              *tests.MockDataStore
		admin, listener model.User
		inbox           string
		job             *model.RipJob
		queue           *editjob.Queue
	)

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.Ripping.Enabled = true
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
		_, err := core.EnsureInboxLibrary(GinkgoT().Context(), ds, inbox)
		Expect(err).ToNot(HaveOccurred())
		conf.Server.Inbox.Path = inbox
		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Name: "Music", Writable: true}}})
		DeferCleanup(func() { core.SetLibraryEditing(core.LibraryEditing{}) })
		queue = editjob.New(ds, nil, nil)

		admin = model.User{ID: "admin-1", UserName: "admin", IsAdmin: true, NewPassword: "pw"}
		listener = model.User{ID: "user-1", UserName: "listener", NewPassword: "pw"}
		Expect(users.Put(&admin)).To(Succeed())
		Expect(users.Put(&listener)).To(Succeed())

		Expect(os.Mkdir(filepath.Join(inbox, "Album"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(inbox, "Album", "a.opus"), []byte("audio"), 0o644)).To(Succeed())
		ctx := GinkgoT().Context()
		job = &model.RipJob{URL: "https://music.youtube.com/playlist?list=x", Status: model.RipReady, Folder: "Album"}
		Expect(ds.RipJob(ctx).Put(job)).To(Succeed())
		it := model.RipItem{JobID: job.ID, Position: 1, Status: model.RipReady, Path: "Album/a.opus"}
		Expect(ds.RipItem(ctx).Put(&it)).To(Succeed())
	})

	send := func(q *editjob.Queue, user *model.User, path, body string) *httptest.ResponseRecorder {
		ripper := rip.New(ds, &fakeFetcher{}, nil, rip.Options{})
		if q != nil {
			ripper.SetEditQueue(q)
		}
		router := server.JWTVerifier(New(ds, ripper))
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
		if user != nil {
			token, err := auth.CreateToken(user)
			Expect(err).ToNot(HaveOccurred())
			req.Header.Set(consts.UIAuthorizationHeader, "Bearer "+token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	code := func(w *httptest.ResponseRecorder) string {
		GinkgoHelper()
		var res map[string]string
		Expect(json.Unmarshal(w.Body.Bytes(), &res)).To(Succeed(), w.Body.String())
		return res["error"]
	}

	It("doesn't let a Listener promote, nor plan", func() {
		Expect(send(queue, &listener, "/"+job.ID+"/promote/plan", "").Code).To(Equal(http.StatusForbidden))
		Expect(send(queue, &listener, "/"+job.ID+"/promote", `{"token":"x"}`).Code).To(Equal(http.StatusForbidden))
		Expect(send(queue, nil, "/"+job.ID+"/promote", `{"token":"x"}`).Code).To(Equal(http.StatusUnauthorized))
	})

	It("isn't there while the Library is read-only, or there is no Inbox", func() {
		Expect(send(nil, &admin, "/"+job.ID+"/promote/plan", "").Code).To(Equal(http.StatusNotFound))
		Expect(send(nil, &admin, "/"+job.ID+"/promote", `{"token":"x"}`).Code).To(Equal(http.StatusNotFound))
		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Writable: false}}})
		Expect(send(queue, &admin, "/"+job.ID+"/promote/plan", "").Code).To(Equal(http.StatusNotFound))
		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Writable: true}}})
		conf.Server.Inbox.Path = ""
		Expect(send(queue, &admin, "/"+job.ID+"/promote/plan", "").Code).To(Equal(http.StatusNotFound))
	})

	It("works with ripping off: uploads and dropped Files are promoted too (#35)", func() {
		conf.Server.Ripping.Enabled = false
		w := send(queue, &admin, "/"+job.ID+"/promote/plan", "")
		// No Track was scanned in the Inbox for the File: nothing to promote yet.
		Expect(w.Code).To(Equal(http.StatusConflict), w.Body.String())
		Expect(code(w)).To(Equal("nothingToPromote"))
	})

	It("refuses while the job is queued or downloading", func() {
		for _, status := range []model.RipStatus{model.RipQueued, model.RipDownloading} {
			job.Status = status
			Expect(ds.RipJob(GinkgoT().Context()).Put(job)).To(Succeed())
			w := send(queue, &admin, "/"+job.ID+"/promote/plan", "")
			Expect(w.Code).To(Equal(http.StatusConflict))
			Expect(code(w)).To(Equal("notReady"))
		}
	})

	It("answers 404 for a job that isn't there", func() {
		w := send(queue, &admin, "/no-such-job/promote/plan", "")
		Expect(w.Code).To(Equal(http.StatusNotFound))
		Expect(code(w)).To(Equal("notFound"))
	})

	It("needs the plan's token to run it", func() {
		w := send(queue, &admin, "/"+job.ID+"/promote", "")
		Expect(w.Code).To(Equal(http.StatusBadRequest))
		Expect(code(w)).To(Equal("noToken"))
		Expect(filepath.Join(inbox, "Album", "a.opus")).To(BeAnExistingFile())
	})
})
