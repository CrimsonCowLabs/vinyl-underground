package musicbrainz_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/navidrome/navidrome/core/musicbrainz"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// instantClock never really waits: it records each wait and moves its time on by it.
type instantClock struct {
	mu    sync.Mutex
	now   time.Time
	waits []time.Duration
}

func (c *instantClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *instantClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.waits = append(c.waits, d)
	c.now = c.now.Add(d)
	ch := make(chan time.Time, 1)
	ch <- c.now
	return ch
}

func (c *instantClock) Waits() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.waits...)
}

const searchJSON = `{"count": 1, "recordings": [{"id": "rec-1", "score": 100, "title": "Song", "length": 200000,
  "artist-credit": [{"name": "Band", "artist": {"id": "a-1", "name": "Band"}}],
  "releases": [{"id": "rel-1", "title": "Record", "status": "Official", "date": "1999-05-01",
    "artist-credit": [{"name": "Band", "joinphrase": " & ", "artist": {"id": "a-1", "name": "Band"}},
                      {"name": "Friend", "artist": {"id": "a-2", "name": "Friend"}}],
    "release-group": {"id": "rg-1", "title": "Record", "primary-type": "Album", "secondary-types": ["Live"]}}]}]}`

var _ = Describe("Client", func() {
	var srv *httptest.Server
	var mu sync.Mutex
	var requests []*http.Request
	var replies []func(w http.ResponseWriter)
	var clock *instantClock
	var client *musicbrainz.Client

	BeforeEach(func() {
		requests, replies = nil, nil
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			requests = append(requests, r)
			var reply func(w http.ResponseWriter)
			if len(replies) > 0 {
				reply, replies = replies[0], replies[1:]
			}
			mu.Unlock()
			if reply == nil {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(searchJSON))
				return
			}
			reply(w)
		}))
		DeferCleanup(srv.Close)
		clock = &instantClock{now: time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)}
		client = musicbrainz.New(musicbrainz.Options{
			BaseURL: srv.URL + "/ws/2/", Limiter: musicbrainz.NewLimiter(time.Second, clock),
		})
	})

	busy := func(retryAfter string) func(w http.ResponseWriter) {
		return func(w http.ResponseWriter) {
			if retryAfter != "" {
				w.Header().Set("Retry-After", retryAfter)
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error": "The MusicBrainz web server is currently busy. Please try again later."}`))
		}
	}
	served := func() []*http.Request {
		mu.Lock()
		defer mu.Unlock()
		return append([]*http.Request(nil), requests...)
	}

	It("searches recordings, sending a User-Agent that names the app, its version and the repo", func() {
		recs, err := client.SearchRecordings(context.Background(), `recording:"Song" AND artist:"Band"`)
		Expect(err).ToNot(HaveOccurred())
		Expect(recs).To(HaveLen(1))
		Expect(recs[0].Title).To(Equal("Song"))
		Expect(recs[0].Releases[0].ArtistCredit.String()).To(Equal("Band & Friend"))
		Expect(recs[0].Releases[0].ReleaseGroup.SecondaryTypes).To(Equal([]string{"Live"}))

		reqs := served()
		Expect(reqs).To(HaveLen(1))
		Expect(reqs[0].URL.Path).To(Equal("/ws/2/recording"))
		Expect(reqs[0].URL.Query().Get("query")).To(Equal(`recording:"Song" AND artist:"Band"`))
		Expect(reqs[0].URL.Query().Get("fmt")).To(Equal("json"))
		ua := reqs[0].Header.Get("User-Agent")
		Expect(ua).To(MatchRegexp(`^VinylUnderground/\S+ \( https://github\.com/CrimsonCowLabs/vinyl-underground \)$`))
		Expect(ua).ToNot(ContainSubstring("@"))
	})

	It("waits out a 503 and tries again, backing off longer each time", func() {
		replies = []func(http.ResponseWriter){busy(""), busy("")}
		recs, err := client.SearchRecordings(context.Background(), "q")
		Expect(err).ToNot(HaveOccurred())
		Expect(recs).To(HaveLen(1))
		Expect(served()).To(HaveLen(3))
		waits := clock.Waits()
		Expect(waits).To(HaveLen(2))
		Expect(waits[0]).To(BeNumerically(">=", 2*time.Second))
		Expect(waits[1]).To(BeNumerically(">", waits[0]))
	})

	It("honours a 503's Retry-After", func() {
		replies = []func(http.ResponseWriter){busy("7")}
		_, err := client.SearchRecordings(context.Background(), "q")
		Expect(err).ToNot(HaveOccurred())
		Expect(clock.Waits()).To(ConsistOf(BeNumerically(">=", 7*time.Second)))
	})

	It("gives up with an error when MusicBrainz stays busy", func() {
		for range 10 {
			replies = append(replies, busy(""))
		}
		_, err := client.SearchRecordings(context.Background(), "q")
		Expect(err).To(MatchError(ContainSubstring("busy")))
		Expect(len(served())).To(BeNumerically("<", 10))
	})

	It("reports any other error status without retrying", func() {
		replies = []func(http.ResponseWriter){func(w http.ResponseWriter) { w.WriteHeader(http.StatusBadRequest) }}
		_, err := client.SearchRecordings(context.Background(), "q")
		Expect(err).To(MatchError(ContainSubstring("400")))
		Expect(served()).To(HaveLen(1))
	})

	It("caches answers, so the same search is only sent once", func() {
		for range 2 {
			_, err := client.SearchRecordings(context.Background(), "q")
			Expect(err).ToNot(HaveOccurred())
		}
		Expect(served()).To(HaveLen(1))
	})

	It("doesn't cache errors", func() {
		replies = []func(http.ResponseWriter){func(w http.ResponseWriter) { w.WriteHeader(http.StatusInternalServerError) }}
		_, err := client.SearchRecordings(context.Background(), "q")
		Expect(err).To(HaveOccurred())
		_, err = client.SearchRecordings(context.Background(), "q")
		Expect(err).ToNot(HaveOccurred())
		Expect(served()).To(HaveLen(2))
	})
})
