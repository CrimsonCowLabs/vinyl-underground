package nativeapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GET /api/libraryEditing", func() {
	var router http.Handler
	var admin, listener model.User

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.EnableSharing = false
		ds := &tests.MockDataStore{}
		auth.Init(ds)
		router = server.JWTVerifier(New(ds, nil, nil, nil, tests.NewMockLibraryService(), tests.NewMockUserService(), nil, nil, nil, nil, nil, nil, nil))
		admin = model.User{ID: "admin-1", UserName: "admin", IsAdmin: true, NewPassword: "pass"}
		listener = model.User{ID: "user-1", UserName: "listener", NewPassword: "pass"}
		Expect(ds.User(context.TODO()).Put(&admin)).To(Succeed())
		Expect(ds.User(context.TODO()).Put(&listener)).To(Succeed())

		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Name: "Music", Writable: true}}})
		DeferCleanup(core.SetLibraryEditing, core.LibraryEditing{})
	})

	get := func(user model.User) *httptest.ResponseRecorder {
		token, err := auth.CreateToken(&user)
		Expect(err).ToNot(HaveOccurred())
		w := httptest.NewRecorder()
		router.ServeHTTP(w, createAuthenticatedRequest("GET", "/libraryEditing", nil, token))
		return w
	}

	It("tells the admin whether editing is on, and which libraries are writable", func() {
		w := get(admin)
		Expect(w.Code).To(Equal(http.StatusOK))
		var got core.LibraryEditing
		Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
		Expect(got).To(Equal(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Name: "Music", Writable: true}}}))
	})

	It("reports editing off when no Library folder is writable", func() {
		core.SetLibraryEditing(core.LibraryEditing{Libraries: []core.LibraryWritable{{ID: 1, Name: "Music"}}})
		var got core.LibraryEditing
		Expect(json.Unmarshal(get(admin).Body.Bytes(), &got)).To(Succeed())
		Expect(got.Enabled).To(BeFalse())
	})

	It("is admin only: a Listener never sees editing as on", func() {
		w := get(listener)
		Expect(w.Code).To(Equal(http.StatusForbidden))
		Expect(w.Body.String()).ToNot(ContainSubstring("enabled"))
	})
})
