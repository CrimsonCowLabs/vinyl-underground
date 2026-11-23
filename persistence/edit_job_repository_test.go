package persistence

import (
	"context"
	"time"

	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("EditJobRepository", func() {
	var repo model.EditJobRepository

	BeforeEach(func() {
		repo = NewEditJobRepository(log.NewContext(context.TODO()), GetDBXBuilder())
		for _, t := range []string{"edit_job_step", "edit_job"} {
			_, err := GetDBXBuilder().NewQuery("DELETE FROM " + t).Execute()
			Expect(err).ToNot(HaveOccurred())
		}
	})

	It("keeps a job and its steps, in order", func() {
		job := &model.EditJob{Kind: model.EditTags, State: model.EditJobQueued, UserID: adminUser.ID, Total: 2}
		Expect(repo.Put(job)).To(Succeed())
		Expect(job.ID).ToNot(BeEmpty())
		second := &model.EditJobStep{JobID: job.ID, Position: 2, Kind: model.EditTags, Path: "b.mp3", Args: `{"title":["B"]}`, State: model.EditStepQueued}
		first := &model.EditJobStep{JobID: job.ID, Position: 1, Kind: model.EditTags, Path: "a.mp3", Args: `{"title":["A"]}`, State: model.EditStepQueued}
		Expect(repo.PutStep(second)).To(Succeed())
		Expect(repo.PutStep(first)).To(Succeed())

		first.State, first.Code, first.Error = model.EditStepSkipped, "changedOnDisk", "the File changed on disk"
		Expect(repo.PutStep(first)).To(Succeed())

		got, err := repo.Get(job.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Total).To(Equal(2))
		steps, err := repo.Steps(job.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(steps).To(HaveLen(2))
		Expect(steps[0].Path).To(Equal("a.mp3"))
		Expect(steps[0].Code).To(Equal("changedOnDisk"))
		Expect(steps[1].Args).To(Equal(`{"title":["B"]}`))

		skipped, err := repo.Steps(job.ID, model.EditStepSkipped)
		Expect(err).ToNot(HaveOccurred())
		Expect(skipped).To(HaveLen(1))
		Expect(skipped[0].Path).To(Equal("a.mp3"))
	})

	It("hands out an interrupted job first, then the oldest queued", func() {
		Expect(repo.Next()).Error().To(MatchError(model.ErrNotFound))
		old := &model.EditJob{Kind: model.EditTags, State: model.EditJobQueued, CreatedAt: time.Now().Add(-time.Hour)}
		newer := &model.EditJob{Kind: model.EditTags, State: model.EditJobQueued}
		done := &model.EditJob{Kind: model.EditTags, State: model.EditJobDone, CreatedAt: time.Now().Add(-2 * time.Hour)}
		for _, j := range []*model.EditJob{old, newer, done} {
			Expect(repo.Put(j)).To(Succeed())
		}
		next, err := repo.Next()
		Expect(err).ToNot(HaveOccurred())
		Expect(next.ID).To(Equal(old.ID))

		newer.State = model.EditJobRunning
		Expect(repo.Put(newer)).To(Succeed())
		next, err = repo.Next()
		Expect(err).ToNot(HaveOccurred())
		Expect(next.ID).To(Equal(newer.ID))
	})

	It("lists the latest jobs, newest first", func() {
		for i := range 3 {
			Expect(repo.Put(&model.EditJob{Kind: model.EditTags, State: model.EditJobDone,
				CreatedAt: time.Now().Add(time.Duration(i) * time.Minute), Current: string(rune('a' + i))})).To(Succeed())
		}
		jobs, err := repo.GetAll(2)
		Expect(err).ToNot(HaveOccurred())
		Expect(jobs).To(HaveLen(2))
		Expect(jobs[0].Current).To(Equal("c"))
		Expect(jobs[1].Current).To(Equal("b"))
	})
})
