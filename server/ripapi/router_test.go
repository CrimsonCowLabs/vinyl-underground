package ripapi

import (
	"bytes"
	"context"
	"errors"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
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
		updater  *fakeUpdater
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

		updater = &fakeUpdater{}
		ripper := rip.New(ds, nil, nil, rip.Options{})
		router = server.JWTVerifier(New(ds, ripper))
	})

	send := func(user *model.User, method, path, body string) *httptest.ResponseRecorder {
		var req *http.Request
		if body != "" {
			req = httptest.NewRequest(method, path, bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
		} else {
			req = httptest.NewRequest(method, path, nil)
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
	do := func(user *model.User, method, body string) *httptest.ResponseRecorder {
		return send(user, method, "/", body)
	}
	doPath := func(user *model.User, method, path string) *httptest.ResponseRecorder {
		return send(user, method, path, "")
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

	Describe("yt-dlp", func() {
		BeforeEach(func() {
			ripper := rip.New(ds, nil, nil, rip.Options{Updater: updater})
			router = server.JWTVerifier(New(ds, ripper))
		})

		It("shows the admin the installed version", func() {
			w := doPath(&admin, http.MethodGet, "/ytdlp")
			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(w.Body.String()).To(MatchJSON(`{"version":"2026.01.01"}`))
		})

		It("updates yt-dlp for the admin and says what changed", func() {
			w := doPath(&admin, http.MethodPost, "/ytdlp/update")
			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(w.Body.String()).To(MatchJSON(`{"previous":"2026.01.01","version":"2026.11.01","updated":true}`))
			Expect(updater.updates()).To(Equal(1))
		})

		It("reports a failed update, and the version kept", func() {
			updater.err = errors.New("GitHub is down")
			w := doPath(&admin, http.MethodPost, "/ytdlp/update")
			Expect(w.Code).To(Equal(http.StatusBadGateway))
			Expect(w.Body.String()).To(ContainSubstring("GitHub is down"))
			Expect(w.Body.String()).To(ContainSubstring("2026.01.01"))
		})

		It("refuses to update while yt-dlp is busy", func() {
			updater.block = make(chan struct{})
			first := make(chan int)
			go func() {
				defer GinkgoRecover()
				first <- doPath(&admin, http.MethodPost, "/ytdlp/update").Code
			}()
			Eventually(updater.updates).Should(Equal(1))
			Expect(doPath(&admin, http.MethodPost, "/ytdlp/update").Code).To(Equal(http.StatusConflict))
			close(updater.block)
			Eventually(first).Should(Receive(Equal(http.StatusOK)))
		})

		It("doesn't let a Listener see or update yt-dlp", func() {
			Expect(doPath(&listener, http.MethodGet, "/ytdlp").Code).To(Equal(http.StatusForbidden))
			Expect(doPath(&listener, http.MethodPost, "/ytdlp/update").Code).To(Equal(http.StatusForbidden))
			Expect(updater.updates()).To(BeZero())
		})

		It("says so when yt-dlp can't be updated on this platform", func() {
			router = server.JWTVerifier(New(ds, rip.New(ds, nil, nil, rip.Options{})))
			Expect(doPath(&admin, http.MethodGet, "/ytdlp").Code).To(Equal(http.StatusNotImplemented))
			w := doPath(&admin, http.MethodPost, "/ytdlp/update")
			Expect(w.Code).To(Equal(http.StatusNotImplemented))
			Expect(w.Body.String()).To(ContainSubstring("no standalone yt-dlp build"))
		})

		It("exposes nothing when ripping is off", func() {
			conf.Server.Ripping.Enabled = false
			Expect(doPath(&admin, http.MethodGet, "/ytdlp").Code).To(Equal(http.StatusNotFound))
			Expect(doPath(&admin, http.MethodPost, "/ytdlp/update").Code).To(Equal(http.StatusNotFound))
			Expect(updater.updates()).To(BeZero())
		})
	})
})

// fakeUpdater stands in for yt-dlp's updates. When block is set, an update waits for it to close.
type fakeUpdater struct {
	mu    sync.Mutex
	calls int
	err   error
	block chan struct{}
}

func (u *fakeUpdater) Update(context.Context) (rip.UpdateResult, error) {
	u.mu.Lock()
	u.calls++
	block, err := u.block, u.err
	u.mu.Unlock()
	if block != nil {
		<-block
	}
	if err != nil {
		return rip.UpdateResult{Previous: "2026.01.01", Version: "2026.01.01"}, err
	}
	return rip.UpdateResult{Previous: "2026.01.01", Version: "2026.11.01", Updated: true}, nil
}

func (u *fakeUpdater) Version(context.Context) (string, error) { return "2026.01.01", nil }

func (u *fakeUpdater) updates() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.calls
}
