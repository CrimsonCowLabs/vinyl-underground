package nativeapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"

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

type fakeStations struct {
	err      error
	gotDJ    string
	gotCount int
	gotUser  string
}

func (f *fakeStations) DJs() []stations.DJ { return stations.DJs() }

func (f *fakeStations) Next(ctx context.Context, djID string, count int) (model.MediaFiles, error) {
	f.gotDJ, f.gotCount = djID, count
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

	It("lists the DJs with their names and descriptions", func() {
		code, res := call(listDJs(svc), http.MethodGet, "")
		Expect(code).To(Equal(http.StatusOK))
		Expect(res).To(HaveLen(3))
		first := res.([]any)[0].(map[string]any)
		Expect(first).To(HaveKeyWithValue("id", "night-owl"))
		Expect(first).To(HaveKeyWithValue("name", "Night Owl"))
		Expect(first).To(HaveKey("description"))
		Expect(first).ToNot(HaveKey("Taste"))
	})

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
		Entry("empty Library", stations.ErrNoTracks, `{"dj":"sunny"}`, http.StatusUnprocessableEntity, "noTracks"),
		Entry("anything else", errors.New("disk on fire"), `{"dj":"sunny"}`, http.StatusInternalServerError, "internal"),
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
