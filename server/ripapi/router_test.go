package ripapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "github.com/navidrome/navidrome/adapters/gotaglib"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/core/rip"
	_ "github.com/navidrome/navidrome/core/storage/local"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestRipAPI(t *testing.T) {
	tests.Init(t, false)
	log.SetLevel(log.LevelFatal)
	RegisterFailHandler(Fail)
	RunSpecs(t, "Rip API Suite")
}

var _ = Describe("Rip API", func() {
	var (
		ds       *tests.MockDataStore
		router   http.Handler
		admin    model.User
		listener model.User
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
		_, err := core.EnsureInboxLibrary(GinkgoT().Context(), ds, GinkgoT().TempDir())
		Expect(err).ToNot(HaveOccurred())

		admin = model.User{ID: "admin-1", UserName: "admin", IsAdmin: true, NewPassword: "pw"}
		listener = model.User{ID: "user-1", UserName: "listener", NewPassword: "pw"}
		Expect(users.Put(&admin)).To(Succeed())
		Expect(users.Put(&listener)).To(Succeed())

		ripper := rip.New(ds, nil, nil, rip.Options{})
		router = server.JWTVerifier(New(ds, ripper))
	})

	do := func(user *model.User, method, body string) *httptest.ResponseRecorder {
		var req *http.Request
		if body != "" {
			req = httptest.NewRequest(method, "/", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
		} else {
			req = httptest.NewRequest(method, "/", nil)
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

	It("lets the admin start a Rip and see it in the job list", func() {
		w := do(&admin, http.MethodPost, `{"url":"https://www.youtube.com/watch?v=abc"}`)
		Expect(w.Code).To(Equal(http.StatusCreated))
		var job model.RipJob
		Expect(json.Unmarshal(w.Body.Bytes(), &job)).To(Succeed())
		Expect(job.ID).ToNot(BeEmpty())
		Expect(job.Status).To(Equal(model.RipQueued))

		w = do(&admin, http.MethodGet, "")
		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(w.Header().Get("X-Total-Count")).To(Equal("1"))
		var jobs []model.RipJob
		Expect(json.Unmarshal(w.Body.Bytes(), &jobs)).To(Succeed())
		Expect(jobs).To(HaveLen(1))
		Expect(jobs[0].URL).To(Equal("https://www.youtube.com/watch?v=abc"))
	})

	It("returns an empty list, not null, when there are no jobs", func() {
		w := do(&admin, http.MethodGet, "")
		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(w.Body.String()).To(MatchJSON(`[]`))
	})

	It("rejects a URL that isn't http(s)", func() {
		w := do(&admin, http.MethodPost, `{"url":"file:///etc/passwd"}`)
		Expect(w.Code).To(Equal(http.StatusBadRequest))
		Expect(w.Body.String()).To(ContainSubstring("http(s)"))
	})

	It("rejects a body that isn't JSON", func() {
		w := do(&admin, http.MethodPost, `url=https://example.com`)
		Expect(w.Code).To(Equal(http.StatusBadRequest))
	})

	It("doesn't let a Listener start a Rip or see the jobs", func() {
		Expect(do(&listener, http.MethodPost, `{"url":"https://www.youtube.com/watch?v=abc"}`).Code).To(Equal(http.StatusForbidden))
		Expect(do(&listener, http.MethodGet, "").Code).To(Equal(http.StatusForbidden))
		Expect(do(&admin, http.MethodGet, "").Body.String()).To(MatchJSON(`[]`))
	})

	It("requires a login", func() {
		Expect(do(nil, http.MethodGet, "").Code).To(Equal(http.StatusUnauthorized))
		Expect(do(nil, http.MethodPost, `{"url":"https://www.youtube.com/watch?v=abc"}`).Code).To(Equal(http.StatusUnauthorized))
	})

	It("exposes nothing when ripping is off", func() {
		conf.Server.Ripping.Enabled = false
		Expect(do(&admin, http.MethodGet, "").Code).To(Equal(http.StatusNotFound))
		Expect(do(&admin, http.MethodPost, `{"url":"https://www.youtube.com/watch?v=abc"}`).Code).To(Equal(http.StatusNotFound))
	})
})
