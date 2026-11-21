package nativeapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/stations"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeStations stands in for the Station Service's refills and skips. DJs are tested against
// the real Service (see "DJ endpoints").
type fakeStations struct {
	stations.Service
	err      error
	gotDJ    string
	gotTheme string
	gotCount int
	gotUser  string
	gotTrack string
}

func (f *fakeStations) Skip(ctx context.Context, djID, trackID string) error {
	f.gotDJ, f.gotTrack = djID, trackID
	usr, _ := request.UserFrom(ctx)
	f.gotUser = usr.ID
	return f.err
}

func (f *fakeStations) Next(ctx context.Context, djID, theme string, count int) (model.MediaFiles, error) {
	f.gotDJ, f.gotTheme, f.gotCount = djID, theme, count
	usr, _ := request.UserFrom(ctx)
	f.gotUser = usr.ID
	if f.err != nil {
		return nil, f.err
	}
	return model.MediaFiles{{ID: "mf-1", Title: "Blue in Green", Artist: "Miles Davis"}}, nil
}

var _ = Describe("Station endpoints", func() {
	var svc *fakeStations
	alice := model.User{ID: "u1", UserName: "alice"}

	BeforeEach(func() {
		svc = &fakeStations{}
	})

	call := func(handler http.HandlerFunc, method, body string) (int, any) {
		r := httptest.NewRequest(method, "/station", strings.NewReader(body))
		r = r.WithContext(request.WithUser(r.Context(), alice))
		w := httptest.NewRecorder()
		handler(w, r)
		var res any
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		return w.Code, res
	}

	It("serves the Listener's next Station Tracks", func() {
		code, res := call(nextStationTracks(svc), http.MethodPost, `{"dj":"night-owl","count":5}`)
		Expect(code).To(Equal(http.StatusOK))
		Expect(svc.gotDJ).To(Equal("night-owl"))
		Expect(svc.gotCount).To(Equal(5))
		Expect(svc.gotUser).To(Equal("u1"))
		tracks := res.(map[string]any)["tracks"]
		Expect(tracks).To(HaveLen(1))
		Expect(tracks.([]any)[0]).To(HaveKeyWithValue("id", "mf-1"))
	})

	It("passes the Station's Theme on", func() {
		code, _ := call(nextStationTracks(svc), http.MethodPost, `{"dj":"night-owl","theme":"late-night drive","count":5}`)
		Expect(code).To(Equal(http.StatusOK))
		Expect(svc.gotTheme).To(Equal("late-night drive"))
	})

	DescribeTable("maps errors to a status and a code",
		func(err error, body string, status int, code string) {
			svc.err = err
			got, res := call(nextStationTracks(svc), http.MethodPost, body)
			Expect(got).To(Equal(status))
			if code != "" {
				Expect(res).To(HaveKeyWithValue("error", code))
			}
		},
		Entry("unknown DJ", stations.ErrUnknownDJ, `{"dj":"x"}`, http.StatusNotFound, "unknownDJ"),
		Entry("bad count", stations.ErrInvalidCount, `{"dj":"sunny","count":99}`, http.StatusBadRequest, "invalidCount"),
		Entry("Theme too long", stations.ErrInvalidTheme, `{"dj":"sunny","theme":"x"}`, http.StatusBadRequest, "invalidTheme"),
		Entry("empty Library", stations.ErrNoTracks, `{"dj":"sunny"}`, http.StatusUnprocessableEntity, "noTracks"),
		Entry("anything else", errors.New("disk on fire"), `{"dj":"sunny"}`, http.StatusInternalServerError, "internal"),
		Entry("not JSON", nil, `nope`, http.StatusBadRequest, "badRequest"),
	)

	It("records the Listener's skip on their Station", func() {
		code, _ := call(skipStationTrack(svc), http.MethodPost, `{"dj":"sunny","track":"mf-1"}`)
		Expect(code).To(Equal(http.StatusNoContent))
		Expect(svc.gotDJ).To(Equal("sunny"))
		Expect(svc.gotTrack).To(Equal("mf-1"))
		Expect(svc.gotUser).To(Equal("u1"))
	})

	DescribeTable("maps skip errors to a status and a code",
		func(err error, body string, status int, code string) {
			svc.err = err
			got, res := call(skipStationTrack(svc), http.MethodPost, body)
			Expect(got).To(Equal(status))
			Expect(res).To(HaveKeyWithValue("error", code))
		},
		Entry("unknown DJ", stations.ErrUnknownDJ, `{"dj":"x","track":"mf-1"}`, http.StatusNotFound, "unknownDJ"),
		Entry("not served", stations.ErrNotServed, `{"dj":"sunny","track":"mf-9"}`, http.StatusNotFound, "notServed"),
		Entry("anything else", errors.New("disk on fire"), `{"dj":"sunny","track":"mf-1"}`, http.StatusInternalServerError, "internal"),
		Entry("not JSON", nil, `nope`, http.StatusBadRequest, "badRequest"),
	)

	It("needs a logged-in Listener", func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.EnableSharing = false
		ds := &tests.MockDataStore{}
		router := New(ds, nil, nil, nil, tests.NewMockLibraryService(), core.NewUser(ds, noopPluginUnloader{}), nil, nil, nil, nil, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/station/dj", nil))
		Expect(w.Code).To(Equal(http.StatusUnauthorized))
	})
})

