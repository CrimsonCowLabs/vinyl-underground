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
	"github.com/navidrome/navidrome/core/corrections"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Album Correction API", func() {
	var svc *fakeCorrections
	var router http.Handler
	var adminToken, userToken string
	body := map[string]any{"albumId": "al-1", "name": "Record", "albumArtist": "Band", "year": 1999}

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.EnableSharing = false
		ds := &tests.MockDataStore{}
		auth.Init(ds)
		nativeRouter := New(ds, nil, nil, nil, tests.NewMockLibraryService(), tests.NewMockUserService(), nil, nil, nil, nil, nil, nil, nil)
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

	post := func(token string, body any) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		Expect(json.NewEncoder(&buf).Encode(body)).To(Succeed())
		w := httptest.NewRecorder()
		router.ServeHTTP(w, createAuthenticatedRequest("POST", "/correction/album", &buf, token))
		return w
	}

	It("corrects an album, answering with its new ID", func() {
		w := post(adminToken, body)
		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(svc.albums).To(Equal([]string{"al-1->Band|Record|1999"}))
		var got map[string]any
		Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
		Expect(got).To(HaveKeyWithValue("albumId", "new-album"))
		Expect(got).To(HaveKeyWithValue("done", true))
	})

	It("passes on the MusicBrainz release picked with Find on MusicBrainz", func() {
		picked := map[string]any{"albumId": "al-1", "name": "Record", "albumArtist": "Band", "year": 1999,
			"mbzAlbumId": "rel-1", "mbzReleaseGroupId": "rg-1", "mbzRelease": "Record (GB, CD, 1999)"}
		Expect(post(adminToken, picked).Code).To(Equal(http.StatusOK))
		Expect(svc.fixes).To(Equal([]corrections.AlbumFix{{Name: "Record", AlbumArtist: "Band", Year: 1999,
			MbzAlbumID: "rel-1", MbzReleaseGroupID: "rg-1", MbzRelease: "Record (GB, CD, 1999)"}}))
	})

	It("refuses an album sharing its folder with another album", func() {
		svc.err = corrections.ErrSharedFolder
		w := post(adminToken, body)
		Expect(w.Code).To(Equal(http.StatusBadRequest))
		var res struct{ Error string }
		Expect(json.Unmarshal(w.Body.Bytes(), &res)).To(Succeed())
		Expect(res.Error).To(Equal("sharedFolder"))
	})

	It("denies access to regular users", func() {
		Expect(post(userToken, body).Code).To(Equal(http.StatusForbidden))
		Expect(svc.albums).To(BeEmpty())
	})
})
