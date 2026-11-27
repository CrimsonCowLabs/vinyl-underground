package nativeapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"

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
	// asked are the searches sent: "artist|title", "barcode:...", "catno:...". Each is answered
	// with its own releases in found, if there, else with releases.
	asked    []string
	releases []musicbrainz.Release
	found    map[string][]musicbrainz.Release
	err      error
	// looked are the releases looked up (Release), each answered from releases, or with its
	// error in errs. Lookups may run at once.
	mu     sync.Mutex
	looked []string
	errs   map[string]error
}

func (f *fakeReleaseSearch) Release(_ context.Context, id string) (*musicbrainz.Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.looked = append(f.looked, id)
	if f.err != nil {
		return nil, f.err
	}
	if err := f.errs[id]; err != nil {
		return nil, err
	}
	for _, r := range f.releases {
		if r.ID == id {
			return &r, nil
		}
	}
	return &musicbrainz.Release{}, nil
}

func (f *fakeReleaseSearch) SearchReleases(_ context.Context, artist, title string) ([]musicbrainz.Release, error) {
	return f.answer(artist + "|" + title)
}

func (f *fakeReleaseSearch) SearchReleasesByBarcode(_ context.Context, barcode string) ([]musicbrainz.Release, error) {
	return f.answer("barcode:" + barcode)
}

func (f *fakeReleaseSearch) SearchReleasesByCatalogNumber(_ context.Context, catno string) ([]musicbrainz.Release, error) {
	return f.answer("catno:" + catno)
}

// answer notes the search asked, and answers it from found when it is there, else with releases.
func (f *fakeReleaseSearch) answer(asked string) ([]musicbrainz.Release, error) {
	f.asked = append(f.asked, asked)
	if rels, ok := f.found[asked]; ok {
		return rels, f.err
	}
	return f.releases, f.err
}

