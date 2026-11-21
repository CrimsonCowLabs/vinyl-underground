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

var _ = Describe("Rip review API", func() {
	var (
		ds       *tests.MockDataStore
		router   http.Handler
		admin    model.User
		listener model.User
		inbox    string
		job      *model.RipJob
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

		admin = model.User{ID: "admin-1", UserName: "admin", IsAdmin: true, NewPassword: "pw"}
		listener = model.User{ID: "user-1", UserName: "listener", NewPassword: "pw"}
		Expect(users.Put(&admin)).To(Succeed())
		Expect(users.Put(&listener)).To(Succeed())

		// A ready album of two Tracks, from the Rip fixtures.
		Expect(os.Mkdir(filepath.Join(inbox, "Album"), 0o755)).To(Succeed())
		for _, name := range []string{"ripped.opus", "ripped.mp3"} {
			data, err := os.ReadFile(filepath.Join("tests", "fixtures", "rips", name))
			Expect(err).ToNot(HaveOccurred())
			Expect(os.WriteFile(filepath.Join(inbox, "Album", name), data, 0o644)).To(Succeed())
		}
		ctx := GinkgoT().Context()
		job = &model.RipJob{URL: "https://music.youtube.com/playlist?list=x", Status: model.RipReady, Folder: "Album"}
		Expect(ds.RipJob(ctx).Put(job)).To(Succeed())
		for i, name := range []string{"ripped.opus", "ripped.mp3"} {
			it := model.RipItem{JobID: job.ID, Position: i + 1, Status: model.RipReady, Path: "Album/" + name}
			Expect(ds.RipItem(ctx).Put(&it)).To(Succeed())
			job.Items = append(job.Items, it)
		}

		router = server.JWTVerifier(New(ds, rip.New(ds, &fakeFetcher{}, nil, rip.Options{})))
	})

	send := func(user *model.User, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if user != nil {
			token, err := auth.CreateToken(user)
			Expect(err).ToNot(HaveOccurred())
			req.Header.Set(consts.UIAuthorizationHeader, "Bearer "+token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	inInbox := func(rel string) bool {
		_, err := os.Stat(filepath.Join(inbox, rel))
		return err == nil
	}

	It("shows the admin a job's review card, with the tags from its Files", func() {
		w := send(&admin, http.MethodGet, "/"+job.ID+"/tags", "")
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var rev rip.Review
		Expect(json.Unmarshal(w.Body.Bytes(), &rev)).To(Succeed())
		Expect(rev.Album).To(Equal("Rip Album"))
		Expect(rev.Tracks).To(HaveLen(2))
		Expect(rev.Tracks[1].Title).To(Equal("Ripped MP3"))
		Expect(rev.Tracks[1].ItemID).To(Equal(job.Items[1].ID))
	})

	It("saves the admin's edits into the Files", func() {
		body := `{"album":"New Album","tracks":[{"itemId":"` + job.Items[0].ID + `","title":"New Title"}]}`
		w := send(&admin, http.MethodPut, "/"+job.ID+"/tags", body)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var rev rip.Review
		Expect(json.Unmarshal(w.Body.Bytes(), &rev)).To(Succeed())
		Expect(rev.Album).To(Equal("New Album"))
		Expect(rev.Tracks[0].Title).To(Equal("New Title"))
		Expect(rev.Tracks[1].Title).To(Equal("Ripped MP3"))
		Expect(rev.Tracks[1].Album).To(Equal("New Album"))
	})

	It("says what is wrong with an edit", func() {
		w := send(&admin, http.MethodPut, "/"+job.ID+"/tags", `{"year":"soon"}`)
		Expect(w.Code).To(Equal(http.StatusBadRequest))
		Expect(w.Body.String()).To(ContainSubstring("year"))
		Expect(send(&admin, http.MethodPut, "/"+job.ID+"/tags", `not json`).Code).To(Equal(http.StatusBadRequest))
	})

	It("discards one Track, then the whole job", func() {
		w := send(&admin, http.MethodDelete, "/"+job.ID+"/items/"+job.Items[0].ID, "")
		Expect(w.Code).To(Equal(http.StatusNoContent), w.Body.String())
		Expect(inInbox("Album/ripped.opus")).To(BeFalse())
		Expect(inInbox("Album/ripped.mp3")).To(BeTrue())

		w = send(&admin, http.MethodDelete, "/"+job.ID, "")
		Expect(w.Code).To(Equal(http.StatusNoContent), w.Body.String())
		Expect(inInbox("Album")).To(BeFalse())
		Expect(send(&admin, http.MethodGet, "/"+job.ID+"/tags", "").Code).To(Equal(http.StatusNotFound))
		Expect(send(&admin, http.MethodDelete, "/"+job.ID, "").Code).To(Equal(http.StatusNotFound))
	})

	It("answers 404 for a Track of another job", func() {
		Expect(send(&admin, http.MethodDelete, "/"+job.ID+"/items/nope", "").Code).To(Equal(http.StatusNotFound))
		Expect(inInbox("Album/ripped.opus")).To(BeTrue())
	})

	It("refuses edits and discards while the job is still running", func() {
		job.Status = model.RipDownloading
		Expect(ds.RipJob(GinkgoT().Context()).Put(job)).To(Succeed())

		Expect(send(&admin, http.MethodPut, "/"+job.ID+"/tags", `{"album":"x"}`).Code).To(Equal(http.StatusConflict))
		Expect(send(&admin, http.MethodDelete, "/"+job.ID, "").Code).To(Equal(http.StatusConflict))
		Expect(send(&admin, http.MethodDelete, "/"+job.ID+"/items/"+job.Items[0].ID, "").Code).To(Equal(http.StatusConflict))
		Expect(inInbox("Album/ripped.opus")).To(BeTrue())
	})

	It("doesn't let a Listener review or discard", func() {
		Expect(send(&listener, http.MethodGet, "/"+job.ID+"/tags", "").Code).To(Equal(http.StatusForbidden))
		Expect(send(&listener, http.MethodPut, "/"+job.ID+"/tags", `{"album":"x"}`).Code).To(Equal(http.StatusForbidden))
		Expect(send(&listener, http.MethodDelete, "/"+job.ID, "").Code).To(Equal(http.StatusForbidden))
		Expect(send(&listener, http.MethodDelete, "/"+job.ID+"/items/"+job.Items[0].ID, "").Code).To(Equal(http.StatusForbidden))
		Expect(send(nil, http.MethodDelete, "/"+job.ID, "").Code).To(Equal(http.StatusUnauthorized))
		Expect(inInbox("Album/ripped.opus")).To(BeTrue())
	})

	It("exposes nothing when ripping is off", func() {
		conf.Server.Ripping.Enabled = false
		Expect(send(&admin, http.MethodGet, "/"+job.ID+"/tags", "").Code).To(Equal(http.StatusNotFound))
		Expect(send(&admin, http.MethodDelete, "/"+job.ID, "").Code).To(Equal(http.StatusNotFound))
		Expect(inInbox("Album/ripped.opus")).To(BeTrue())
	})
})
