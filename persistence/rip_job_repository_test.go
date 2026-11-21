package persistence

import (
	"context"
	"time"

	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("RipJobRepository", func() {
	var repo model.RipJobRepository

	BeforeEach(func() {
		repo = NewRipJobRepository(log.NewContext(context.TODO()), GetDBXBuilder())
		_, err := GetDBXBuilder().NewQuery("DELETE FROM rip_job").Execute()
		Expect(err).ToNot(HaveOccurred())
	})

	newJob := func(url string, status model.RipStatus, created time.Time) *model.RipJob {
		j := &model.RipJob{URL: url, Status: status, UserID: adminUser.ID, CreatedAt: created, UpdatedAt: created}
		Expect(repo.Put(j)).To(Succeed())
		return j
	}

	It("saves a job with a new ID and reads it back", func() {
		j := newJob("https://example.com/a", model.RipQueued, time.Now())
		Expect(j.ID).ToNot(BeEmpty())

		got, err := repo.Get(j.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.URL).To(Equal("https://example.com/a"))
		Expect(got.Status).To(Equal(model.RipQueued))
	})

	It("reports ErrNotFound for an unknown job", func() {
		_, err := repo.Get("nope")
		Expect(err).To(MatchError(model.ErrNotFound))
	})

	It("updates a job in place", func() {
		j := newJob("https://example.com/a", model.RipQueued, time.Now())
		j.Status = model.RipFailed
		j.Error = "ERROR: Video unavailable"
		Expect(repo.Put(j)).To(Succeed())

		got, err := repo.Get(j.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Status).To(Equal(model.RipFailed))
		Expect(got.Error).To(Equal("ERROR: Video unavailable"))
		Expect(repo.GetAll()).To(HaveLen(1))
	})

	It("lists jobs newest first", func() {
		t := time.Now()
		newJob("https://example.com/old", model.RipReady, t.Add(-time.Hour))
		newJob("https://example.com/new", model.RipQueued, t)

		all, err := repo.GetAll()
		Expect(err).ToNot(HaveOccurred())
		Expect(all).To(HaveLen(2))
		Expect(all[0].URL).To(Equal("https://example.com/new"))
	})

	It("hands out the oldest queued job next", func() {
		t := time.Now()
		newJob("https://example.com/done", model.RipReady, t.Add(-3*time.Hour))
		newJob("https://example.com/second", model.RipQueued, t.Add(-time.Hour))
		newJob("https://example.com/first", model.RipQueued, t.Add(-2*time.Hour))

		next, err := repo.NextQueued()
		Expect(err).ToNot(HaveOccurred())
		Expect(next.URL).To(Equal("https://example.com/first"))
	})

	It("reports ErrNotFound when nothing is queued", func() {
		newJob("https://example.com/done", model.RipReady, time.Now())
		_, err := repo.NextQueued()
		Expect(err).To(MatchError(model.ErrNotFound))
	})

	It("puts interrupted jobs back in the queue", func() {
		t := time.Now()
		j := newJob("https://example.com/a", model.RipDownloading, t)
		newJob("https://example.com/b", model.RipFailed, t)

		Expect(repo.RequeueInterrupted()).To(Equal(int64(1)))
		got, err := repo.Get(j.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Status).To(Equal(model.RipQueued))
	})

	It("saves a job's kind, a Rip when none is given", func() {
		rip := newJob("https://example.com/a", model.RipReady, time.Now())
		up := &model.RipJob{Kind: model.RipKindUpload, Status: model.RipReady, UserID: adminUser.ID, Folder: "Upload"}
		Expect(repo.Put(up)).To(Succeed())

		got, err := repo.Get(rip.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Kind).To(Equal(model.RipKindRip))
		got, err = repo.Get(up.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Kind).To(Equal(model.RipKindUpload))
	})

	It("never hands out, or re-queues, an upload or a drop", func() {
		t := time.Now()
		for _, kind := range []model.RipKind{model.RipKindUpload, model.RipKindDrop} {
			for _, status := range []model.RipStatus{model.RipQueued, model.RipDownloading} {
				Expect(repo.Put(&model.RipJob{Kind: kind, Status: status, UserID: adminUser.ID, CreatedAt: t.Add(-time.Hour)})).To(Succeed())
			}
		}
		_, err := repo.NextQueued()
		Expect(err).To(MatchError(model.ErrNotFound))
		Expect(repo.RequeueInterrupted()).To(Equal(int64(0)))

		newJob("https://example.com/rip", model.RipQueued, t)
		next, err := repo.NextQueued()
		Expect(err).ToNot(HaveOccurred())
		Expect(next.URL).To(Equal("https://example.com/rip"))
	})
})
