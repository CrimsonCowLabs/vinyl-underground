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
	"github.com/navidrome/navidrome/core/rip"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Promote API", func() {
	var (
		ds              *tests.MockDataStore
		admin, listener model.User
		inbox, promote  string
		job             *model.RipJob
	)

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.Ripping.Enabled = true
		conf.Server.SessionTimeout = time.Minute

		music := GinkgoT().TempDir()
		promote = filepath.Join(music, "Vinyl Underground")
		Expect(os.Mkdir(promote, 0o755)).To(Succeed())
		users := tests.CreateMockUserRepo()
		libs := &tests.MockLibraryRepo{}
		libs.SetData(model.Libraries{{ID: 1, Name: "Music", Path: music}})
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

		admin = model.User{ID: "admin-1", UserName: "admin", IsAdmin: true, NewPassword: "pw"}
		listener = model.User{ID: "user-1", UserName: "listener", NewPassword: "pw"}
		Expect(users.Put(&admin)).To(Succeed())
		Expect(users.Put(&listener)).To(Succeed())

		Expect(os.Mkdir(filepath.Join(inbox, "Album"), 0o755)).To(Succeed())
		data, err := os.ReadFile(filepath.Join("tests", "fixtures", "rips", "ripped.opus"))
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(inbox, "Album", "a.opus"), data, 0o644)).To(Succeed())
		ctx := GinkgoT().Context()
		job = &model.RipJob{URL: "https://music.youtube.com/playlist?list=x", Status: model.RipReady, Folder: "Album"}
		Expect(ds.RipJob(ctx).Put(job)).To(Succeed())
		it := model.RipItem{JobID: job.ID, Position: 1, Status: model.RipReady, Path: "Album/a.opus"}
		Expect(ds.RipItem(ctx).Put(&it)).To(Succeed())
		job.Items = append(job.Items, it)
	})

	send := func(opts rip.Options, user *model.User, path string) *httptest.ResponseRecorder {
		router := server.JWTVerifier(New(ds, rip.New(ds, &fakeFetcher{}, nil, opts)))
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(""))
		if user != nil {
			token, err := auth.CreateToken(user)
			Expect(err).ToNot(HaveOccurred())
			req.Header.Set(consts.UIAuthorizationHeader, "Bearer "+token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	on := func() rip.Options { return rip.Options{PromoteRoot: promote} }
	inInbox := func() bool {
		_, err := os.Stat(filepath.Join(inbox, "Album", "a.opus"))
		return err == nil
	}

	It("promotes the job's Files and says where each went", func() {
		w := send(on(), &admin, "/"+job.ID+"/promote")
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var res rip.PromoteResult
		Expect(json.Unmarshal(w.Body.Bytes(), &res)).To(Succeed())
		Expect(res.Promoted).To(ConsistOf(rip.PromotedFile{ItemID: job.Items[0].ID,
			Path: "Vinyl Underground/Rip Artist/Rip Album/Ripped Opus.opus"}))
		Expect(res.Failed).To(BeEmpty())
		Expect(res.Review).To(BeNil())
		Expect(inInbox()).To(BeFalse())
		Expect(filepath.Join(promote, "Rip Artist", "Rip Album", "Ripped Opus.opus")).To(BeAnExistingFile())
		Expect(send(on(), &admin, "/"+job.ID+"/promote").Code).To(Equal(http.StatusNotFound))
	})

	It("doesn't let a Listener promote", func() {
		Expect(send(on(), &listener, "/"+job.ID+"/promote").Code).To(Equal(http.StatusForbidden))
		Expect(send(on(), nil, "/"+job.ID+"/promote").Code).To(Equal(http.StatusUnauthorized))
		Expect(inInbox()).To(BeTrue())
	})

	It("refuses while the job is queued or downloading", func() {
		for _, status := range []model.RipStatus{model.RipQueued, model.RipDownloading} {
			job.Status = status
			Expect(ds.RipJob(GinkgoT().Context()).Put(job)).To(Succeed())
			Expect(send(on(), &admin, "/"+job.ID+"/promote").Code).To(Equal(http.StatusConflict))
		}
		Expect(inInbox()).To(BeTrue())
	})

	It("refuses a job with no Files to promote", func() {
		failed := &model.RipJob{URL: "https://www.youtube.com/watch?v=x", Status: model.RipFailed}
		Expect(ds.RipJob(GinkgoT().Context()).Put(failed)).To(Succeed())
		Expect(send(on(), &admin, "/"+failed.ID+"/promote").Code).To(Equal(http.StatusConflict))
	})

	It("isn't there when Promote is off, or ripping is", func() {
		Expect(send(rip.Options{}, &admin, "/"+job.ID+"/promote").Code).To(Equal(http.StatusNotFound))
		conf.Server.Ripping.Enabled = false
		Expect(send(on(), &admin, "/"+job.ID+"/promote").Code).To(Equal(http.StatusNotFound))
		Expect(inInbox()).To(BeTrue())
	})
})
