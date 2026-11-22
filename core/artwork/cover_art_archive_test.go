package artwork

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core/agents"
	"github.com/navidrome/navidrome/core/musicbrainz"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeCoverArtArchive serves canned archive listings by path, plus the images they point at.
type fakeCoverArtArchive struct {
	srv      *httptest.Server
	mu       sync.Mutex
	listings map[string]string // path -> JSON; anything else is a 404
	status   int               // when set, every listing request answers with it
	paths    []string
	agents   []string // User-Agent of every request
}

func newFakeCoverArtArchive() *fakeCoverArtArchive {
	f := &fakeCoverArtArchive{listings: map[string]string{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.paths = append(f.paths, r.URL.Path)
		f.agents = append(f.agents, r.UserAgent())
		body, ok := f.listings[r.URL.Path]
		status := f.status
		f.mu.Unlock()
		switch {
		case strings.HasPrefix(r.URL.Path, "/img/"):
			_, _ = w.Write([]byte("cover:" + strings.TrimPrefix(r.URL.Path, "/img/")))
		case status != 0:
			w.WriteHeader(status)
		case ok:
			_, _ = w.Write([]byte(body))
		default:
			http.NotFound(w, r)
		}
	}))
	DeferCleanup(f.srv.Close)
	f.useClient(musicbrainz.NewLimiter(0, nil))
	return f
}

// front lists a single front cover for the release or release group at path, belonging to release.
func (f *fakeCoverArtArchive) front(path, release string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listings[path] = `{"release": "https://musicbrainz.org/release/` + release + `", "images": [
	  {"types": ["Front"], "front": true, "image": "` + f.srv.URL + `/img/` + release + `-original.jpg",
	   "thumbnails": {"1200": "` + f.srv.URL + `/img/` + release + `.jpg"}}]}`
}

// useClient points the artwork package at this archive, through a real client spaced by limiter.
func (f *fakeCoverArtArchive) useClient(limiter *musicbrainz.Limiter) {
	client := musicbrainz.NewCoverArt(musicbrainz.CoverArtOptions{BaseURL: f.srv.URL + "/", Limiter: limiter})
	prev := coverArtArchive
	coverArtArchive = func() frontCoverSource { return client }
	DeferCleanup(func() { coverArtArchive = prev })
}

func (f *fakeCoverArtArchive) userAgents() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.agents...)
}

// answerAll makes every listing request answer with status.
func (f *fakeCoverArtArchive) answerAll(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = status
}

func (f *fakeCoverArtArchive) requested() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.paths...)
}

func readAll(r io.ReadCloser) string {
	defer r.Close()
	b, err := io.ReadAll(r)
	Expect(err).ToNot(HaveOccurred())
	return string(b)
}

