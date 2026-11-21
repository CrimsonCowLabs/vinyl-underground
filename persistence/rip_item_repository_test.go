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
})
