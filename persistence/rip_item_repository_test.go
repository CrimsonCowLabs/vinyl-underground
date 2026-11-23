package persistence

import (
	"context"
	"time"

	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("RipItemRepository", func() {
	var (
		jobs  model.RipJobRepository
		items model.RipItemRepository
	)

	BeforeEach(func() {
		ctx := log.NewContext(context.TODO())
		jobs = NewRipJobRepository(ctx, GetDBXBuilder())
		items = NewRipItemRepository(ctx, GetDBXBuilder())
		_, err := GetDBXBuilder().NewQuery("DELETE FROM rip_job").Execute()
		Expect(err).ToNot(HaveOccurred())
	})

	newJob := func(url string) *model.RipJob {
		now := time.Now()
		j := &model.RipJob{URL: url, Status: model.RipQueued, UserID: adminUser.ID, CreatedAt: now, UpdatedAt: now}
		Expect(jobs.Put(j)).To(Succeed())
		return j
	}
	newItem := func(job *model.RipJob, pos int, status model.RipStatus) *model.RipItem {
		now := time.Now()
		it := &model.RipItem{JobID: job.ID, Position: pos, URL: job.URL, Status: status, CreatedAt: now, UpdatedAt: now}
		Expect(items.Put(it)).To(Succeed())
		return it
	}

	It("saves a job's items and lists them by job, in order", func() {
		a, b := newJob("https://example.com/a"), newJob("https://example.com/b")
		newItem(a, 2, model.RipQueued)
		first := newItem(a, 1, model.RipQueued)
		newItem(b, 1, model.RipQueued)
		Expect(first.ID).ToNot(BeEmpty())

		got, err := items.ForJobs(a.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(HaveLen(2))
		Expect(got[0].ID).To(Equal(first.ID))
		Expect(got[1].Position).To(Equal(2))

		Expect(items.ForJobs(a.ID, b.ID)).To(HaveLen(3))
		Expect(items.ForJobs()).To(BeEmpty())
	})

	It("updates an item in place", func() {
		it := newItem(newJob("https://example.com/a"), 1, model.RipDownloading)
		it.Status, it.Title, it.Artist, it.Path = model.RipReady, "Song", "Artist", "Album/Artist - Song [x].opus"
		Expect(items.Put(it)).To(Succeed())

		got, err := items.ForJobs(it.JobID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(HaveLen(1))
		Expect(got[0].Status).To(Equal(model.RipReady))
		Expect(got[0].Path).To(Equal("Album/Artist - Song [x].opus"))
		Expect(got[0].Artist).To(Equal("Artist"))
	})

	It("keeps the Tracks the Library already seemed to have for an item", func() {
		it := newItem(newJob("https://example.com/a"), 1, model.RipQueued)
		Expect(items.ForJobs(it.JobID)).To(HaveEach(HaveField("AlreadyHave", BeEmpty())))

		have := model.RipMatches{{ID: "mf-1", Title: "Halo", Artist: "Beyoncé", Album: "I Am... Sasha Fierce",
			Library: "Inbox", Source: "YouTube", SourceURL: "https://www.youtube.com/watch?v=x"}}
		it.AlreadyHave = have
		Expect(items.Put(it)).To(Succeed())

		got, err := items.ForJobs(it.JobID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got[0].AlreadyHave).To(Equal(have))
	})

	It("keeps what the site said about an item's video", func() {
		it := newItem(newJob("https://example.com/a"), 1, model.RipQueued)
		it.Origin = model.RipOrigin{Title: "Song (Official Video)", Uploader: "Band - Topic", Duration: 201.5,
			Artist: "Band", Track: "Song", Album: "Record", ReleaseYear: 1999}
		Expect(items.Put(it)).To(Succeed())

		got, err := items.ForJobs(it.JobID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got[0].Origin).To(Equal(it.Origin))
	})

	It("saves an item's proposal on its own, and Put never undoes it", func() {
		it := newItem(newJob("https://example.com/a"), 1, model.RipReady)
		Expect(items.ForJobs(it.JobID)).To(HaveEach(HaveField("Proposal.Status", model.RipProposalNone)))

		p := model.RipProposal{Status: model.RipProposalReady, Title: "Song", Artist: "Band", Album: "Record",
			AlbumArtist: "Band", Year: "1999", From: "title", AlbumFrom: "musicbrainz", ReleaseType: "Album",
			ReleaseGroupID: "rg-1", UpdatedAt: time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)}
		Expect(items.SetProposal(it.ID, p)).To(Succeed())

		// The Ripper saving its own copy of the item, without the proposal, keeps it.
		it.Title = "Song (Official Video)"
		Expect(items.Put(it)).To(Succeed())

		got, err := items.ForJobs(it.JobID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got[0].Title).To(Equal("Song (Official Video)"))
		Expect(got[0].Proposal).To(Equal(p))
	})

	It("doesn't bring back a discarded item when saving its proposal", func() {
		it := newItem(newJob("https://example.com/a"), 1, model.RipReady)
		Expect(items.Delete(it.ID)).To(Succeed())
		Expect(items.SetProposal(it.ID, model.RipProposal{Status: model.RipProposalReady})).To(MatchError(model.ErrNotFound))
		Expect(items.ForJobs(it.JobID)).To(BeEmpty())
	})

	It("finds the items of the Files at some paths, of any job", func() {
		a := newItem(newJob("https://example.com/a"), 1, model.RipReady)
		a.Path = "A/01.opus"
		Expect(items.Put(a)).To(Succeed())
		b := newItem(newJob("https://example.com/b"), 1, model.RipReady)
		b.Path = "B/01.opus"
		Expect(items.Put(b)).To(Succeed())
		c := newItem(newJob("https://example.com/c"), 1, model.RipReady)
		c.Path = "C/01.opus"
		Expect(items.Put(c)).To(Succeed())

		got, err := items.ForPaths("A/01.opus", "C/01.opus", "D/01.opus")
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(ConsistOf(HaveField("ID", a.ID), HaveField("ID", c.ID)))
		Expect(items.ForPaths()).To(BeEmpty())
	})

	It("moves a promoted Track's \"already have\" snapshots to its new library, and no other's", func() {
		j := newJob("https://example.com/a")
		a, b := newItem(j, 1, model.RipQueued), newItem(j, 2, model.RipQueued)
		a.AlreadyHave = model.RipMatches{{ID: "mf_1", Title: "Halo", Library: "Inbox"}, {ID: "mf-2", Library: "Inbox"}}
		b.AlreadyHave = model.RipMatches{{ID: "mf-1", Title: "Halo", Library: "Inbox"}}
		Expect(items.Put(a)).To(Succeed())
		Expect(items.Put(b)).To(Succeed())

		Expect(items.MoveMatches("mf-1", "Music")).To(Succeed())

		got, err := items.ForJobs(j.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got[0].AlreadyHave).To(Equal(model.RipMatches{{ID: "mf_1", Title: "Halo", Library: "Inbox"}, {ID: "mf-2", Library: "Inbox"}}))
		Expect(got[1].AlreadyHave).To(Equal(model.RipMatches{{ID: "mf-1", Title: "Halo", Library: "Music"}}))
	})

	It("puts interrupted items back in the queue, and leaves the rest alone", func() {
		j := newJob("https://example.com/a")
		newItem(j, 1, model.RipReady)
		newItem(j, 2, model.RipDownloading)
		newItem(j, 3, model.RipFailed)

		Expect(items.RequeueInterrupted()).To(Equal(int64(1)))
		got, err := items.ForJobs(j.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect([]model.RipStatus{got[0].Status, got[1].Status, got[2].Status}).To(Equal(
			[]model.RipStatus{model.RipReady, model.RipQueued, model.RipFailed}))
	})

	It("deletes a job's items with the job", func() {
		j := newJob("https://example.com/a")
		newItem(j, 1, model.RipReady)
		_, err := GetDBXBuilder().NewQuery("DELETE FROM rip_job").Execute()
		Expect(err).ToNot(HaveOccurred())
		Expect(items.ForJobs(j.ID)).To(BeEmpty())
	})

	It("deletes one item, and keeps the job's others", func() {
		j := newJob("https://example.com/a")
		gone := newItem(j, 1, model.RipReady)
		kept := newItem(j, 2, model.RipReady)

		Expect(items.Delete(gone.ID)).To(Succeed())

		got, err := items.ForJobs(j.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(HaveLen(1))
		Expect(got[0].ID).To(Equal(kept.ID))
		Expect(items.Delete(gone.ID)).To(MatchError(model.ErrNotFound))
	})

	It("deletes a job with its items through the job repository", func() {
		j := newJob("https://example.com/a")
		other := newJob("https://example.com/b")
		newItem(j, 1, model.RipReady)
		newItem(other, 1, model.RipReady)

		Expect(jobs.Delete(j.ID)).To(Succeed())

		_, err := jobs.Get(j.ID)
		Expect(err).To(MatchError(model.ErrNotFound))
		Expect(items.ForJobs(j.ID)).To(BeEmpty())
		Expect(items.ForJobs(other.ID)).To(HaveLen(1))
		Expect(jobs.Delete(j.ID)).To(MatchError(model.ErrNotFound))
	})

	It("lists the path of every item's File, of any job", func() {
		a, b := newJob("https://example.com/a"), newJob("https://example.com/b")
		newItem(a, 1, model.RipQueued)
		landed := newItem(a, 2, model.RipReady)
		landed.Path = "Album/02.opus"
		Expect(items.Put(landed)).To(Succeed())
		other := newItem(b, 1, model.RipReady)
		other.Path = "Single.m4a"
		Expect(items.Put(other)).To(Succeed())

		Expect(items.Paths()).To(ConsistOf("Album/02.opus", "Single.m4a"))
	})

	It("saves an item's matches alone", func() {
		it := newItem(newJob("https://example.com/a"), 1, model.RipReady)
		have := model.RipMatches{{ID: "mf-1", Title: "Song", Artist: "Band", Library: "Music"}}
		Expect(items.SetAlreadyHave(it.ID, have)).To(Succeed())

		got, err := items.ForJobs(it.JobID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got[0].AlreadyHave).To(Equal(have))
		Expect(items.SetAlreadyHave("gone", have)).To(MatchError(model.ErrNotFound))
	})
})
