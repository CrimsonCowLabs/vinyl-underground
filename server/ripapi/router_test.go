package ripapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

// fakeFetcher lists a playlist of n items at any URL with "list=" in it, and a single video
// anywhere else. Nothing is ever downloaded: the API only queues.
type fakeFetcher struct{ n int }

func (f *fakeFetcher) Probe(_ context.Context, url string) (*rip.Info, error) {
	if !strings.Contains(url, "list=") {
		return &rip.Info{ID: "abc", Title: "Song", Artist: "Artist", Source: "YouTube", SourceURL: url, Count: 1}, nil
	}
	info := &rip.Info{ID: "PL", Title: "Big Playlist", Source: "YouTube", SourceURL: url, IsPlaylist: true, Count: f.n}
	for i := 1; i <= min(f.n, rip.MaxItems+1); i++ {
		info.Entries = append(info.Entries, rip.PlaylistEntry{URL: fmt.Sprintf("https://www.youtube.com/watch?v=v%d", i), Title: fmt.Sprintf("Song %d", i), Artist: "Band"})
	}
	return info, nil
}

func (f *fakeFetcher) Download(context.Context, string, string) (string, error) {
	return "", fmt.Errorf("not in this test")
}

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
		fetcher  *fakeFetcher
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

		fetcher = &fakeFetcher{n: 3}
		ripper := rip.New(ds, fetcher, nil, rip.Options{})
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

	It("queues a playlist as one job, and lists its items with it", func() {
		w := do(&admin, http.MethodPost, `{"url":"https://www.youtube.com/playlist?list=PL"}`)
		Expect(w.Code).To(Equal(http.StatusCreated))

		var jobs []model.RipJob
		Expect(json.Unmarshal(do(&admin, http.MethodGet, "").Body.Bytes(), &jobs)).To(Succeed())
		Expect(jobs).To(HaveLen(1))
		Expect(jobs[0].Title).To(Equal("Big Playlist"))
		Expect(jobs[0].Items).To(HaveLen(3))
		Expect(jobs[0].Items[2].Title).To(Equal("Song 3"))
		Expect(jobs[0].Items[2].Status).To(Equal(model.RipQueued))
	})

	It("asks for confirmation before queuing more than 50 items", func() {
		fetcher.n = 51
		w := do(&admin, http.MethodPost, `{"url":"https://www.youtube.com/playlist?list=PL"}`)
		Expect(w.Code).To(Equal(http.StatusConflict))
		var body struct {
			Error             string   `json:"error"`
			NeedsConfirmation bool     `json:"needsConfirmation"`
			Count             int      `json:"count"`
			Title             string   `json:"title"`
			Titles            []string `json:"titles"`
		}
		Expect(json.Unmarshal(w.Body.Bytes(), &body)).To(Succeed())
		Expect(body.NeedsConfirmation).To(BeTrue())
		Expect(body.Count).To(Equal(51))
		Expect(body.Title).To(Equal("Big Playlist"))
		Expect(body.Titles).To(HaveLen(51))
		Expect(body.Titles[0]).To(Equal("Band - Song 1"))
		Expect(body.Error).To(ContainSubstring("51"))
		Expect(do(&admin, http.MethodGet, "").Body.String()).To(MatchJSON(`[]`), "nothing is queued yet")

		w = do(&admin, http.MethodPost, `{"url":"https://www.youtube.com/playlist?list=PL","confirm":true}`)
		Expect(w.Code).To(Equal(http.StatusCreated))
		var job model.RipJob
		Expect(json.Unmarshal(w.Body.Bytes(), &job)).To(Succeed())
		Expect(job.Items).To(HaveLen(51))
	})

	It("refuses more than 300 items with a clear message", func() {
		fetcher.n = 301
		w := do(&admin, http.MethodPost, `{"url":"https://www.youtube.com/playlist?list=PL","confirm":true}`)
		Expect(w.Code).To(Equal(http.StatusUnprocessableEntity))
		Expect(w.Body.String()).To(ContainSubstring("at most 300"))
		Expect(do(&admin, http.MethodGet, "").Body.String()).To(MatchJSON(`[]`))
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
