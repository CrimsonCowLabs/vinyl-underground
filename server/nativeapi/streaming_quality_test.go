package nativeapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/navidrome/navidrome/core/streamquality"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Streaming quality endpoints", func() {
	var (
		quality streamquality.Service
		alice   = model.User{ID: "u1", UserName: "alice"}
		bob     = model.User{ID: "u2", UserName: "bob"}
	)

	BeforeEach(func() {
		quality = streamquality.New(&tests.MockDataStore{})
	})

	as := func(user model.User, r *http.Request) *http.Request {
		return r.WithContext(request.WithUser(r.Context(), user))
	}
	get := func(user model.User) (int, string) {
		w := httptest.NewRecorder()
		getStreamingQuality(quality)(w, as(user, httptest.NewRequest("GET", "/streamingQuality", nil)))
		var body struct {
			Preference string `json:"preference"`
		}
		if w.Code == http.StatusOK {
			Expect(json.Unmarshal(w.Body.Bytes(), &body)).To(Succeed())
		}
		return w.Code, body.Preference
	}
	put := func(user model.User, body string) int {
		w := httptest.NewRecorder()
		setStreamingQuality(quality)(w, as(user, httptest.NewRequest("PUT", "/streamingQuality", strings.NewReader(body))))
		return w.Code
	}

	It("returns auto for a Listener who never chose", func() {
		code, pref := get(alice)
		Expect(code).To(Equal(http.StatusOK))
		Expect(pref).To(Equal("auto"))
	})

	It("saves the Listener's choice", func() {
		Expect(put(alice, `{"preference":"original"}`)).To(Equal(http.StatusOK))
		_, pref := get(alice)
		Expect(pref).To(Equal("original"))
	})

	It("only changes the calling Listener's setting", func() {
		Expect(put(alice, `{"preference":"dataSaver"}`)).To(Equal(http.StatusOK))
		_, pref := get(bob)
		Expect(pref).To(Equal("auto"))
	})

	It("rejects an unknown preference", func() {
		Expect(put(alice, `{"preference":"potato"}`)).To(Equal(http.StatusBadRequest))
		_, pref := get(alice)
		Expect(pref).To(Equal("auto"))
	})

	It("rejects a malformed body", func() {
		Expect(put(alice, `not json`)).To(Equal(http.StatusBadRequest))
	})
})