var _ = Describe("Find on MusicBrainz API", func() {
	var search *fakeReleaseSearch
	var router http.Handler
	var nativeRouter *Router
	var adminToken, userToken string

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.EnableSharing = false
		conf.Server.EnableExternalServices = true
		ds := &tests.MockDataStore{}
		auth.Init(ds)
		nativeRouter = New(ds, nil, nil, nil, tests.NewMockLibraryService(), tests.NewMockUserService(), nil, nil, nil, nil, nil, nil, nil, nil)
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
			"year": "1999", "country": "GB", "format": "2×CD", "status": "Official", "score": float64(100), "trackCount": float64(0),
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

	It("lists each release's number of tracks", func() {
		search.releases[0].Media = []musicbrainz.Media{{Format: "CD", TrackCount: 12}, {Format: "CD", TrackCount: 3}}
		var got []map[string]any
		Expect(json.Unmarshal(find(adminToken, "Band", "Record").Body.Bytes(), &got)).To(Succeed())
		Expect(got[0]).To(HaveKeyWithValue("trackCount", float64(15)))
		Expect(got[0]).To(HaveKeyWithValue("discTrackCounts", []any{float64(12), float64(3)}), "an album may be one disc of it")
		Expect(got[1]).ToNot(HaveKey("discTrackCounts"))
		Expect(got[1]).To(HaveKeyWithValue("trackCount", float64(0)))
	})

	Describe("searching by what the admin typed", func() {
		const mbid = "5e8cb3d5-0a4e-4b3c-9c56-1d2f3a4b5c6d"
		var houseParty musicbrainz.Release

		BeforeEach(func() {
			houseParty = musicbrainz.Release{ID: mbid, Title: "House Party, Volume 1 (Mixed by the Happy Boys)",
				Status: "Official", Date: "1993", Country: "US", Media: []musicbrainz.Media{{Format: "CD", TrackCount: 14}},
				ReleaseGroup: musicbrainz.ReleaseGroup{ID: "rg-hp"},
				ArtistCredit: musicbrainz.ArtistCredits{{Name: "Various Artists"}}}
			search.releases = append(search.releases, houseParty)
		})

		query := func(params url.Values) *httptest.ResponseRecorder {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, createAuthenticatedRequest("GET", "/musicbrainz/release?"+params.Encode(), nil, adminToken))
			return w
		}
		listed := func(w *httptest.ResponseRecorder) []map[string]any {
			GinkgoHelper()
			Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
			var got []map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
			return got
		}

		It("searches a title with the album artist, as title= does", func() {
			listed(query(url.Values{"artist": {"Band"}, "q": {"Record"}}))
			Expect(search.asked).To(Equal([]string{"Band|Record"}))
		})

		It("looks up a release by its link or MBID, listing it alone, as a search lists it", func() {
			for _, q := range []string{"https://musicbrainz.org/release/" + mbid + "/cover-art", strings.ToUpper(mbid)} {
				search.looked = nil
				got := listed(query(url.Values{"artist": {"Various"}, "q": {q}}))
				Expect(search.looked).To(Equal([]string{mbid}))
				Expect(got).To(Equal([]map[string]any{{
					"id": mbid, "releaseGroupId": "rg-hp", "title": houseParty.Title, "artist": "Various Artists",
					"year": "1993", "country": "US", "format": "CD", "status": "Official", "score": float64(100),
					"trackCount": float64(14), "description": houseParty.Title + " (US, CD, 1993)",
					"thumbnailUrl": "https://coverartarchive.org/release/" + mbid + "/front-250",
				}}))
			}
			Expect(search.asked).To(BeEmpty(), "no search")
		})

		It("answers a release lookup's errors as the pairing does", func() {
			w := query(url.Values{"q": {"00000000-0000-4000-8000-000000000001"}})
			Expect(w.Code).To(Equal(http.StatusNotFound))
			Expect(errorCode(w)).To(Equal("notFound"))

			search.err = musicbrainz.ErrBusy
			w = query(url.Values{"q": {mbid}})
			Expect(w.Code).To(Equal(http.StatusServiceUnavailable))
			Expect(errorCode(w)).To(Equal("busy"))

			search.err = errors.New("asking MusicBrainz: connection refused")
			w = query(url.Values{"q": {mbid}})
			Expect(w.Code).To(Equal(http.StatusBadGateway))
			Expect(errorCode(w)).To(Equal("unavailable"))
		})

		It("searches a barcode by any artist", func() {
			search.found = map[string][]musicbrainz.Release{"barcode:768697502725": {houseParty}}
			got := listed(query(url.Values{"artist": {"Various"}, "q": {"7 68697 50272-5"}}))
			Expect(search.asked).To(Equal([]string{"barcode:768697502725"}))
			Expect(got).To(HaveLen(1))
			Expect(got[0]).To(HaveKeyWithValue("id", mbid))
		})

		It("tries a barcode that finds nothing as a catalog number, as typed", func() {
			search.found = map[string][]musicbrainz.Release{"barcode:76869750272": {}}
			got := listed(query(url.Values{"artist": {"Various"}, "q": {"76869-75027-2"}}))
			Expect(search.asked).To(Equal([]string{"barcode:76869750272", "catno:76869-75027-2"}))
			Expect(got).To(HaveLen(len(search.releases)))
		})

		It("tries a title that finds nothing as a catalog number, by any artist", func() {
			search.found = map[string][]musicbrainz.Release{"Various Artists|House Party (Volume One)": {}}
			got := listed(query(url.Values{"artist": {"Various"}, "q": {"House Party (Volume One)"}}))
			Expect(search.asked).To(Equal([]string{"Various Artists|House Party (Volume One)", "catno:House Party (Volume One)"}))
			Expect(got).To(HaveLen(len(search.releases)))
		})

		It("doesn't try a title that finds releases as a catalog number", func() {
			listed(query(url.Values{"artist": {"Band"}, "q": {"Record"}}))
			Expect(search.asked).To(Equal([]string{"Band|Record"}))
		})

		It("searches what looks like a catalog number as one first, then as a title", func() {
			search.found = map[string][]musicbrainz.Release{"catno:WARPCD92": {houseParty}}
			Expect(listed(query(url.Values{"artist": {"Band"}, "q": {"WARPCD92"}}))).To(HaveLen(1))
			Expect(search.asked).To(Equal([]string{"catno:WARPCD92"}))

			search.asked = nil
			search.found = map[string][]musicbrainz.Release{"catno:4:44": {}}
			Expect(listed(query(url.Values{"artist": {"Band"}, "q": {"4:44"}}))).To(HaveLen(len(search.releases)))
			Expect(search.asked).To(Equal([]string{"catno:4:44", "Band|4:44"}))
		})

		It("lists nothing when no search finds anything", func() {
			search.releases = nil
			Expect(listed(query(url.Values{"artist": {"Band"}, "q": {"Record"}}))).To(BeEmpty())
			Expect(search.asked).To(Equal([]string{"Band|Record", "catno:Record"}))
		})

		It("searches a compilation, or an album by Various, VA..., as Various Artists", func() {
			for _, artist := range []string{"Various", "va", "V.A.", "V/A", "Various Artist"} {
				search.asked = nil
				listed(query(url.Values{"artist": {artist}, "q": {"Record"}}))
				Expect(search.asked).To(Equal([]string{"Various Artists|Record"}), artist)
			}
			search.asked = nil
			listed(query(url.Values{"artist": {"DJ Someone"}, "q": {"Record"}, "compilation": {"true"}}))
			Expect(search.asked).To(Equal([]string{"Various Artists|Record"}))

			// title=, the dialog's search before q=, too
			search.asked = nil
			Expect(find(adminToken, "Various", "Record").Code).To(Equal(http.StatusOK))
			Expect(search.asked).To(Equal([]string{"Various Artists|Record"}))
		})

		It("says a link to anything but a release can't be searched by, asking nothing", func() {
			w := query(url.Values{"q": {"https://musicbrainz.org/release-group/" + mbid}})
			Expect(w.Code).To(Equal(http.StatusBadRequest))
			Expect(errorCode(w)).To(Equal("notRelease"))
			Expect(search.asked).To(BeEmpty())
			Expect(search.looked).To(BeEmpty())
		})

		It("answers the search's errors as before", func() {
			search.err = musicbrainz.ErrBusy
			w := query(url.Values{"q": {"768697502725"}})
			Expect(w.Code).To(Equal(http.StatusServiceUnavailable))
			Expect(errorCode(w)).To(Equal("busy"))
			Expect(search.asked).To(HaveLen(1), "no fallback once MusicBrainz is busy")

			conf.Server.EnableExternalServices = false
			w = query(url.Values{"q": {mbid}})
			Expect(w.Code).To(Equal(http.StatusServiceUnavailable))
			Expect(errorCode(w)).To(Equal("disabled"))
			Expect(search.looked).To(BeEmpty())
		})

		It("needs something to search by", func() {
			w := query(url.Values{"artist": {"Band"}, "q": {"  "}})
			Expect(w.Code).To(Equal(http.StatusBadRequest))
			Expect(errorCode(w)).To(Equal("invalid"))
			Expect(search.asked).To(BeEmpty())
		})
	})

	Describe("fitting the releases to the album's Files", func() {
		var svc *fakeCorrections
		// ids are MusicBrainz IDs, rel-a... in search order, each a release MusicBrainz has.
		ids := []string{
			"00000000-0000-4000-8000-00000000000a", "00000000-0000-4000-8000-00000000000b",
			"00000000-0000-4000-8000-00000000000c", "00000000-0000-4000-8000-00000000000d",
			"00000000-0000-4000-8000-00000000000e", "00000000-0000-4000-8000-00000000000f",
		}

		BeforeEach(func() {
			svc = &fakeCorrections{}
			nativeRouter.corrections = svc
			search.releases = nil
			for _, id := range ids {
				search.releases = append(search.releases, musicbrainz.Release{ID: id, Title: "Record"})
			}
		})

		fit := func(token, albumID string, ids ...string) *httptest.ResponseRecorder {
			q := url.Values{"albumId": {albumID}, "ids": {strings.Join(ids, ",")}}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, createAuthenticatedRequest("GET", "/musicbrainz/release/fit?"+q.Encode(), nil, token))
			return w
		}
		fitsOf := func(w *httptest.ResponseRecorder) []map[string]any {
			GinkgoHelper()
			Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
			var got []map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
			return got
		}

		It("looks up the first five releases, in search order, and scores them against the album, marking the best", func() {
			got := fitsOf(fit(adminToken, "al-1", ids...))
			Expect(search.looked).To(ConsistOf(ids[:5]), "five at most")
			Expect(svc.fitted).To(Equal([]string{"al-1|" + ids[0], "al-1|" + ids[1], "al-1|" + ids[2],
				"al-1|" + ids[3], "al-1|" + ids[4]}))
			Expect(got).To(HaveLen(5))
			Expect(got[0]).To(Equal(map[string]any{"id": ids[0], "paired": float64(1), "trackCount": float64(0),
				"albumTracks": float64(1), "lengthDiff": nil, "best": true}))
			Expect(got[1]).To(HaveKeyWithValue("best", false))
		})

		It("leaves a release that can't be looked up unscored, and scores the others", func() {
			search.errs = map[string]error{ids[0]: musicbrainz.ErrBusy, ids[2]: errors.New("asking MusicBrainz: timeout")}
			got := fitsOf(fit(adminToken, "al-1", ids[:3]...))
			Expect(got).To(HaveLen(1))
			Expect(got[0]).To(HaveKeyWithValue("id", ids[1]))
		})

		It("says MusicBrainz is busy when it is for every release", func() {
			search.err = musicbrainz.ErrBusy
			w := fit(adminToken, "al-1", ids[:2]...)
			Expect(w.Code).To(Equal(http.StatusServiceUnavailable))
			Expect(errorCode(w)).To(Equal("busy"))
			Expect(svc.fitted).To(BeEmpty())

			// Not when the others fail otherwise: none is scored, and that is the answer.
			search.err = nil
			search.errs = map[string]error{ids[0]: musicbrainz.ErrBusy, ids[1]: musicbrainz.ErrNotFound}
			Expect(fitsOf(fit(adminToken, "al-1", ids[:2]...))).To(BeEmpty())
		})

		It("needs an album and MusicBrainz release IDs, asking nothing otherwise", func() {
			for _, w := range []*httptest.ResponseRecorder{
				fit(adminToken, "", ids[0]),
				fit(adminToken, "al-1"),
				fit(adminToken, "al-1", ids[0], "nope"),
			} {
				Expect(w.Code).To(Equal(http.StatusBadRequest))
				Expect(errorCode(w)).To(Equal("invalid"))
			}
			Expect(search.looked).To(BeEmpty())
		})

		It("answers the album's error when it can't be had", func() {
			svc.err = model.ErrNotFound
			w := fit(adminToken, "al-x", ids[0])
			Expect(w.Code).To(Equal(http.StatusNotFound))
			Expect(errorCode(w)).To(Equal("notFound"))
		})

		It("asks nothing when external services are off, and only for admins", func() {
			conf.Server.EnableExternalServices = false
			w := fit(adminToken, "al-1", ids[0])
			Expect(w.Code).To(Equal(http.StatusServiceUnavailable))
			Expect(errorCode(w)).To(Equal("disabled"))
			conf.Server.EnableExternalServices = true
			Expect(fit(userToken, "al-1", ids[0]).Code).To(Equal(http.StatusForbidden))
			Expect(search.looked).To(BeEmpty())
		})
	})
})
