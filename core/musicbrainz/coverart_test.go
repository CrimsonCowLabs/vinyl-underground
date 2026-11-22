package musicbrainz_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/navidrome/navidrome/core/musicbrainz"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// A trimmed answer from coverartarchive.org/release/rel-1: a back cover listed before the front.
const releaseCoversJSON = `{"release": "https://musicbrainz.org/release/rel-1", "images": [
  {"types": ["Back"], "front": false, "image": "http://archive.example/rel-1-back.jpg",
   "thumbnails": {"250": "http://archive.example/rel-1-back-250.jpg"}},
  {"types": ["Front"], "front": true, "image": "http://archive.example/rel-1-front.jpg",
   "thumbnails": {"250": "http://archive.example/rel-1-front-250.jpg", "500": "http://archive.example/rel-1-front-500.jpg",
                  "1200": "http://archive.example/rel-1-front-1200.jpg", "large": "http://archive.example/rel-1-front-500.jpg"}}]}`

// The release group answers with the covers of whichever release represents it.
const groupCoversJSON = `{"release": "https://musicbrainz.org/release/rel-9", "images": [
  {"types": ["Front"], "front": true, "image": "http://archive.example/rel-9-front.jpg",
   "thumbnails": {"large": "http://archive.example/rel-9-front-500.jpg"}}]}`

// Only a back cover: the release has art, but no front.
const backOnlyJSON = `{"release": "https://musicbrainz.org/release/rel-2", "images": [
  {"types": ["Back"], "front": false, "image": "http://archive.example/rel-2-back.jpg", "thumbnails": {}}]}`

type cannedReply struct {
	status int
	body   string
	header http.Header
}

