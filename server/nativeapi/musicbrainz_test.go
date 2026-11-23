package nativeapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/core/musicbrainz"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakeReleaseSearch struct {
	asked    []string
	releases []musicbrainz.Release
	err      error
}

func (f *fakeReleaseSearch) SearchReleases(_ context.Context, artist, title string) ([]musicbrainz.Release, error) {
	f.asked = append(f.asked, artist+"|"+title)
	return f.releases, f.err
}

var _ = Describe("Find on MusicBrainz API", func() {
	var search *fakeReleaseSearch
	var router http.Handler
	var adminToken, userToken string

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.EnableSharing = false
		conf.Server.EnableExternalServices = true
		ds := &tests.MockDataStore{}
		auth.Init(ds)
		nativeRouter := New(ds, nil, nil, nil, tests.NewMockLibraryService(), tests.NewMockUserService(), nil, nil, nil, nil, nil, nil, nil, nil)
		rel := musicbrainz.Release{ID: "rel-1", Title: "Record", Score: 100, Status: "Official", Date: "1999-05-01",
			Country: "GB", Media: []musicbrainz.Media{{Format: "CD"}, {Format: "CD"}},
			ReleaseGroup: musicbrainz.ReleaseGroup{ID: "rg-1", PrimaryType: "Album"}}
		rel.ArtistCredit = musicbrainz.ArtistCredits{{Name: "Band", JoinPhrase: " & "}, {Name: "Friend"}}
		search = &fakeReleaseSearch{releases: []musicbrainz.Release{rel, {ID: "rel-2", Title: "Record"}}}
		nativeRouter.releaseSearch = search
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

	find := func(token, artist, title string) *httptest.ResponseRecorder {
		q := url.Values{"artist": {artist}, "title": {title}}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, createAuthenticatedRequest("GET", "/musicbrainz/release?"+q.Encode(), nil, token))
		return w
	}
	errorCode := func(w *httptest.ResponseRecorder) string {
		var res struct{ Error string }
		Expect(json.Unmarshal(w.Body.Bytes(), &res)).To(Succeed())
		return res.Error
	}

	It("lists the releases matching the album artist and title, with what tells them apart", func() {
		w := find(adminToken, "Band", "Record")
		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(search.asked).To(Equal([]string{"Band|Record"}))
		var got []map[string]any
		Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
		Expect(got).To(HaveLen(2))
		Expect(got[0]).To(Equal(map[string]any{
			"id": "rel-1", "releaseGroupId": "rg-1", "title": "Record", "artist": "Band & Friend",
			"year": "1999", "country": "GB", "format": "2×CD", "status": "Official", "score": float64(100),
			"description":  "Record (GB, 2×CD, 1999)",
			"thumbnailUrl": "https://coverartarchive.org/release/rel-1/front-250",
		}))
		Expect(got[1]).To(HaveKeyWithValue("description", "Record"))
	})

	It("answers an error code when MusicBrainz can't be reached, so the dialog can say so", func() {
		search.err = errors.New("asking MusicBrainz: connection refused")
		w := find(adminToken, "Band", "Record")
		Expect(w.Code).To(Equal(http.StatusBadGateway))
		Expect(errorCode(w)).To(Equal("unavailable"))
	})

	It("says so when MusicBrainz is busy", func() {
		search.err = musicbrainz.ErrBusy
		w := find(adminToken, "Band", "Record")
		Expect(w.Code).To(Equal(http.StatusServiceUnavailable))
		Expect(errorCode(w)).To(Equal("busy"))
	})

	It("asks nothing when external services are off", func() {
		conf.Server.EnableExternalServices = false
		w := find(adminToken, "Band", "Record")
		Expect(w.Code).To(Equal(http.StatusServiceUnavailable))
		Expect(errorCode(w)).To(Equal("disabled"))
		Expect(search.asked).To(BeEmpty())
	})

	It("needs a title", func() {
		w := find(adminToken, "Band", " ")
		Expect(w.Code).To(Equal(http.StatusBadRequest))
		Expect(errorCode(w)).To(Equal("invalid"))
		Expect(search.asked).To(BeEmpty())
	})

	It("denies access to regular users", func() {
		Expect(find(userToken, "Band", "Record").Code).To(Equal(http.StatusForbidden))
		Expect(search.asked).To(BeEmpty())
	})
})
