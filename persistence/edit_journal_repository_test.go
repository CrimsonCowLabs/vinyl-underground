package persistence

import (
	"context"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("EditJournalRepository", func() {
	var repo model.EditJournalRepository

	BeforeEach(func() {
		repo = NewEditJournalRepository(log.NewContext(context.TODO()), GetDBXBuilder())
		_, err := GetDBXBuilder().NewQuery("DELETE FROM edit_journal").Execute()
		Expect(err).ToNot(HaveOccurred())
	})

	It("keeps an entry's original tags and changes, a removed tag included", func() {
		e := &model.EditEntry{Kind: model.EditTags, State: model.EditPending, UserID: adminUser.ID, LibraryID: 1,
			MediaFileID: "mf-1", Path: "Artist/Album/01 Song.mp3", NewPath: "Artist/Album/01 Song.mp3",
			OriginalTags: model.EditTagValues{"TITLE": {"Song"}, "GENRE": {"Rock", "Pop"}, "COMMENT": {"old"}},
			Changes:      model.EditTagValues{"TITLE": {"Song (Live)"}, "COMMENT": {}}}
		Expect(repo.Put(e)).To(Succeed())
		Expect(e.ID).ToNot(BeEmpty())

		got, err := repo.Get(e.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.OriginalTags).To(Equal(e.OriginalTags))
		Expect(got.Changes).To(HaveKeyWithValue("TITLE", []string{"Song (Live)"}))
		Expect(got.Changes).To(HaveKey("COMMENT"))
		Expect(got.Changes["COMMENT"]).To(BeEmpty())
		Expect(got.Kind).To(Equal(model.EditTags))
		Expect(got.CreatedAt).ToNot(BeZero())
	})

	It("finds the latest write that left a File at a path", func() {
		Expect(repo.LastWritten(1, "a.mp3")).Error().To(MatchError(model.ErrNotFound))
		older := &model.EditEntry{Kind: model.EditTags, State: model.EditWritten, LibraryID: 1, Path: "a.mp3", NewPath: "a.mp3", CopySum: "old"}
		Expect(repo.Put(older)).To(Succeed())
		newer := &model.EditEntry{Kind: model.EditTags, State: model.EditWritten, LibraryID: 1, Path: "a.mp3", NewPath: "a.mp3", CopySum: "new"}
		Expect(repo.Put(newer)).To(Succeed())
		failed := &model.EditEntry{Kind: model.EditTags, State: model.EditFailed, LibraryID: 1, Path: "a.mp3", NewPath: "a.mp3", CopySum: "failed"}
		Expect(repo.Put(failed)).To(Succeed())
		Expect(repo.Put(&model.EditEntry{Kind: model.EditTags, State: model.EditWritten, LibraryID: 2, Path: "a.mp3", NewPath: "a.mp3"})).To(Succeed())

		got, err := repo.LastWritten(1, "a.mp3")
		Expect(err).ToNot(HaveOccurred())
		Expect(got.CopySum).To(Equal("new"))
	})

	It("lists the entries of an edit job, oldest first", func() {
		a := &model.EditEntry{JobID: "job-1", Kind: model.EditTags, State: model.EditWritten, LibraryID: 1, Path: "a.mp3"}
		other := &model.EditEntry{JobID: "job-2", Kind: model.EditTags, State: model.EditWritten, LibraryID: 1, Path: "b.mp3"}
		b := &model.EditEntry{JobID: "job-1", Kind: model.EditTags, State: model.EditFailed, LibraryID: 1, Path: "c.mp3"}
		for _, e := range []*model.EditEntry{a, other, b} {
			Expect(repo.Put(e)).To(Succeed())
		}
		got, err := repo.ForJob("job-1")
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(HaveLen(2))
		// The Edit history counts a job's entries this way.
		Expect(repo.CountAll(model.QueryOptions{Filters: squirrel.Eq{"job_id": "job-1"}})).To(BeEquivalentTo(2))
		Expect(got[0].Path).To(Equal("a.mp3"))
		Expect(got[1].Path).To(Equal("c.mp3"))
	})

	It("lists the entries still pending, oldest first", func() {
		a := &model.EditEntry{Kind: model.EditTags, State: model.EditPending, LibraryID: 1, Path: "a.mp3"}
		b := &model.EditEntry{Kind: model.EditTags, State: model.EditPending, LibraryID: 1, Path: "b.mp3"}
		done := &model.EditEntry{Kind: model.EditTags, State: model.EditPending, LibraryID: 1, Path: "c.mp3"}
		for _, e := range []*model.EditEntry{a, b, done} {
			Expect(repo.Put(e)).To(Succeed())
		}
		done.State = model.EditWritten
		Expect(repo.Put(done)).To(Succeed())

		got, err := repo.Pending()
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(HaveLen(2))
		Expect(got[0].Path).To(Equal("a.mp3"))
		Expect(got[1].Path).To(Equal("b.mp3"))
	})

	It("finds the writes to the same File recorded after an entry, oldest first", func() {
		put := func(e model.EditEntry) *model.EditEntry {
			GinkgoHelper()
			e.Kind = model.EditTags
			Expect(repo.Put(&e)).To(Succeed())
			return &e
		}
		put(model.EditEntry{State: model.EditWritten, LibraryID: 1, MediaFileID: "mf-1", Path: "a.mp3", NewPath: "a.mp3"})
		entry := put(model.EditEntry{State: model.EditWritten, LibraryID: 1, MediaFileID: "mf-1", Path: "a.mp3", NewPath: "a.mp3"})
		later := put(model.EditEntry{State: model.EditWritten, LibraryID: 1, MediaFileID: "mf-1", Path: "a.mp3", NewPath: "a.mp3"})
		put(model.EditEntry{State: model.EditFailed, LibraryID: 1, MediaFileID: "mf-1", Path: "a.mp3", NewPath: "a.mp3"})
		put(model.EditEntry{State: model.EditWritten, LibraryID: 1, MediaFileID: "mf-2", Path: "b.mp3", NewPath: "b.mp3"})
		put(model.EditEntry{State: model.EditWritten, LibraryID: 2, MediaFileID: "mf-3", Path: "a.mp3", NewPath: "a.mp3"})
		// The same File, found by its path when the Track isn't known, and a pending write.
		byPath := put(model.EditEntry{State: model.EditPending, LibraryID: 1, Path: "a.mp3", NewPath: "a.mp3"})

		got, err := repo.After(entry)
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(HaveLen(2))
		Expect(got[0].ID).To(Equal(later.ID))
		Expect(got[1].ID).To(Equal(byPath.ID))
	})

	It("lists every entry newest first, a page at a time, and counts them", func() {
		for _, p := range []string{"a.mp3", "b.mp3", "c.mp3"} {
			Expect(repo.Put(&model.EditEntry{Kind: model.EditTags, State: model.EditWritten, LibraryID: 1, Path: p, NewPath: p})).To(Succeed())
		}

		got, err := repo.GetAll(model.QueryOptions{Max: 2})
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(HaveLen(2))
		Expect(got[0].Path).To(Equal("c.mp3"))
		Expect(got[1].Path).To(Equal("b.mp3"))
		got, err = repo.GetAll(model.QueryOptions{Max: 2, Offset: 2})
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(HaveLen(1))
		Expect(got[0].Path).To(Equal("a.mp3"))
		Expect(repo.CountAll()).To(BeNumerically("==", 3))
	})
})
