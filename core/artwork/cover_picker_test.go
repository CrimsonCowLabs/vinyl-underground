package artwork

import (
	"context"
	"fmt"
	"image/color"
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

// fakeMusicBrainz answers release browses by release group, from canned JSON.
type fakeMusicBrainz struct {
	srv      *httptest.Server
	mu       sync.Mutex
	releases map[string]string // release group ID -> JSON
	status   int
	block    chan struct{} // when set, every request waits for it (or for the client to give up)
	asked    []string
}

func newFakeMusicBrainz() *fakeMusicBrainz {
	f := &fakeMusicBrainz{releases: map[string]string{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		rg := r.URL.Query().Get("release-group")
		f.asked = append(f.asked, rg)
		body, ok := f.releases[rg]
		status, block := f.status, f.block
		f.mu.Unlock()
		if block != nil {
			select {
			case <-block:
			case <-r.Context().Done():
				return
			}
		}
		switch {
		case status != 0:
			w.WriteHeader(status)
		case ok:
			_, _ = w.Write([]byte(body))
		default:
			http.NotFound(w, r)
		}
	}))
	DeferCleanup(f.srv.Close)
	return f
}

func (f *fakeMusicBrainz) client() *musicbrainz.Client {
	return musicbrainz.New(musicbrainz.Options{BaseURL: f.srv.URL + "/ws/2/", Limiter: musicbrainz.NewLimiter(0, nil)})
}

func (f *fakeMusicBrainz) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.asked...)
}

// release is one browsed release, as MusicBrainz lists it.
func release(id, title, status, date, country, format string, front bool) string {
	return fmt.Sprintf(`{"id": %q, "title": %q, "status": %q, "date": %q, "country": %q,
	  "media": [{"format": %q}], "cover-art-archive": {"front": %t}}`, id, title, status, date, country, format, front)
}

