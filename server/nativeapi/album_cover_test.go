package nativeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakeCoverPicker struct {
	candidates map[artwork.CoverSource]*artwork.CoverCandidates
	err        error
	stored     []string
	preview    string // when set, what every preview answers
}

func (f *fakeCoverPicker) Candidates(_ context.Context, albumID string, source artwork.CoverSource) (*artwork.CoverCandidates, error) {
	if f.err != nil {
		return nil, f.err
	}
	if c, ok := f.candidates[source]; ok && albumID == "al-1" {
		return c, nil
	}
	return nil, model.ErrNotFound
}

func (f *fakeCoverPicker) Preview(_ context.Context, albumID, candidateID string) (io.ReadCloser, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.preview != "" {
		return io.NopCloser(strings.NewReader(f.preview)), nil
	}
	return io.NopCloser(strings.NewReader("\x89PNG\r\n\x1a\n" + albumID + "/" + candidateID)), nil
}

func (f *fakeCoverPicker) Store(_ context.Context, albumID, candidateID string) (artwork.StoredCover, artwork.CoverCandidate, error) {
	if f.err != nil {
		return artwork.StoredCover{}, artwork.CoverCandidate{}, f.err
	}
	f.stored = append(f.stored, albumID+"/"+candidateID)
	return artwork.StoredCover{Hash: "0123456789abcdef", Mime: "image/png"},
		artwork.CoverCandidate{ID: candidateID, Source: "folder", Label: "cover.png"}, nil
}

var _ = Describe("Album cover API", func() {
	var svc *fakeCorrections
	var picker *fakeCoverPicker
	var router http.Handler
	var adminToken, userToken string

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.EnableSharing = false
		ds := &tests.MockDataStore{}
		auth.Init(ds)
		picker = &fakeCoverPicker{candidates: map[artwork.CoverSource]*artwork.CoverCandidates{
			artwork.CoverSourceLocal:           {Candidates: []artwork.CoverCandidate{{ID: "cand-1", Source: "folder", Label: "cover.png"}}},
			artwork.CoverSourceCoverArtArchive: {Candidates: []artwork.CoverCandidate{}, Failed: true},
		}}
		nativeRouter := New(ds, nil, nil, nil, tests.NewMockLibraryService(), tests.NewMockUserService(), nil, nil, nil, nil, nil, nil, picker)
		svc = &fakeCorrections{}
		nativeRouter.corrections = svc
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

	do := func(method, path, token string, body any) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		if body != nil {
			Expect(json.NewEncoder(&buf).Encode(body)).To(Succeed())
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, createAuthenticatedRequest(method, path, &buf, token))
		return w
	}

	It("lists one source's candidates", func() {
		w := do("GET", "/albumCover/al-1/candidates?source=local", adminToken, nil)
		Expect(w.Code).To(Equal(http.StatusOK))
		var got artwork.CoverCandidates
		Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
		Expect(got.Candidates).To(HaveLen(1))
		Expect(got.Candidates[0].Label).To(Equal("cover.png"))

		w = do("GET", "/albumCover/al-1/candidates?source=coverartarchive", adminToken, nil)
		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(w.Body.String()).To(ContainSubstring(`"failed":true`))
	})

	It("serves a candidate's preview", func() {
		w := do("GET", "/albumCover/al-1/candidates/cand-1/preview", adminToken, nil)
		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(w.Header().Get("Content-Type")).To(Equal("image/png"))
		Expect(w.Header().Get("X-Content-Type-Options")).To(Equal("nosniff"))
		Expect(w.Body.String()).To(HaveSuffix("al-1/cand-1"))
	})

	It("never serves a preview that isn't an image", func() {
		picker.preview = "<html><script>alert(1)</script></html>"
		w := do("GET", "/albumCover/al-1/candidates/cand-1/preview", adminToken, nil)
		Expect(w.Code).To(Equal(http.StatusBadGateway))
		Expect(w.Body.String()).ToNot(ContainSubstring("script"))
	})

	It("chooses a candidate, saving it as a Correction", func() {
		w := do("POST", "/correction/albumCover", adminToken, map[string]string{"albumId": "al-1", "candidateId": "cand-1"})
		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(picker.stored).To(Equal([]string{"al-1/cand-1"}))
		Expect(svc.covers).To(Equal([]string{"al-1->0123456789abcdef Folder image: cover.png"}))
	})

	DescribeTable("answers each failure with a status and a code the UI can show",
		func(err error, status int, code string) {
			picker.err = err
			w := do("POST", "/correction/albumCover", adminToken, map[string]string{"albumId": "al-1", "candidateId": "cand-1"})
			Expect(w.Code).To(Equal(status))
			var res struct{ Error string }
			Expect(json.Unmarshal(w.Body.Bytes(), &res)).To(Succeed())
			Expect(res.Error).To(Equal(code))
			Expect(svc.covers).To(BeEmpty())
		},
		Entry("candidate forgotten", model.ErrNotFound, http.StatusNotFound, "notFound"),
		Entry("not an image", model.ErrValidation, http.StatusBadRequest, "invalid"),
		Entry("source unreachable", errors.New("connection refused"), http.StatusBadGateway, "unavailable"),
	)

	It("denies access to regular users", func() {
		Expect(do("GET", "/albumCover/al-1/candidates?source=local", userToken, nil).Code).To(Equal(http.StatusForbidden))
		Expect(do("GET", "/albumCover/al-1/candidates/cand-1/preview", userToken, nil).Code).To(Equal(http.StatusForbidden))
		Expect(do("POST", "/correction/albumCover", userToken, map[string]string{"albumId": "al-1", "candidateId": "cand-1"}).Code).
			To(Equal(http.StatusForbidden))
		Expect(picker.stored).To(BeEmpty())
	})
})
