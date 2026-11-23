package nativeapi

import (
	"context"
	"strings"

	"github.com/navidrome/navidrome/core/corrections"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeWrite is the step kind of the fake Corrections' plans: it writes nothing.
const fakeWrite model.EditKind = "fakeWrite"

// fakeWriteKind writes nothing: a step of a Track "skip-..." is skipped as changed on disk, and
// with hold set each step waits for it.
type fakeWriteKind struct{ hold chan struct{} }

func (fakeWriteKind) Check(context.Context, *model.EditJobStep) error { return nil }

func (k fakeWriteKind) Run(_ context.Context, _ string, step *model.EditJobStep) (editjob.Outcome, error) {
	if k.hold != nil {
		<-k.hold
	}
	if strings.HasPrefix(step.MediaFileID, "skip") {
		return editjob.Outcome{}, tagedit.ErrChangedOnDisk
	}
	return editjob.Outcome{Changed: true, EntryID: "entry-" + step.MediaFileID}, nil
}

func (fakeWriteKind) Code(err error) string { return tagedit.Code(err) }

// fakePlan is a plan of a job of the kind writing the Tracks.
func fakePlan(kind model.EditKind, trackIDs ...string) *corrections.Plan {
	plan := &corrections.Plan{Kind: kind, Args: "{}", TrackID: trackIDs[0], ArtistID: "new-artist"}
	for _, id := range trackIDs {
		plan.Steps = append(plan.Steps, model.EditJobStep{Kind: fakeWrite, MediaFileID: id, Path: id + ".mp3"})
	}
	return plan
}

func (f *fakeCorrections) planned(call string) (*corrections.Plan, error) {
	f.plans = append(f.plans, call)
	if f.planErr != nil {
		return nil, f.planErr
	}
	if f.plan == nil {
		return fakePlan(corrections.JobAlbumEdit, "mf-1"), nil
	}
	return f.plan, nil
}

func (f *fakeCorrections) PlanAlbumEdit(_ context.Context, albumID string, fix *corrections.AlbumFix, numbers []corrections.TrackNumber) (*corrections.Plan, error) {
	call := "album " + albumID
	if fix != nil {
		call += " " + fix.AlbumArtist + "|" + fix.Name
	}
	for _, n := range numbers {
		call += " " + n.ID
	}
	return f.planned(call)
}

func (f *fakeCorrections) PlanArtistRename(_ context.Context, artistID, name string) (*corrections.Plan, error) {
	return f.planned("rename " + artistID + "->" + name)
}

func (f *fakeCorrections) PlanArtistMerge(_ context.Context, artistID, intoID string) (*corrections.Plan, error) {
	return f.planned("merge " + artistID + "->" + intoID)
}

// runEditQueue gives the router an edit job queue writing fake steps, run until the spec ends,
// with the job kinds handle registers.
func runEditQueue(api *Router, ds model.DataStore, k fakeWriteKind, handle ...func(q *editjob.Queue)) {
	q := editjob.New(ds, nil, map[model.EditKind]editjob.StepKind{fakeWrite: k})
	for _, h := range handle {
		h(q)
	}
	api.SetEditQueue(q)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { q.Run(ctx); close(done) }()
	DeferCleanup(func() {
		cancel()
		if k.hold != nil {
			close(k.hold)
		}
		Eventually(done).Should(BeClosed())
	})
}
