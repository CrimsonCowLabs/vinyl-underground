package nativeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/core/corrections"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func (f *fakeCorrections) CorrectTrackNumbers(_ context.Context, albumID string, tracks []corrections.TrackNumber) (*corrections.Applied, error) {
	if f.err != nil {
		return nil, f.err
	}
	for _, t := range tracks {
		f.numbers = append(f.numbers, fmt.Sprintf("%s/%s->%d-%d", albumID, t.ID, t.DiscNumber, t.TrackNumber))
	}
	return &corrections.Applied{Correction: model.Correction{ID: "c4"}, AlbumID: albumID, Done: true}, nil
}

var _ = Describe("Track order API", func() {
	var svc *fakeCorrections
	var router http.Handler
	var adminToken, userToken string
	body := map[string]any{"albumId": "al-1", "tracks": []map[string]any{
		{"id": "mf-1", "discNumber": 1, "trackNumber": 2}, {"id": "mf-2", "discNumber": 2, "trackNumber": 1}}}

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.EnableSharing = false
		ds := &tests.MockDataStore{}
		auth.Init(ds)
		nativeRouter := New(ds, nil, nil, nil, tests.NewMockLibraryService(), tests.NewMockUserService(), nil, nil, nil, nil, nil, nil, nil, nil)
		svc = &fakeCorrections{}
		nativeRouter.corrections = svc
		runEditQueue(nativeRouter, ds, fakeWriteKind{})
		router = server.JWTVerifier(nativeRouter)
		ds.MediaFile(context.TODO()).(*tests.MockMediaFileRepo).SetData(model.MediaFiles{
			{ID: "mf-1", AlbumID: "al-1"}, {ID: "mf-2", AlbumID: "al-1"}})

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

	post := func(token string, body any) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		Expect(json.NewEncoder(&buf).Encode(body)).To(Succeed())
		w := httptest.NewRecorder()
		router.ServeHTTP(w, createAuthenticatedRequest("POST", "/correction/trackNumbers", &buf, token))
		return w
	}

	It("writes the Tracks' disc and track numbers into their Files, as one job", func() {
		svc.plan = fakePlan(corrections.JobTrackNumbers, "mf-1", "mf-2")
		w := post(adminToken, body)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		Expect(svc.plans).To(Equal([]string{"album al-1 mf-1 mf-2"}))
		Expect(svc.numbers).To(BeEmpty())
		var got struct {
			Done    bool           `json:"done"`
			AlbumID string         `json:"albumId"`
			Job     *model.EditJob `json:"job"`
		}
		Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
		Expect(got.Done).To(BeTrue())
		Expect(got.AlbumID).To(Equal("al-1"))
		Expect(got.Job.Kind).To(Equal(corrections.JobTrackNumbers))
	})

	It("refuses numbers that are no numbers", func() {
		svc.planErr = model.ErrValidation
		w := post(adminToken, body)
		Expect(w.Code).To(Equal(http.StatusBadRequest))
		var res struct{ Error string }
		Expect(json.Unmarshal(w.Body.Bytes(), &res)).To(Succeed())
		Expect(res.Error).To(Equal("invalid"))
	})

	It("denies access to regular users", func() {
		Expect(post(userToken, body).Code).To(Equal(http.StatusForbidden))
		Expect(svc.plans).To(BeEmpty())
	})
})
