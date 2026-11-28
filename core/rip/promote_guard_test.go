package rip

import (
	"context"

	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// A write to an Inbox album's Files from outside the card (a MusicBrainz release applied from it,
// #219) waits for no Promote: it is refused while one moves the album's Files, as the card's own
// changes are.
var _ = Describe("UnlessPromoting", func() {
	const inboxID = 2
	var (
		ctx     context.Context
		ds      *tests.MockDataStore
		ripper  *Ripper
		promote *model.EditJob
		queued  int
	)
	queue := func() error { queued++; return nil }

	BeforeEach(func() {
		ctx = request.WithUser(GinkgoT().Context(), model.User{ID: "admin", IsAdmin: true})
		libs := &tests.MockLibraryRepo{}
		libs.SetData(model.Libraries{{ID: 1, Name: "Music", Path: GinkgoT().TempDir()}})
		libs.PutFn = func(l *model.Library) error {
			if l.ID == 0 {
				l.ID = inboxID
			}
			libs.Data[l.ID] = *l
			return nil
		}
		ds = &tests.MockDataStore{MockedLibrary: libs}
		_, err := core.EnsureInboxLibrary(ctx, ds, GinkgoT().TempDir())
		Expect(err).ToNot(HaveOccurred())
		ripper = New(ds, newFakeFetcher(), nil, Options{})
		ripper.SetEditQueue(editjob.New(ds, nil, nil))
		queued = 0

		ds.MediaFile(ctx).(*tests.MockMediaFileRepo).SetData(model.MediaFiles{
			{ID: "mf-a", LibraryID: inboxID, AlbumID: "al-1", Path: "Album/a.opus"}})
		promote = &model.EditJob{Kind: model.EditPromote, State: model.EditJobRunning}
		Expect(ds.EditJob(ctx).Put(promote)).To(Succeed())
		step, err := editjob.MoveStep(tagedit.Move{LibraryID: inboxID, NewLibraryID: 1, TrackID: "mf-a",
			Path: "Album/a.opus", NewPath: "A/B (2020)/01 A.opus"})
		Expect(err).ToNot(HaveOccurred())
		step.JobID = promote.ID
		Expect(ds.EditJob(ctx).PutStep(&step)).To(Succeed())
	})

	It("refuses while a Promote moving one of the album's Files is queued or running, and queues nothing", func() {
		Expect(ripper.UnlessPromoting(ctx, "al-1", queue)).To(MatchError(ErrPromoting))
		Expect(queued).To(BeZero())
	})

	It("queues the write once the Promote is done", func() {
		promote.State = model.EditJobDone
		Expect(ds.EditJob(ctx).Put(promote)).To(Succeed())
		Expect(ripper.UnlessPromoting(ctx, "al-1", queue)).To(Succeed())
		Expect(queued).To(Equal(1))
	})

	It("queues the write of another album, none of whose Files the Promote moves", func() {
		ds.MediaFile(ctx).(*tests.MockMediaFileRepo).SetData(model.MediaFiles{
			{ID: "mf-b", LibraryID: inboxID, AlbumID: "al-2", Path: "Other/b.opus"}})
		Expect(ripper.UnlessPromoting(ctx, "al-2", queue)).To(Succeed())
		Expect(queued).To(Equal(1))
	})

	It("passes on the write's own refusal", func() {
		promote.State = model.EditJobDone
		Expect(ds.EditJob(ctx).Put(promote)).To(Succeed())
		Expect(ripper.UnlessPromoting(ctx, "al-1", func() error { return tagedit.ErrEditingOff })).To(MatchError(tagedit.ErrEditingOff))
	})
})
