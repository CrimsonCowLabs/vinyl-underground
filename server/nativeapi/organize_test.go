package nativeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Organize's endpoints (#167). The plan and the job themselves are tested in core/organize, and
// end to end, on the real database, in server/subsonic/e2e.
var _ = Describe("Organize API", func() {
	var (
		router          http.Handler
		admin, listener model.User
	)

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.EnableSharing = false
		ds := &tests.MockDataStore{MockedEditJob: &tests.MockEditJobRepo{}}
		auth.Init(ds)
		// No edit queue: editing is off.
		api := New(ds, nil, nil, nil, tests.NewMockLibraryService(), tests.NewMockUserService(),
			nil, nil, nil, nil, nil, nil, nil, nil)
		router = server.JWTVerifier(api)
		admin = model.User{ID: "admin-1", UserName: "admin", IsAdmin: true, NewPassword: "pass"}
		listener = model.User{ID: "user-1", UserName: "listener", NewPassword: "pass"}
		Expect(ds.User(context.TODO()).Put(&admin)).To(Succeed())
		Expect(ds.User(context.TODO()).Put(&listener)).To(Succeed())
	})

	do := func(user model.User, url string, body any) *httptest.ResponseRecorder {
		GinkgoHelper()
		token, err := auth.CreateToken(&user)
		Expect(err).ToNot(HaveOccurred())
		b, err := json.Marshal(body)
		Expect(err).ToNot(HaveOccurred())
		w := httptest.NewRecorder()
		router.ServeHTTP(w, createAuthenticatedRequest(http.MethodPost, url, bytes.NewBuffer(b), token))
		return w
	}
	code := func(w *httptest.ResponseRecorder) string {
		GinkgoHelper()
		var res map[string]string
		Expect(json.Unmarshal(w.Body.Bytes(), &res)).To(Succeed())
		return res["error"]
	}

	It("is admin only", func() {
		Expect(do(listener, "/organize/plan", map[string]any{"albumId": "al-1"}).Code).To(Equal(http.StatusForbidden))
		Expect(do(listener, "/organize/run", map[string]any{"albumId": "al-1"}).Code).To(Equal(http.StatusForbidden))
	})

	It("needs a scope", func() {
		w := do(admin, "/organize/plan", map[string]any{})
		Expect(w.Code).To(Equal(http.StatusBadRequest))
		Expect(code(w)).To(Equal("noSelection"))
	})

	It("runs nothing while editing is off", func() {
		w := do(admin, "/organize/run", map[string]any{"albumId": "al-1", "token": "x"})
		Expect(w.Code).To(Equal(http.StatusForbidden))
		Expect(code(w)).To(Equal("editingOff"))
	})
})