var _ = Describe("CoverPicker", func() {
	var (
		ctx     context.Context
		ds      *tests.MockDataStore
		ag      *agents.Agents
		archive *fakeCoverArtArchive
		mbz     *fakeMusicBrainz
		picker  func() CoverPicker
		timeout time.Duration
		album   model.Album
	)

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.DataFolder = conf.NewDir(GinkgoT().TempDir())
		ctx = context.Background()
		repoRoot, err := os.Getwd()
		Expect(err).ToNot(HaveOccurred())
		libRepo := &tests.MockLibraryRepo{}
		libRepo.SetData(model.Libraries{{ID: 0, Path: testFileLibPath(repoRoot)}})
		folders := &fakeFolderRepo{result: []model.Folder{{
			ID: "f1", Path: "tests/fixtures/artist", Name: "an-album",
			ImageFiles: []string{"artist.png", "cover.jpg", "front.png"},
		}}}
		ds = &tests.MockDataStore{MockedFolder: folders, MockedLibrary: libRepo}
		album = model.Album{ID: "al1", Name: "Record", AlbumArtist: "Band", FolderIDs: []string{"f1"},
			EmbedArtPath: "tests/fixtures/artist/an-album/test.mp3", MbzAlbumID: "rel-1", MbzReleaseGroupID: "rg-1"}
		ds.Album(ctx).(*tests.MockAlbumRepo).SetData(model.Albums{album})
		ag = agents.GetAgents(&tests.MockDataStore{}, nil)
		archive = newFakeCoverArtArchive()
		mbz = newFakeMusicBrainz()
		timeout = 5 * time.Second
		picker = func() CoverPicker {
			archiveClient := musicbrainz.NewCoverArt(musicbrainz.CoverArtOptions{
				BaseURL: archive.srv.URL + "/", Limiter: musicbrainz.NewLimiter(0, nil)})
			return newCoverPicker(ds, ag, tests.NewMockFFmpeg(""), archiveClient, mbz.client(), timeout)
		}
	})

	labels := func(cs *CoverCandidates) []string {
		var res []string
		for _, c := range cs.Candidates {
			res = append(res, c.Source+": "+c.Label+" "+c.Detail)
		}
		return res
	}

	Describe("local candidates", func() {
		It("lists the album folder's cover, folder and front images, and the embedded art", func() {
			cs, err := picker().Candidates(ctx, "al1", CoverSourceLocal)
			Expect(err).ToNot(HaveOccurred())
			Expect(cs.Failed).To(BeFalse())
			Expect(labels(cs)).To(Equal([]string{"folder: cover.jpg ", "folder: front.png ", "embedded: test.mp3 "}))
		})

		It("previews and stores the chosen one", func() {
			p := picker()
			cs, err := p.Candidates(ctx, "al1", CoverSourceLocal)
			Expect(err).ToNot(HaveOccurred())

			preview, err := p.Preview(ctx, "al1", cs.Candidates[0].ID)
			Expect(err).ToNot(HaveOccurred())
			want, err := os.ReadFile("tests/fixtures/artist/an-album/cover.jpg")
			Expect(err).ToNot(HaveOccurred())
			Expect(readAll(preview)).To(Equal(string(want)))

			stored, cand, err := p.Store(ctx, "al1", cs.Candidates[0].ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(stored.Mime).To(Equal("image/jpeg"))
			Expect(cand.Description()).To(Equal("Folder image: cover.jpg"))
			got, err := os.ReadFile(ChosenCoverStore().path(stored.Hash, stored.Mime))
			Expect(err).ToNot(HaveOccurred())
			Expect(got).To(Equal(want))
		})

		It("only knows the candidates it listed, for the album it listed them for", func() {
			p := picker()
			cs, err := p.Candidates(ctx, "al1", CoverSourceLocal)
			Expect(err).ToNot(HaveOccurred())
			_, _, err = p.Store(ctx, "al1", "made-up")
			Expect(err).To(MatchError(model.ErrNotFound))
			_, err = p.Preview(ctx, "al2", cs.Candidates[0].ID)
			Expect(err).To(MatchError(model.ErrNotFound))
		})

		It("returns not found for an unknown album", func() {
			_, err := picker().Candidates(ctx, "nope", CoverSourceLocal)
			Expect(err).To(MatchError(model.ErrNotFound))
		})
	})

	Describe("external candidates", func() {
		It("lists each image agent's album image, by the agent's name", func() {
			fake := &fakeImageAgent{name: "pickerAgent", imgs: []agents.ExternalImage{
				{URL: archive.srv.URL + "/img/small.jpg", Size: 64}, {URL: archive.srv.URL + "/img/agent.jpg", Size: 300}}}
			ag = imageAgents(fake)

			p := picker()
			cs, err := p.Candidates(ctx, "al1", CoverSourceExternal)
			Expect(err).ToNot(HaveOccurred())
			Expect(labels(cs)).To(Equal([]string{"pickerAgent: pickerAgent "}))
			Expect(fake.gotAlbumName).To(Equal("Record"))

			stored, cand, err := p.Store(ctx, "al1", cs.Candidates[0].ID)
			Expect(err).To(MatchError(model.ErrValidation), "the fake image is no image")
			Expect(stored).To(BeZero())
			Expect(cand.Description()).To(Equal("pickerAgent"))
		})

		It("reports an agent that fails", func() {
			ag = imageAgents(&fakeImageAgent{name: "pickerFailing", err: fmt.Errorf("boom")})
			cs, err := picker().Candidates(ctx, "al1", CoverSourceExternal)
			Expect(err).ToNot(HaveOccurred())
			Expect(cs.Candidates).To(BeEmpty())
			Expect(cs.Failed).To(BeTrue())
		})
	})

	Describe("Cover Art Archive candidates", func() {
		BeforeEach(func() {
			archive.front("/release/rel-1", "rel-1")
			mbz.releases["rg-1"] = `{"releases": [` + strings.Join([]string{
				release("rel-1", "Record", "Official", "1999-05-01", "GB", "CD", true),
				release("rel-2", "Record", "Bootleg", "1998", "XW", "Cassette", true),
				release("rel-3", "Record (Deluxe)", "Official", "2009-10-01", "US", `12" Vinyl`, true),
				release("rel-4", "Record", "Official", "2001", "JP", "CD", false),
				release("rel-5", "Record", "Official", "", "", "", true),
			}, ",") + `]}`
		})

		It("lists the exact release, then the other releases with a front cover, official and oldest first", func() {
			cs, err := picker().Candidates(ctx, "al1", CoverSourceCoverArtArchive)
			Expect(err).ToNot(HaveOccurred())
			Expect(cs.Failed).To(BeFalse())
			Expect(labels(cs)).To(Equal([]string{
				"coverartarchive: Record GB, CD, 1999",
				"coverartarchive: Record (Deluxe) US, 12\" Vinyl, 2009",
				"coverartarchive: Record ",
				"coverartarchive: Record XW, Cassette, 1998",
			}))
			Expect(cs.Candidates[0].ExactRelease).To(BeTrue())
			Expect(cs.Candidates[1].ExactRelease).To(BeFalse())
			Expect(archive.requested()).To(Equal([]string{"/release/rel-1"}), "other releases are named, not listed")
			Expect(mbz.requests()).To(Equal([]string{"rg-1"}))
		})

		It("stops at 8 other releases", func() {
			var rels []string
			for i := range 12 {
				rels = append(rels, release(fmt.Sprintf("other-%d", i), "Record", "Official", "", "", "", true))
			}
			mbz.releases["rg-1"] = `{"releases": [` + strings.Join(rels, ",") + `]}`
			cs, err := picker().Candidates(ctx, "al1", CoverSourceCoverArtArchive)
			Expect(err).ToNot(HaveOccurred())
			Expect(cs.Candidates).To(HaveLen(9))
		})

		It("previews and stores another release's front cover through the archive", func() {
			p := picker()
			cs, err := p.Candidates(ctx, "al1", CoverSourceCoverArtArchive)
			Expect(err).ToNot(HaveOccurred())
			deluxe := cs.Candidates[1]
			Expect(deluxe.Description()).To(Equal(`Cover Art Archive: Record (Deluxe) (US, 12" Vinyl, 2009)`))

			_, err = p.Preview(ctx, "al1", deluxe.ID)
			Expect(err).To(HaveOccurred(), "the fake archive has no image there")
			Expect(archive.requested()).To(ContainElement("/release/rel-3/front-250"))
			Expect(archive.userAgents()).To(HaveEach(MatchRegexp(`^VinylUnderground/\S+$`)))
		})

		It("still lists the exact release when browsing the other releases fails or is slow", func() {
			mbz.block = make(chan struct{})
			DeferCleanup(func() { close(mbz.block) })
			timeout = 200 * time.Millisecond

			start := time.Now()
			cs, err := picker().Candidates(ctx, "al1", CoverSourceCoverArtArchive)
			Expect(err).ToNot(HaveOccurred())
			Expect(time.Since(start)).To(BeNumerically("<", 2*time.Second))
			Expect(cs.Failed).To(BeTrue())
			Expect(labels(cs)).To(Equal([]string{"coverartarchive: Record "}))
		})

		It("asks nothing for an album without a MusicBrainz ID", func() {
			album.MbzAlbumID, album.MbzReleaseGroupID = "", ""
			ds.Album(ctx).(*tests.MockAlbumRepo).SetData(model.Albums{album})
			cs, err := picker().Candidates(ctx, "al1", CoverSourceCoverArtArchive)
			Expect(err).ToNot(HaveOccurred())
			Expect(cs.Candidates).To(BeEmpty())
			Expect(cs.Failed).To(BeFalse())
			Expect(archive.requested()).To(BeEmpty())
			Expect(mbz.requests()).To(BeEmpty())
		})

		It("asks nothing when external services are off", func() {
			conf.Server.EnableExternalServices = false
			cs, err := picker().Candidates(ctx, "al1", CoverSourceCoverArtArchive)
			Expect(err).ToNot(HaveOccurred())
			Expect(cs.Candidates).To(BeEmpty())
			Expect(archive.requested()).To(BeEmpty())
		})
	})

	It("stores a real image from a remote candidate", func() {
		red := pngOf(color.RGBA{R: 255, A: 255})
		img := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(red) }))
		DeferCleanup(img.Close)
		ag = imageAgents(&fakeImageAgent{name: "pickerRealImage", imgs: []agents.ExternalImage{{URL: img.URL + "/a.png"}}})

		p := picker()
		cs, err := p.Candidates(ctx, "al1", CoverSourceExternal)
		Expect(err).ToNot(HaveOccurred())
		stored, _, err := p.Store(ctx, "al1", cs.Candidates[0].ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(stored.Mime).To(Equal("image/png"))
	})

	It("rejects an unknown source", func() {
		_, err := picker().Candidates(ctx, "al1", "nope")
		Expect(err).To(MatchError(model.ErrValidation))
	})
})
