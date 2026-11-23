package persistence

import (
	"context"
	"time"

	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// An artist rename or merge job's annotations (#163): kept before its Files are written, merged
// into the new name once they are, and split again by its undo.
var _ = Describe("EditJobRepository artist annotations", func() {
	const from, to = "job-artist-from", "job-artist-into"
	var (
		repo  model.EditJobRepository
		jobID string
		since = time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	)

	type ann struct {
		PlayCount int
		Rating    int
		Starred   bool
	}
	exec := func(sql string, args ...any) {
		GinkgoHelper()
		_, err := GetDBXBuilder().NewQuery(sql).Bind(nil).Execute()
		if len(args) > 0 {
			_, err = GetDBXBuilder().NewQuery(sql).Bind(args[0].(map[string]any)).Execute()
		}
		Expect(err).ToNot(HaveOccurred())
	}
	annotate := func(user, artist string, a ann) {
		GinkgoHelper()
		exec(`insert into annotation (user_id, item_id, item_type, play_count, rating, starred, starred_at, rated_at)
			values ({:user}, {:item}, 'artist', {:plays}, {:rating}, {:starred}, {:at}, {:at})`,
			map[string]any{"user": user, "item": artist, "plays": a.PlayCount, "rating": a.Rating, "starred": a.Starred, "at": since})
	}
	get := func(user, artist string) *ann {
		GinkgoHelper()
		var a ann
		err := GetDBXBuilder().NewQuery(`select coalesce(play_count, 0) as play_count, coalesce(rating, 0) as rating,
			coalesce(starred, 0) as starred from annotation where user_id = {:user} and item_id = {:item} and item_type = 'artist'`).
			Bind(map[string]any{"user": user, "item": artist}).Row(&a.PlayCount, &a.Rating, &a.Starred)
		if err != nil {
			return nil
		}
		return &a
	}

	BeforeEach(func() {
		repo = NewEditJobRepository(log.NewContext(context.TODO()), GetDBXBuilder())
		exec("delete from annotation where item_id in ('" + from + "', '" + to + "')")
		job := &model.EditJob{Kind: "artistMerge", State: model.EditJobQueued}
		Expect(repo.Put(job)).To(Succeed())
		jobID = job.ID
		DeferCleanup(func() { exec("delete from annotation where item_id in ('" + from + "', '" + to + "')") })
	})

	It("merges the kept stars and ratings into the other Artist, once, and the undo splits them again", func() {
		annotate(adminUser.ID, from, ann{PlayCount: 3, Rating: 4, Starred: true})
		annotate(regularUser.ID, from, ann{PlayCount: 2, Rating: 5})
		annotate(adminUser.ID, to, ann{PlayCount: 10, Rating: 2})

		Expect(repo.KeepArtistAnnotations(jobID, from)).To(Succeed())
		// The scans after the job's Files delete the renamed Artist, and its annotations.
		exec("delete from annotation where item_id = '" + from + "'")

		Expect(repo.MergeArtistAnnotations(jobID, from, to)).To(Succeed())
		// Play counts are the scanner's: it counts them from the Tracks.
		Expect(get(adminUser.ID, to)).To(Equal(&ann{PlayCount: 10, Rating: 2, Starred: true}))
		Expect(get(regularUser.ID, to)).To(Equal(&ann{Rating: 5}))
		// A restart may run it again: it merges once.
		Expect(repo.MergeArtistAnnotations(jobID, from, to)).To(Succeed())
		Expect(get(adminUser.ID, to)).To(Equal(&ann{PlayCount: 10, Rating: 2, Starred: true}))

		// The undo's rescans bring the old Artist back; it gets its own back, and the other Artist
		// keeps only what it had.
		Expect(repo.SplitArtistAnnotations(jobID, from, to, false)).To(Succeed())
		Expect(get(adminUser.ID, from)).To(Equal(&ann{Rating: 4, Starred: true}))
		Expect(get(regularUser.ID, from)).To(Equal(&ann{Rating: 5}))
		Expect(get(adminUser.ID, to)).To(Equal(&ann{PlayCount: 10, Rating: 2}))
		Expect(get(regularUser.ID, to)).To(Equal(&ann{}))
	})

	It("takes the star from what the renamed Artist has left when it merges", func() {
		annotate(adminUser.ID, from, ann{PlayCount: 1, Starred: true})
		Expect(repo.KeepArtistAnnotations(jobID, from)).To(Succeed())
		Expect(repo.MergeArtistAnnotations(jobID, from, to)).To(Succeed())
		Expect(get(adminUser.ID, from)).To(Equal(&ann{PlayCount: 1}))
		Expect(get(adminUser.ID, to)).To(Equal(&ann{Starred: true}))
	})

	It("gives a renamed Artist back all its new name got, when undoing a rename", func() {
		annotate(adminUser.ID, from, ann{Starred: true})
		Expect(repo.KeepArtistAnnotations(jobID, from)).To(Succeed())
		exec("delete from annotation where item_id = '" + from + "'")
		Expect(repo.MergeArtistAnnotations(jobID, from, to)).To(Succeed())
		// Rated under the new name.
		exec(`update annotation set rating = 3 where item_id = '` + to + `'`)

		Expect(repo.SplitArtistAnnotations(jobID, from, to, true)).To(Succeed())
		Expect(get(adminUser.ID, from)).To(Equal(&ann{Rating: 3, Starred: true}))
		Expect(get(adminUser.ID, to)).To(Equal(&ann{}))
	})
})