var _ = Describe("DJ endpoints", func() {
	var router chi.Router
	alice := model.User{ID: "u1", UserName: "alice"}
	bob := model.User{ID: "u2", UserName: "bob"}

	BeforeEach(func() {
		router = chi.NewRouter()
		stationRoutes(router, stations.New(&tests.MockDataStore{}, nil))
	})

	call := func(as model.User, method, path, body string) (int, map[string]any, []any) {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r = r.WithContext(request.WithUser(r.Context(), as))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		var obj map[string]any
		var list []any
		_ = json.Unmarshal(w.Body.Bytes(), &obj)
		_ = json.Unmarshal(w.Body.Bytes(), &list)
		return w.Code, obj, list
	}

	create := func() string {
		code, dj, _ := call(alice, http.MethodPost, "/station/dj", `{"name":"Mr. Mellow","description":"Slow and soft."}`)
		Expect(code).To(Equal(http.StatusCreated))
		Expect(dj).To(HaveKeyWithValue("name", "Mr. Mellow"))
		Expect(dj).To(HaveKeyWithValue("description", "Slow and soft."))
		Expect(dj).To(HaveKeyWithValue("builtIn", false))
		Expect(dj["id"]).ToNot(BeEmpty())
		return dj["id"].(string)
	}

	It("lists the built-in DJs, read-only, with their names and descriptions", func() {
		code, _, djs := call(alice, http.MethodGet, "/station/dj", "")
		Expect(code).To(Equal(http.StatusOK))
		Expect(djs).To(HaveLen(3))
		first := djs[0].(map[string]any)
		Expect(first).To(HaveKeyWithValue("id", "night-owl"))
		Expect(first).To(HaveKeyWithValue("name", "Night Owl"))
		Expect(first).To(HaveKeyWithValue("builtIn", true))
		Expect(first).To(HaveKey("description"))
		Expect(first).ToNot(HaveKey("Taste"))
	})

	It("creates a Listener's DJ, listed after the built-in ones for that Listener only", func() {
		id := create()
		_, _, djs := call(alice, http.MethodGet, "/station/dj", "")
		Expect(djs).To(HaveLen(4))
		Expect(djs[3]).To(HaveKeyWithValue("id", id))
		_, _, djs = call(bob, http.MethodGet, "/station/dj", "")
		Expect(djs).To(HaveLen(3))
	})

	It("edits and deletes the Listener's own DJ", func() {
		id := create()
		code, dj, _ := call(alice, http.MethodPut, "/station/dj/"+id, `{"name":"Mr. Loud","description":"Fast and loud."}`)
		Expect(code).To(Equal(http.StatusOK))
		Expect(dj).To(HaveKeyWithValue("id", id))
		Expect(dj).To(HaveKeyWithValue("name", "Mr. Loud"))

		code, _, _ = call(alice, http.MethodDelete, "/station/dj/"+id, "")
		Expect(code).To(Equal(http.StatusNoContent))
		_, _, djs := call(alice, http.MethodGet, "/station/dj", "")
		Expect(djs).To(HaveLen(3))
	})

	It("keeps another Listener's DJ out of reach", func() {
		id := create()
		code, res, _ := call(bob, http.MethodPut, "/station/dj/"+id, `{"name":"Hijacked","description":"Mine now."}`)
		Expect(code).To(Equal(http.StatusNotFound))
		Expect(res).To(HaveKeyWithValue("error", "unknownDJ"))
		code, res, _ = call(bob, http.MethodDelete, "/station/dj/"+id, "")
		Expect(code).To(Equal(http.StatusNotFound))
		Expect(res).To(HaveKeyWithValue("error", "unknownDJ"))
	})

	It("refuses to edit or delete a built-in DJ", func() {
		code, res, _ := call(alice, http.MethodPut, "/station/dj/sunny", `{"name":"Gloomy","description":"Sad songs."}`)
		Expect(code).To(Equal(http.StatusForbidden))
		Expect(res).To(HaveKeyWithValue("error", "builtInDJ"))
		code, res, _ = call(alice, http.MethodDelete, "/station/dj/sunny", "")
		Expect(code).To(Equal(http.StatusForbidden))
		Expect(res).To(HaveKeyWithValue("error", "builtInDJ"))
	})

	DescribeTable("rejects a DJ it can't save",
		func(method, body string, status int, code string) {
			path := "/station/dj"
			if method == http.MethodPut {
				path += "/" + create()
			}
			got, res, _ := call(alice, method, path, body)
			Expect(got).To(Equal(status))
			Expect(res).To(HaveKeyWithValue("error", code))
		},
		Entry("no name", http.MethodPost, `{"name":" ","description":"Slow."}`, http.StatusBadRequest, "invalidDJName"),
		Entry("no description", http.MethodPost, `{"name":"Mr. Mellow"}`, http.StatusBadRequest, "invalidDJDescription"),
		Entry("not JSON", http.MethodPost, `nope`, http.StatusBadRequest, "badRequest"),
		Entry("no name, editing", http.MethodPut, `{"name":"","description":"Slow."}`, http.StatusBadRequest, "invalidDJName"),
		Entry("not JSON, editing", http.MethodPut, `nope`, http.StatusBadRequest, "badRequest"),
	)
})