var _ = Describe("CoverArt", func() {
	var srv *httptest.Server
	var mu sync.Mutex
	var requests []*http.Request
	var routes map[string][]cannedReply
	var clock *instantClock
	var archive *musicbrainz.CoverArt
	ctx := context.Background()

	paths := func() []string {
		mu.Lock()
		defer mu.Unlock()
		var p []string
		for _, r := range requests {
			p = append(p, r.URL.Path)
		}
		return p
	}

	BeforeEach(func() {
		requests, routes = nil, map[string][]cannedReply{}
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			requests = append(requests, r)
			replies := routes[r.URL.Path]
			reply := cannedReply{status: http.StatusNotFound, body: "Not Found"}
			if len(replies) > 0 {
				reply = replies[0]
				if len(replies) > 1 {
					routes[r.URL.Path] = replies[1:]
				}
			}
			mu.Unlock()
			for k, v := range reply.header {
				w.Header()[k] = v
			}
			w.WriteHeader(reply.status)
			_, _ = w.Write([]byte(reply.body))
		}))
		DeferCleanup(srv.Close)
		clock = &instantClock{now: time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC)}
		archive = musicbrainz.NewCoverArt(musicbrainz.CoverArtOptions{
			BaseURL: srv.URL + "/", Limiter: musicbrainz.NewLimiter(time.Second, clock),
		})
	})

	ok := func(body string) []cannedReply { return []cannedReply{{status: http.StatusOK, body: body}} }

	Describe("ReleaseFront", func() {
		It("names a release's front cover without asking the archive", func() {
			cover := archive.ReleaseFront("rel-3")
			Expect(cover).To(Equal(musicbrainz.Cover{
				ReleaseID: "rel-3", Front: true,
				URL: srv.URL + "/release/rel-3/front-1200", ThumbnailURL: srv.URL + "/release/rel-3/front-250",
			}))
			Expect(paths()).To(BeEmpty())
		})
	})

	Describe("FrontCover", func() {
		It("returns the exact release's front cover, at the 1200px thumbnail", func() {
			routes["/release/rel-1"] = ok(releaseCoversJSON)

			cover, err := archive.FrontCover(ctx, "rel-1", "rg-1")
			Expect(err).ToNot(HaveOccurred())
			Expect(cover.ReleaseID).To(Equal("rel-1"))
			Expect(cover.URL).To(Equal("http://archive.example/rel-1-front-1200.jpg"))
			Expect(cover.FromReleaseGroup).To(BeFalse())
			Expect(paths()).To(Equal([]string{"/release/rel-1"}))
		})

		It("falls back to the release group's front cover when the release has none", func() {
			routes["/release-group/rg-1"] = ok(groupCoversJSON)

			cover, err := archive.FrontCover(ctx, "rel-1", "rg-1")
			Expect(err).ToNot(HaveOccurred())
			Expect(cover.ReleaseID).To(Equal("rel-9"))
			Expect(cover.URL).To(Equal("http://archive.example/rel-9-front-500.jpg"))
			Expect(cover.FromReleaseGroup).To(BeTrue())
			Expect(paths()).To(Equal([]string{"/release/rel-1", "/release-group/rg-1"}))
		})

		It("falls back to the release group when the release has art but no front cover", func() {
			routes["/release/rel-2"] = ok(backOnlyJSON)
			routes["/release-group/rg-1"] = ok(groupCoversJSON)

			cover, err := archive.FrontCover(ctx, "rel-2", "rg-1")
			Expect(err).ToNot(HaveOccurred())
			Expect(cover.ReleaseID).To(Equal("rel-9"))
		})

		It("reports ErrNoCover when neither the release nor its group has a front cover", func() {
			_, err := archive.FrontCover(ctx, "rel-1", "rg-1")
			Expect(err).To(MatchError(musicbrainz.ErrNoCover))
		})

		It("asks only for the release group when there is no release ID", func() {
			routes["/release-group/rg-1"] = ok(groupCoversJSON)

			_, err := archive.FrontCover(ctx, "", "rg-1")
			Expect(err).ToNot(HaveOccurred())
			Expect(paths()).To(Equal([]string{"/release-group/rg-1"}))
		})

		It("reports ErrNoCover without asking when there is no ID at all", func() {
			_, err := archive.FrontCover(ctx, "", "")
			Expect(err).To(MatchError(musicbrainz.ErrNoCover))
			Expect(paths()).To(BeEmpty())
		})

		It("treats a malformed ID (400) as no cover", func() {
			routes["/release/not-an-mbid"] = []cannedReply{{status: http.StatusBadRequest, body: "invalid MBID"}}

			_, err := archive.FrontCover(ctx, "not-an-mbid", "")
			Expect(err).To(MatchError(musicbrainz.ErrNoCover))
		})

		It("does not fall back to the release group when the release lookup failed", func() {
			routes["/release/rel-1"] = []cannedReply{{status: http.StatusInternalServerError}}
			routes["/release-group/rg-1"] = ok(groupCoversJSON)

			_, err := archive.FrontCover(ctx, "rel-1", "rg-1")
			Expect(err).To(HaveOccurred())
			Expect(err).ToNot(MatchError(musicbrainz.ErrNoCover))
			Expect(paths()).To(Equal([]string{"/release/rel-1"}))
		})
	})

	Describe("ReleaseCovers", func() {
		It("lists the release's images, front first, labelled with their release", func() {
			routes["/release/rel-1"] = ok(releaseCoversJSON)

			covers, err := archive.ReleaseCovers(ctx, "rel-1")
			Expect(err).ToNot(HaveOccurred())
			Expect(covers).To(HaveLen(2))
			Expect(covers[0].Front).To(BeTrue())
			Expect(covers[0].ReleaseID).To(Equal("rel-1"))
			Expect(covers[1].Front).To(BeFalse())
			Expect(covers[1].URL).To(Equal("http://archive.example/rel-1-back.jpg"))
			Expect(covers[1].ThumbnailURL).To(Equal("http://archive.example/rel-1-back-250.jpg"))
		})
	})

	Describe("politeness", func() {
		It("names the app and its version only, with no contact details", func() {
			routes["/release/rel-1"] = ok(releaseCoversJSON)

			_, _ = archive.FrontCover(ctx, "rel-1", "")
			mu.Lock()
			defer mu.Unlock()
			Expect(requests).To(HaveLen(1))
			Expect(requests[0].Header.Get("User-Agent")).To(MatchRegexp(`^VinylUnderground/\S+$`))
		})

		It("spaces its requests one interval apart", func() {
			_, _ = archive.FrontCover(ctx, "rel-1", "rg-1")
			_, _ = archive.FrontCover(ctx, "rel-2", "rg-2")

			Expect(paths()).To(HaveLen(4))
			Expect(clock.Waits()).To(Equal([]time.Duration{time.Second, time.Second, time.Second}))
		})

		It("backs off and retries when the archive is busy, honouring Retry-After", func() {
			routes["/release/rel-1"] = []cannedReply{
				{status: http.StatusServiceUnavailable, header: http.Header{"Retry-After": {"7"}}},
				{status: http.StatusOK, body: releaseCoversJSON},
			}

			cover, err := archive.FrontCover(ctx, "rel-1", "")
			Expect(err).ToNot(HaveOccurred())
			Expect(cover.ReleaseID).To(Equal("rel-1"))
			Expect(clock.Waits()).To(Equal([]time.Duration{7 * time.Second}))
		})

		It("gives up with a busy error, naming the wait, when the archive stays busy", func() {
			busy := cannedReply{status: http.StatusTooManyRequests, header: http.Header{"Retry-After": {"30"}}}
			routes["/release/rel-1"] = []cannedReply{busy}

			_, err := archive.FrontCover(ctx, "rel-1", "rg-1")
			var busyErr *musicbrainz.BusyError
			Expect(errors.As(err, &busyErr)).To(BeTrue())
			Expect(busyErr.RetryIn).To(Equal(30 * time.Second))
			Expect(paths()).To(HaveLen(3))
		})

		It("remembers an answer, so a second lookup of the same release costs nothing", func() {
			routes["/release/rel-1"] = ok(releaseCoversJSON)

			_, _ = archive.FrontCover(ctx, "rel-1", "")
			_, _ = archive.FrontCover(ctx, "rel-1", "")
			Expect(paths()).To(HaveLen(1))
		})
	})
})
