package nativeapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/core/refresh"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/scanner"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakeRefresher struct {
	refresh.Refresher
	err    error
	called []string
}

func (f *fakeRefresher) Refresh(_ context.Context, kind model.Kind, id string) error {
	f.called = append(f.called, kind.Prefix()+"/"+id)
	return f.err
}

var _ = Describe("Metadata API", func() {
	var ds *tests.MockDataStore
	var refresher *fakeRefresher
	var router http.Handler
	var adminToken, userToken string

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.EnableSharing = false
		ds = &tests.MockDataStore{}
		auth.Init(ds)
		refresher = &fakeRefresher{}
		nativeRouter := New(ds, nil, nil, nil, tests.NewMockLibraryService(), tests.NewMockUserService(), nil, nil, nil, nil, nil, refresher, nil)
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

	post := func(path, token string) int {
		req := createAuthenticatedRequest("POST", path, nil, token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w.Code
	}

	Describe("POST /api/metadata/{kind}/{id}/refresh", func() {
		It("rescans and refreshes an album, answering once that is done", func() {
			Expect(post("/metadata/al/al-1/refresh", adminToken)).To(Equal(http.StatusNoContent))
			Expect(refresher.called).To(Equal([]string{"al/al-1"}))
		})

		It("rescans and refreshes an Artist", func() {
			Expect(post("/metadata/ar/ar-1/refresh", adminToken)).To(Equal(http.StatusNoContent))
			Expect(refresher.called).To(Equal([]string{"ar/ar-1"}))
		})

		It("returns 400 for an invalid kind", func() {
			Expect(post("/metadata/xx/id-1/refresh", adminToken)).To(Equal(http.StatusBadRequest))
			Expect(refresher.called).To(BeEmpty())
		})

		It("denies access to regular users", func() {
			Expect(post("/metadata/al/al-1/refresh", userToken)).To(Equal(http.StatusForbidden))
			Expect(refresher.called).To(BeEmpty())
		})

		It("denies access without authentication", func() {
			req := createUnauthenticatedRequest("POST", "/metadata/al/al-1/refresh", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusUnauthorized))
			Expect(refresher.called).To(BeEmpty())
		})

		It("returns 404 for an unknown id", func() {
			refresher.err = model.ErrNotFound
			Expect(post("/metadata/al/nope/refresh", adminToken)).To(Equal(http.StatusNotFound))
		})

		It("returns 409 when another scan keeps running", func() {
			refresher.err = fmt.Errorf("scanning folders: %w", scanner.ErrAlreadyScanning)
			Expect(post("/metadata/al/al-1/refresh", adminToken)).To(Equal(http.StatusConflict))
		})

		It("returns 500 when the refresh fails for another reason", func() {
			refresher.err = errors.New("boom")
			Expect(post("/metadata/al/al-1/refresh", adminToken)).To(Equal(http.StatusInternalServerError))
		})
	})
})
