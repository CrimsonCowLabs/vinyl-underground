package rip

import (
	"cmp"
	"context"
	"errors"
	"time"

	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/librarymanager"
	"github.com/navidrome/navidrome/core/organize"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// Promote (#171, ADR-0011) is Organize from the Inbox: a reviewed job's Files move from the Inbox
// straight to their place in the Library's organised tree, `<Album Artist>/<Album> (<Year>)/<NN>
// <Title>.<ext>` (core/libpath), through Organize's plan (the same collision rules, case-insensitive
// folder reuse and reasons a File can't be placed) and as one edit job of journaled moves
// (model.EditPromote). Each Track keeps its ID, so its favorites, ratings, plays, Playlist
// entries, bookmarks, play queue, shares and Station history stay attached; the move changes its
// library and path (tagedit.Editor.MoveInJob). The job can be undone from the Edit history, which
// puts the Files back in the Inbox. A restart in the middle is settled by the Edit journal's
// startup settle, as any move is.
//
// Promote is off while editing is (no writable Library, ADR-0011): there is no edit queue then
// (SetEditQueue), and no Library to promote into (organize.PromoteLibrary).

var (
	// ErrPromoteOff is returned while the Library isn't writable (editing is off).
	ErrPromoteOff = errors.New("Promote is off: the Library isn't writable")
	// ErrNothingToPromote is returned for a job with no File in the Inbox scanned yet.
	ErrNothingToPromote = errors.New("this Rip has no Files to promote")
)

// ReasonNotScanned is why a Promote leaves one of a job's Files in the Inbox when the Inbox scan
// hasn't read it yet (it has no Track to promote). Promote it once the scan has.
const ReasonNotScanned = "notScanned"

// SetEditQueue gives the Ripper the edit job queue Promote's jobs run on. With none (editing
// off), Promote is off.
func (r *Ripper) SetEditQueue(q *editjob.Queue) { r.editQueue = q }

// PromoteEnabled reports whether Promote is on: the Library is writable and there is an edit
// queue to run it.
func (r *Ripper) PromoteEnabled(ctx context.Context) bool {
	if r.editQueue == nil {
		return false
	}
	lib, err := organize.PromoteLibrary(ctx, r.ds)
	return err == nil && lib != nil
}

// PromotePlan is what Promote would do to the job's Files (see organize.Plan): where each one
// lands in the Library, and why any can't be placed. Nothing is written.
func (r *Ripper) PromotePlan(ctx context.Context, jobID string) (*organize.Plan, error) {
	scope, notScanned, err := r.promoteScope(ctx, jobID)
	if err != nil {
		return nil, err
	}
	p, err := r.organizer().Plan(ctx, scope)
	if err != nil {
		return nil, err
	}
	p.Unplaced = append(p.Unplaced, notScanned...)
	return p, nil
}

// Promote runs the plan the admin reviewed (token, else organize.ErrPlanChanged) as one Promote
// job, and waits up to wait for it. It returns the job, as it is when it is done or the wait is
// over (it then runs on in the background).
func (r *Ripper) Promote(ctx context.Context, jobID, token string, wait time.Duration) (*model.EditJob, error) {
	scope, _, err := r.promoteScope(ctx, jobID)
	if err != nil {
		return nil, err
	}
	job, err := r.organizer().RunAs(ctx, model.EditPromote, scope, token)
	if err != nil {
		return nil, err
	}
	log.Info(ctx, "Promoting a Rip", "id", jobID, "job", job.ID, "files", job.Total)
	got, err := r.editQueue.Wait(ctx, job.ID, wait)
	if err != nil {
		log.Warn(ctx, "Could not follow a Promote job; it runs in the background", "job", job.ID, err)
		return job, nil
	}
	return got, nil
}

func (r *Ripper) organizer() *organize.Organizer { return organize.New(r.ds, r.editQueue, nil) }

// promoteScope is the scope of a Promote of the job: its ready Files' Tracks in the Inbox, into
// the Library. The Files the Inbox scan hasn't read yet are returned as rows left in the Inbox.
func (r *Ripper) promoteScope(ctx context.Context, jobID string) (organize.Scope, []organize.Row, error) {
	if r.editQueue == nil {
		return organize.Scope{}, nil, ErrPromoteOff
	}
	job, inbox, err := r.reviewable(ctx, jobID)
	if err != nil {
		return organize.Scope{}, nil, err
	}
	lib, err := organize.PromoteLibrary(ctx, r.ds)
	if err != nil {
		return organize.Scope{}, nil, err
	}
	if lib == nil {
		return organize.Scope{}, nil, ErrPromoteOff
	}
	tracks := r.tracksByPath(ctx, inbox.ID, job.Items)
	var ids []string
	var notScanned []organize.Row
	for _, it := range job.Items {
		if it.Status != model.RipReady || it.Path == "" {
			continue
		}
		if id, ok := tracks[it.Path]; ok {
			ids = append(ids, id)
		} else {
			notScanned = append(notScanned, organize.Row{LibraryID: inbox.ID, Path: it.Path, Status: ReasonNotScanned})
		}
	}
	if len(ids) == 0 {
		return organize.Scope{}, nil, ErrNothingToPromote
	}
	return organize.Scope{Selection: librarymanager.Selection{IDs: ids}, Into: lib.ID}, notScanned, nil
}

// PromoteJobKind is Promote's job kind (model.EditPromote): base, Organize's (which moves the
// album folders' other Files and removes the folders left empty, in the Inbox too, and rescans),
// then the Inbox's side. Once the Files are in the Library their Rips' items go (and a job left
// with no File, as Discard does), and the other Rips' "already have" warnings name the Library.
// Its undo puts the Files back in the Inbox, where the next look at the Inbox gives them a card
// again (ReconcileDrops), and the warnings name the Inbox again.
func (r *Ripper) PromoteJobKind(base editjob.JobKind) editjob.JobKind {
	return promoteKind{r: r, base: base}
}

type promoteKind struct {
	r    *Ripper
	base editjob.JobKind
}

func (k promoteKind) Start(ctx context.Context, job *model.EditJob) error {
	return k.base.Start(ctx, job)
}

func (k promoteKind) Done(ctx context.Context, job *model.EditJob, steps model.EditJobSteps) error {
	if err := k.base.Done(ctx, job, steps); err != nil {
		return err
	}
	return k.r.promoted(ctx, cmp.Or(editjob.RunningJob(ctx), job.ID), false)
}

func (k promoteKind) Undone(ctx context.Context, job *model.EditJob, steps model.EditJobSteps) error {
	if err := k.base.Undone(ctx, job, steps); err != nil {
		return err
	}
	return k.r.promoted(ctx, cmp.Or(editjob.RunningJob(ctx), job.ID), true)
}

// promoted settles the Inbox's side of the moves the edit job jobID made (see PromoteJobKind):
// out of the Inbox, or back into it (undone). It is safe to run again.
func (r *Ripper) promoted(ctx context.Context, jobID string, undone bool) error {
	inbox, err := core.FindInboxLibrary(ctx, r.ds)
	if err != nil || inbox == nil {
		return err
	}
	entries, err := r.ds.EditJournal(ctx).ForJob(jobID)
	if err != nil {
		return err
	}
	libNames := map[int]string{inbox.ID: inbox.Name}
	var left []string // the Inbox paths the job's Files left
	for _, e := range entries {
		if e.Kind != model.EditMove || e.State != model.EditWritten || e.DestLibraryID() == e.LibraryID {
			continue
		}
		into := e.DestLibraryID()
		switch {
		case !undone && e.LibraryID == inbox.ID:
			left = append(left, e.Path)
		case undone && into == inbox.ID:
		default:
			continue
		}
		if e.MediaFileID == "" {
			continue
		}
		if _, ok := libNames[into]; !ok {
			lib, err := r.ds.Library(ctx).Get(into)
			if err != nil {
				return err
			}
			libNames[into] = lib.Name
		}
		if err := r.ds.RipItem(ctx).MoveMatches(e.MediaFileID, libNames[into]); err != nil {
			return err
		}
	}
	if len(left) == 0 {
		return nil
	}
	r.reviewMu.Lock()
	defer r.reviewMu.Unlock()
	items, err := r.ds.RipItem(ctx).ForPaths(left...)
	if err != nil {
		return err
	}
	done := map[string]map[string]bool{}
	for _, it := range items {
		if done[it.JobID] == nil {
			done[it.JobID] = map[string]bool{}
		}
		done[it.JobID][it.ID] = true
	}
	for ripJobID, promoted := range done {
		if err := r.afterPromote(ctx, ripJobID, inbox, promoted); err != nil {
			return err
		}
	}
	log.Info(ctx, "Promoted Files left their Rips", "job", jobID, "files", len(items))
	return nil
}

// afterPromote deletes the items of the Rip ripJobID whose Files were promoted, and the Rip too,
// with its other items and its empty Inbox subfolder, once no File is left in it, as Discard does.
func (r *Ripper) afterPromote(ctx context.Context, ripJobID string, inbox *model.Library, promoted map[string]bool) error {
	job, err := r.ds.RipJob(ctx).Get(ripJobID)
	if errors.Is(err, model.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if job.Items, err = r.ds.RipItem(ctx).ForJobs(job.ID); err != nil {
		return err
	}
	hasFiles := false
	for _, it := range job.Items {
		if !promoted[it.ID] && it.Path != "" {
			hasFiles = true
		}
	}
	err = r.ds.WithTx(func(tx model.DataStore) error {
		for _, it := range job.Items {
			if !promoted[it.ID] && hasFiles {
				continue
			}
			if err := tx.RipItem(ctx).Delete(it.ID); err != nil && !errors.Is(err, model.ErrNotFound) {
				return err
			}
		}
		if hasFiles {
			return nil
		}
		return tx.RipJob(ctx).Delete(job.ID)
	}, "rip promote done")
	if err != nil {
		return err
	}
	if !hasFiles {
		r.removeFolderIfEmpty(ctx, inbox.Path, job)
	}
	return nil
}