var _ = Describe("Cover Art Archive", func() {
	var (
		ctx      context.Context
		ds       *tests.MockDataStore
		ag       *agents.Agents
		ffm      *tests.MockFFmpeg
		archive  *fakeCoverArtArchive
		trace    *ChainTrace
		resolve  func(id string) resolution
		albumsDo func(model.Albums)
	)

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.CoverArtPriority = "coverartarchive, external"
		repoRoot, err := os.Getwd()
		Expect(err).ToNot(HaveOccurred())
		libRepo := &tests.MockLibraryRepo{}
		libRepo.SetData(model.Libraries{{ID: 0, Path: testFileLibPath(repoRoot)}})
		ds = &tests.MockDataStore{MockedFolder: &fakeFolderRepo{}, MockedLibrary: libRepo}
		ffm = tests.NewMockFFmpeg("")
		ag = agents.GetAgents(&tests.MockDataStore{}, nil)
		archive = newFakeCoverArtArchive()
		trace = &ChainTrace{}
		ctx = withTrace(context.Background(), trace)
		albumsDo = func(als model.Albums) { ds.Album(ctx).(*tests.MockAlbumRepo).SetData(als) }
		resolve = func(id string) resolution {
			res, err := newResolver(ds, ag, ffm, nil).resolve(ctx, model.ArtworkQueueItem{ItemKind: "al", ItemID: id})
			Expect(err).ToNot(HaveOccurred())
			return res
		}
	})

	It("gives an album with a MusicBrainz release ID the front cover of that exact release", func() {
		archive.front("/release/rel-1", "rel-1")
		albumsDo(model.Albums{{ID: "al1", Name: "American Beauty", MbzAlbumID: "rel-1", MbzReleaseGroupID: "rg-1"}})

		res := resolve("al1")
		Expect(res.reader).ToNot(BeNil())
		Expect(readAll(res.reader)).To(Equal("cover:rel-1.jpg"))
		Expect(res.source).To(Equal("external:coverartarchive"))
		Expect(res.extErr).ToNot(HaveOccurred())
		Expect(archive.userAgents()).To(HaveEach(MatchRegexp(`^VinylUnderground/\S+$`)),
			"the listing and the image download both name the app and its version only")
		Expect(trace.Steps()).To(ContainElement(TraceStep{
			Candidate: "external:coverartarchive", Outcome: OutcomeHit, Detail: archive.srv.URL + "/img/rel-1.jpg",
		}))
	})

	It("falls back to the release group's front cover when the release has none", func() {
		archive.front("/release-group/rg-1", "rel-9")
		albumsDo(model.Albums{{ID: "al1", Name: "American Beauty", MbzAlbumID: "rel-1", MbzReleaseGroupID: "rg-1"}})

		res := resolve("al1")
		Expect(res.reader).ToNot(BeNil())
		Expect(readAll(res.reader)).To(Equal("cover:rel-9.jpg"))
		Expect(archive.requested()).To(HaveExactElements("/release/rel-1", "/release-group/rg-1", "/img/rel-9.jpg"))
	})

	It("moves on down the chain after a miss, settling it as a definitive no", func() {
		fake := &fakeImageAgent{name: "caaMissNext", imgs: []agents.ExternalImage{{URL: archive.srv.URL + "/img/lastfm.jpg"}}}
		ag = imageAgents(fake)
		albumsDo(model.Albums{{ID: "al1", Name: "American Beauty", MbzAlbumID: "rel-1", MbzReleaseGroupID: "rg-1"}})

		res := resolve("al1")
		Expect(res.reader).ToNot(BeNil())
		Expect(readAll(res.reader)).To(Equal("cover:lastfm.jpg"))
		Expect(res.source).To(Equal("external:caaMissNext"))
		Expect(res.extErr).ToNot(HaveOccurred())
		Expect(trace.Steps()[0]).To(Equal(TraceStep{Candidate: "external:coverartarchive", Outcome: OutcomeMiss}))
	})

	It("leaves an album without a MusicBrainz ID to the rest of the chain, without asking", func() {
		fake := &fakeImageAgent{name: "caaNoMbid", imgs: []agents.ExternalImage{{URL: archive.srv.URL + "/img/lastfm.jpg"}}}
		ag = imageAgents(fake)
		albumsDo(model.Albums{{ID: "al1", Name: "Some Album"}})

		res := resolve("al1")
		Expect(res.reader).ToNot(BeNil())
		Expect(readAll(res.reader)).To(Equal("cover:lastfm.jpg"))
		Expect(archive.requested()).To(Equal([]string{"/img/lastfm.jpg"}))
		Expect(trace.Steps()[0]).To(Equal(TraceStep{
			Candidate: "external:coverartarchive", Outcome: OutcomeSkipped, Detail: "album has no MusicBrainz ID",
		}))
	})

	It("records a failed lookup for a retry, rather than settling the album absent", func() {
		archive.answerAll(http.StatusInternalServerError)
		albumsDo(model.Albums{{ID: "al1", Name: "American Beauty", MbzAlbumID: "rel-1"}})

		res := resolve("al1")
		Expect(res.reader).To(BeNil())
		Expect(res.extErr).To(HaveOccurred())
		Expect(trace.Steps()[0].Outcome).To(Equal(OutcomeError))
	})

	It("asks the archive to come back later when it stays busy", func() {
		archive.answerAll(http.StatusServiceUnavailable)
		albumsDo(model.Albums{{ID: "al1", Name: "American Beauty", MbzAlbumID: "rel-1"}})
		archive.useClient(musicbrainz.NewLimiter(0, instantClock{}))

		res := resolve("al1")
		Expect(res.reader).To(BeNil())
		var retry *agents.RetryLaterError
		Expect(errors.As(res.extErr, &retry)).To(BeTrue())
	})

	It("goes through the gate under its own name", func() {
		archive.front("/release/rel-1", "rel-1")
		albumsDo(model.Albums{{ID: "al1", Name: "American Beauty", MbzAlbumID: "rel-1"}})
		var gated []string
		gate := func(name string, f func() (io.ReadCloser, string, error)) (io.ReadCloser, string, error) {
			gated = append(gated, name)
			return f()
		}

		res, err := newResolver(ds, ag, ffm, gate).resolve(ctx, model.ArtworkQueueItem{ItemKind: "al", ItemID: "al1"})
		Expect(err).ToNot(HaveOccurred())
		Expect(readAll(res.reader)).To(Equal("cover:rel-1.jpg"))
		Expect(gated).To(Equal([]string{"coverartarchive"}))
	})

	It("is never asked by the local-only resolver", func() {
		archive.front("/release/rel-1", "rel-1")
		albumsDo(model.Albums{{ID: "al1", Name: "American Beauty", MbzAlbumID: "rel-1"}})

		res, err := newLocalResolver(ds, ffm).resolve(ctx, model.ArtworkQueueItem{ItemKind: "al", ItemID: "al1"})
		Expect(err).ToNot(HaveOccurred())
		Expect(res.reader).To(BeNil())
		Expect(archive.requested()).To(BeEmpty())
	})

	It("is not asked when the config leaves it out", func() {
		conf.Server.CoverArtPriority = "cover.*, folder.*, front.*, embedded, external"
		archive.front("/release/rel-1", "rel-1")
		albumsDo(model.Albums{{ID: "al1", Name: "American Beauty", MbzAlbumID: "rel-1"}})

		_ = resolve("al1")
		Expect(archive.requested()).To(BeEmpty())
	})
})

// instantClock lets the archive's busy back-off pass without waiting.
type instantClock struct{}

func (instantClock) Now() time.Time { return time.Time{} }
func (instantClock) After(time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	ch <- time.Time{}
	return ch
}
