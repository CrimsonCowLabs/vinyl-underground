package acoustid_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/navidrome/navidrome/core/acoustid"
	"github.com/navidrome/navidrome/core/musicbrainz"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type mapStore struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (s *mapStore) Get(k string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.m[k]
	return b, ok
}

func (s *mapStore) Set(k string, b []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = b
}

const lookupJSON = `{"status": "ok", "results": [{"id": "fp-1", "score": 0.97, "recordings": [{"id": "rec-1",
  "title": "Mickey", "artists": [{"id": "a-1", "name": "Toni Basil"}],
  "releasegroups": [{"id": "rg-1", "title": "Word of Mouth", "type": "Album"}]}]}]}`

var _ = Describe("Client", func() {
	var srv *httptest.Server
	var forms []map[string][]string
	var reply string
	var fingerprinted int
	var client *acoustid.Client
	var store *mapStore

	BeforeEach(func() {
		forms, reply, fingerprinted = nil, lookupJSON, 0
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			forms = append(forms, r.PostForm)
			if reply == lookupJSON {
				_, _ = w.Write([]byte(reply))
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(reply))
		}))
		DeferCleanup(srv.Close)
		store = &mapStore{m: map[string][]byte{}}
		client = acoustid.New(acoustid.Options{
			Key: "key-1", URL: srv.URL, Store: store, Limiter: musicbrainz.NewLimiter(time.Millisecond, nil),
			Fpcalc: func(_ context.Context, path string) ([]byte, error) {
				fingerprinted++
				return []byte(`{"duration": 200.6, "fingerprint": "AQAAfp"}`), nil
			},
		})
	})

	It("fingerprints a File and looks it up", func() {
		res, err := client.Lookup(context.Background(), "/music/a.mp3", "1-2")
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(HaveLen(1))
		Expect(res[0].Score).To(BeNumerically("~", 0.97))
		rec := res[0].Recordings[0]
		Expect(rec.Title).To(Equal("Mickey"))
		Expect(rec.Artist()).To(Equal("Toni Basil"))
		Expect(rec.ArtistIDs()).To(Equal([]string{"a-1"}))
		Expect(rec.ReleaseGroups[0].Title).To(Equal("Word of Mouth"))

		Expect(forms).To(HaveLen(1))
		Expect(forms[0]["client"]).To(Equal([]string{"key-1"}))
		Expect(forms[0]["duration"]).To(Equal([]string{"200"}))
		Expect(forms[0]["fingerprint"]).To(Equal([]string{"AQAAfp"}))
	})

	It("keeps the answer, so the same File isn't fingerprinted again, unless it changed", func() {
		_, _ = client.Lookup(context.Background(), "/music/a.mp3", "1-2")
		_, err := client.Lookup(context.Background(), "/music/a.mp3", "1-2")
		Expect(err).ToNot(HaveOccurred())
		Expect(fingerprinted).To(Equal(1))
		Expect(forms).To(HaveLen(1))

		_, _ = client.Lookup(context.Background(), "/music/a.mp3", "3-4")
		Expect(fingerprinted).To(Equal(2))
	})

	It("reports AcoustID's error", func() {
		reply = `{"status": "error", "error": {"code": 4, "message": "invalid API key"}}`
		_, err := client.Lookup(context.Background(), "/music/a.mp3", "1-2")
		Expect(err).To(MatchError(ContainSubstring("invalid API key")))
		Expect(store.m).To(BeEmpty())
	})
})
