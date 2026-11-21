package rip

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeMatcher stands in for the Library search: it knows the Tracks for some "<artist>|<title>".
type fakeMatcher struct {
	have  map[string]model.RipMatches
	err   error
	mu    sync.Mutex
	calls int
}

func (m *fakeMatcher) AlreadyHave(_ context.Context, artist, title string) (model.RipMatches, error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	return m.have[artist+"|"+title], nil
}

func (m *fakeMatcher) searches() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// Before a job is queued, its videos are matched against the Library and the Inbox, and the
// admin is warned about the ones that seem to be there already (#32). The warning never blocks.
var _ = Describe("Ripper's \"already have\" warning", func() {
	const (
		videoURL = "https://www.youtube.com/watch?v=abc"
		listURL  = "https://music.youtube.com/playlist?list=OLAK5uy_x"
	)
	var (
		ctx     context.Context
		ds      *tests.MockDataStore
		fetcher *fakeFetcher
		matcher *fakeMatcher
		ripper  *Ripper
	)

	halo := model.RipMatches{{ID: "mf-1", Title: "Halo", Artist: "Beyoncé", Album: "I Am... Sasha Fierce", Library: "Music"}}
	lucky := model.RipMatches{
		{ID: "mf-2", Title: "Get Lucky", Artist: "Daft Punk", Album: "Random Access Memories", Library: "Music"},
		{ID: "mf-3", Title: "Get Lucky (Official Video)", Artist: "Daft Punk", Library: "Inbox",
			Source: "YouTube", SourceURL: "https://www.youtube.com/watch?v=5NV6Rdv1a3I"},
	}

	BeforeEach(func() {
		ctx = request.WithUser(GinkgoT().Context(), model.User{ID: "admin", IsAdmin: true})
		libs := &tests.MockLibraryRepo{}
		libs.SetData(model.Libraries{{ID: 1, Name: "Music", Path: GinkgoT().TempDir()}})
		libs.PutFn = func(l *model.Library) error {
			if l.ID == 0 {
				l.ID = 2
			}
			libs.Data[l.ID] = *l
			return nil
		}
		ds = &tests.MockDataStore{MockedLibrary: libs}
		_, err := core.EnsureInboxLibrary(ctx, ds, GinkgoT().TempDir())
		Expect(err).ToNot(HaveOccurred())
		fetcher = newFakeFetcher()
		matcher = &fakeMatcher{have: map[string]model.RipMatches{}}
		ripper = New(ds, fetcher, &fakeScanner{}, Options{Matcher: matcher})
	})

	// playlist makes the fake list n items at listURL, by "Band", titled "Track <n>".
	playlist := func(n int) {
		info := &Info{ID: "OLAK5uy_x", Title: "Album", Source: "YouTube", IsPlaylist: true, Count: n}
		for i := 1; i <= n; i++ {
			info.Entries = append(info.Entries, PlaylistEntry{
				URL: fmt.Sprintf("https://music.youtube.com/watch?v=t%d", i), Title: fmt.Sprintf("Track %d", i), Artist: "Band"})
		}
		fetcher.infos[listURL] = info
	}

	needsConfirmation := func(err error) *Preview {
		var confirm *NeedsConfirmationError
		ExpectWithOffset(1, errors.As(err, &confirm)).To(BeTrue(), "error: %v", err)
		return confirm.Preview
	}

	It("warns about a video the Library already seems to have, and queues nothing until told to rip anyway", func() {
		fetcher.infos[videoURL] = &Info{ID: "abc", Title: "Halo", Artist: "Beyoncé", Source: "YouTube"}
		matcher.have["Beyoncé|Halo"] = halo

		_, err := ripper.Enqueue(ctx, videoURL, false)

		p := needsConfirmation(err)
		Expect(p.ConfirmCount).To(BeFalse())
		Expect(p.AlreadyHave).To(Equal([]PreviewItem{{Position: 1, Title: "Beyoncé - Halo", Tracks: halo}}))
		Expect(err.Error()).To(Equal("the Library already seems to have this; rip it anyway?"))
		Expect(ripper.Jobs(ctx)).To(BeEmpty())

		job, err := ripper.Enqueue(ctx, videoURL, true)
		Expect(err).ToNot(HaveOccurred())
		Expect(job.Status).To(Equal(model.RipQueued))
		Expect(job.Items[0].AlreadyHave).To(Equal(halo))
	})

	It("keeps the matches on the queued item, so the job list still says \"already have\"", func() {
		fetcher.infos[videoURL] = &Info{ID: "abc", Title: "Halo", Artist: "Beyoncé", Source: "YouTube"}
		matcher.have["Beyoncé|Halo"] = halo
		_, err := ripper.Enqueue(ctx, videoURL, true)
		Expect(err).ToNot(HaveOccurred())

		jobs, err := ripper.Jobs(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(jobs[0].Items[0].AlreadyHave).To(Equal(halo))
	})

	It("queues straight away when nothing matches", func() {
		job, err := ripper.Enqueue(ctx, videoURL, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(job.Items[0].AlreadyHave).To(BeEmpty())
	})

	It("flags a playlist's matching items one by one, and confirming once queues them all", func() {
		playlist(4)
		matcher.have["Band|Track 2"] = halo
		matcher.have["Band|Track 4"] = lucky

		_, err := ripper.Enqueue(ctx, listURL, false)

		p := needsConfirmation(err)
		Expect(p.Count).To(Equal(4))
		Expect(p.ConfirmCount).To(BeFalse())
		Expect(p.AlreadyHave).To(Equal([]PreviewItem{
			{Position: 2, Title: "Band - Track 2", Tracks: halo},
			{Position: 4, Title: "Band - Track 4", Tracks: lucky},
		}))
		Expect(err.Error()).To(Equal("the Library already seems to have 2 of these 4 items; rip them anyway?"))
		Expect(ripper.Jobs(ctx)).To(BeEmpty())

		job, err := ripper.Enqueue(ctx, listURL, true)
		Expect(err).ToNot(HaveOccurred())
		Expect(job.Items).To(HaveLen(4))
		Expect([]model.RipMatches{job.Items[0].AlreadyHave, job.Items[1].AlreadyHave, job.Items[2].AlreadyHave,
			job.Items[3].AlreadyHave}).To(Equal([]model.RipMatches{nil, halo, nil, lucky}))
	})

	It("asks once for a big playlist with matches: the count and the matches together", func() {
		playlist(ConfirmAbove + 1)
		matcher.have["Band|Track 7"] = halo

		_, err := ripper.Enqueue(ctx, listURL, false)

		p := needsConfirmation(err)
		Expect(p.ConfirmCount).To(BeTrue())
		Expect(p.Titles).To(HaveLen(ConfirmAbove + 1))
		Expect(p.AlreadyHave).To(Equal([]PreviewItem{{Position: 7, Title: "Band - Track 7", Tracks: halo}}))
		Expect(err.Error()).To(Equal("this playlist has 51 items, and the Library already seems to have 1 of them; confirm to rip them all"))

		job, err := ripper.Enqueue(ctx, listURL, true)
		Expect(err).ToNot(HaveOccurred())
		Expect(job.Items[6].AlreadyHave).To(Equal(halo))
	})

	It("says nothing of matches when a big playlist has none", func() {
		playlist(ConfirmAbove + 1)
		_, err := ripper.Enqueue(ctx, listURL, false)
		p := needsConfirmation(err)
		Expect(p.ConfirmCount).To(BeTrue())
		Expect(p.AlreadyHave).To(BeEmpty())
		Expect(err.Error()).To(Equal("this playlist has 51 items; confirm to rip them all"))
	})

	It("doesn't search the Library again for each item when the admin confirms", func() {
		playlist(ConfirmAbove + 1)
		matcher.have["Band|Track 7"] = halo
		_, err := ripper.Enqueue(ctx, listURL, false)
		needsConfirmation(err)
		Expect(matcher.searches()).To(Equal(ConfirmAbove + 1))

		job, err := ripper.Enqueue(ctx, listURL, true)
		Expect(err).ToNot(HaveOccurred())
		Expect(matcher.searches()).To(Equal(ConfirmAbove+1), "the preview's matches are used")
		Expect(job.Items[6].AlreadyHave).To(Equal(halo))

		_, err = ripper.Enqueue(ctx, listURL, true)
		Expect(err).ToNot(HaveOccurred())
		Expect(matcher.searches()).To(Equal(2*(ConfirmAbove+1)), "a later Rip of the same page searches again")
	})

	It("never blocks the Rip when the Library can't be searched, and stops searching", func() {
		playlist(20)
		matcher.err = errors.New("database is locked")
		job, err := ripper.Enqueue(ctx, listURL, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(job.Status).To(Equal(model.RipQueued))
		Expect(matcher.searches()).To(BeNumerically("<", 20))
	})
})
