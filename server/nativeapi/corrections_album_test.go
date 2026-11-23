package nativeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"

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

var _ = Describe("Album Edit API", func() {
	var svc *fakeCorrections
	var ds *tests.MockDataStore
	var nativeRouter *Router
	var router http.Handler
	var adminToken, userToken string
	body := map[string]any{"albumId": "al-1", "name": "Record", "albumArtist": "Band", "year": 1999}

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.EnableSharing = false
		ds = &tests.MockDataStore{}
		auth.Init(ds)
		nativeRouter = New(ds, nil, nil, nil, tests.NewMockLibraryService(), tests.NewMockUserService(), nil, nil, nil, nil, nil, nil, nil, nil)
		svc = &fakeCorrections{}
		nativeRouter.corrections = svc
		router = server.JWTVerifier(nativeRouter)
		ds.Album(context.TODO()).(*tests.MockAlbumRepo).SetData(model.Albums{{ID: "al-1", Name: "Recrod", AlbumArtist: "Band"}})
		// Once written, the album's Tracks are on the album with its new ID.
		ds.MediaFile(context.TODO()).(*tests.MockMediaFileRepo).SetData(model.MediaFiles{
			{ID: "mf-1", AlbumID: "new-album"}, {ID: "mf-2", AlbumID: "new-album"}})

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
		router.ServeHTTP(w, createAuthenticatedRequest("POST", "/correction/album", &buf, token))
		return w
	}
	type answer struct {
		JobID      string                `json:"jobId"`
		Done       bool                  `json:"done"`
		Background bool                  `json:"background"`
		AlbumID    string                `json:"albumId"`
		PickCover  corrections.PickCover `json:"pickCover"`
		Job        *model.EditJob        `json:"job"`
	}
	answerOf := func(w *httptest.ResponseRecorder) answer {
		GinkgoHelper()
		var got answer
		Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
		return got
	}

	It("writes the album's values, and the Tracks' changed numbers, into its Files as one job, answering its new ID", func() {
		runEditQueue(nativeRouter, ds, fakeWriteKind{})
		svc.plan = fakePlan(corrections.JobAlbumEdit, "mf-1", "mf-2")
		w := post(adminToken, map[string]any{"albumId": "al-1", "name": "Record", "albumArtist": "Band", "year": 1999,
			"tracks": []map[string]any{{"id": "mf-2", "discNumber": 1, "trackNumber": 2}}})
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		Expect(svc.plans).To(Equal([]string{"album al-1 Band|Record mf-2"}))
		// No Correction is saved.
		Expect(svc.albums).To(BeEmpty())
		got := answerOf(w)
		Expect(got.Done).To(BeTrue())
		Expect(got.AlbumID).To(Equal("new-album"))
		Expect(got.Job.Kind).To(Equal(corrections.JobAlbumEdit))
		Expect(got.Job.Total).To(Equal(2))
	})

	It("answers 202 while the job carries on in the background", func() {
		k := fakeWriteKind{hold: make(chan struct{})}
		runEditQueue(nativeRouter, ds, k)
		nativeRouter.editWait = 50 * time.Millisecond
		w := post(adminToken, body)
		Expect(w.Code).To(Equal(http.StatusAccepted))
		got := answerOf(w)
		Expect(got.Background).To(BeTrue())
		Expect(got.JobID).ToNot(BeEmpty())
		Expect(got.Done).To(BeFalse())
	})

	It("still saves a picked MusicBrainz release as a Correction (#170), with only the release", func() {
		runEditQueue(nativeRouter, ds, fakeWriteKind{})
		picked := map[string]any{"albumId": "al-1", "name": "Record", "albumArtist": "Band", "year": 1999,
			"mbzAlbumId": "rel-1", "mbzReleaseGroupId": "rg-1", "mbzRelease": "Record (GB, CD, 1999)"}
		w := post(adminToken, picked)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		// The album's values as they are: the new ones go to its Files.
		Expect(svc.fixes).To(Equal([]corrections.AlbumFix{{Name: "Recrod", AlbumArtist: "Band",
			MbzAlbumID: "rel-1", MbzReleaseGroupID: "rg-1", MbzRelease: "Record (GB, CD, 1999)"}}))
		Expect(svc.plans).To(HaveLen(1))
		Expect(answerOf(w).Job).ToNot(BeNil())
	})

	It("saves only the release when that is all that changed", func() {
		runEditQueue(nativeRouter, ds, fakeWriteKind{})
		svc.planErr = corrections.ErrNothingToWrite
		w := post(adminToken, map[string]any{"albumId": "al-1", "name": "Recrod", "albumArtist": "Band",
			"mbzAlbumId": "rel-1", "mbzReleaseGroupId": "rg-1"})
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		got := answerOf(w)
		Expect(got.Done).To(BeTrue())
		Expect(got.AlbumID).To(Equal("new-album"))
		Expect(got.Job).To(BeNil())
		Expect(svc.albums).To(HaveLen(1))
	})

	It("refuses a save that changes nothing", func() {
		runEditQueue(nativeRouter, ds, fakeWriteKind{})
		svc.planErr = corrections.ErrNothingToWrite
		w := post(adminToken, body)
		Expect(w.Code).To(Equal(http.StatusBadRequest))
		var res struct{ Error string }
		Expect(json.Unmarshal(w.Body.Bytes(), &res)).To(Succeed())
		Expect(res.Error).To(Equal("invalid"))
	})

	It("answers editingOff with no edit job queue", func() {
		w := post(adminToken, body)
		Expect(w.Code).To(Equal(http.StatusForbidden))
		var res struct{ Error string }
		Expect(json.Unmarshal(w.Body.Bytes(), &res)).To(Succeed())
		Expect(res.Error).To(Equal("editingOff"))
	})

	It("denies access to regular users", func() {
		Expect(post(userToken, body).Code).To(Equal(http.StatusForbidden))
		Expect(svc.plans).To(BeEmpty())
		Expect(svc.albums).To(BeEmpty())
	})
})
