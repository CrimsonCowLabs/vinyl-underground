package rip

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"path/filepath"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

var (
	// ErrPromoteOff is returned when no promote folder is set up (Inbox.PromotePath).
	ErrPromoteOff = errors.New("Promote is off: no read-write promote folder is set up")
	// ErrNothingToPromote is returned for a job with no File in the Inbox.
	ErrNothingToPromote = errors.New("this Rip has no Files to promote")
)

// PromotedFile is one File of a Promote: where it went (relative to its new library), or why
// it stayed in the Inbox.
type PromotedFile struct {
	ItemID string `json:"itemId"`
	Path   string `json:"path,omitempty"`
	Error  string `json:"error,omitempty"`
}

// PromoteResult is what Promote did, File by File. Review is the card for what is left of the
// job, or nil when nothing is (the job is gone).
type PromoteResult struct {
	Promoted []PromotedFile `json:"promoted"`
	Failed   []PromotedFile `json:"failed"`
	Review   *Review        `json:"review"`
}

// PromoteEnabled reports whether a promote folder is set up.
func (r *Ripper) PromoteEnabled() bool { return r.opts.PromoteRoot != "" }

// Promote moves the job's Files from the Inbox into the promote folder (ADR-0008), at
// `<Album Artist>/<Album> (<Year>)/<NN> <Title>.<ext>` from each File's tags (see promoteLayout
// and promoteDest), and keeps each Track's identity: its row is pointed at the new File, in the
// promote folder's library, so favorites, ratings, play counts, Playlist entries, bookmarks,
// the play queue, shares and Station history stay attached, and every Listener with that
// library sees it.
//
// Each File is promoted on its own: one that fails stays in the Inbox, its item keeps the
// error, and the others carry on. When no File is left, the job and its items are deleted and
// its Inbox subfolder is removed if empty, as Discard does. Both libraries are then rescanned in
// the background. A job still queued or downloading is ErrNotReady.
//
// A File is never lost or half-copied (see placeCopy): its destination is recorded on its item
// first, so a restart finishes or undoes a move it interrupted (ResumePromotes), and the Inbox
// copy is deleted only once the Track points at the new File.
func (r *Ripper) Promote(ctx context.Context, jobID string) (*PromoteResult, error) {
	if !r.PromoteEnabled() {
		return nil, ErrPromoteOff
	}
	r.reviewMu.Lock()
	defer r.reviewMu.Unlock()
	job, inbox, err := r.reviewable(ctx, jobID)
	if err != nil {
		return nil, err
	}
	lib, _, err := promoteLibrary(ctx, r.ds, r.opts.PromoteRoot)
	if err != nil {
		return nil, err
	}
	res := &PromoteResult{Promoted: []PromotedFile{}, Failed: []PromotedFile{}}
	promoted := map[string]bool{}
	moved := false // a Track moved, even if its Inbox copy is still there
	for i := range job.Items {
		it := &job.Items[i]
		if it.Path == "" || it.Status != model.RipReady {
			continue
		}
		var rel string
		if it.PromoteDest != "" {
			rel, err = r.resumePromote(ctx, inbox, it)
			if err == nil && rel == "" {
				// The earlier move was undone; this one starts over.
				rel, err = r.promoteItem(ctx, inbox, lib, it)
			}
		} else {
			rel, err = r.promoteItem(ctx, inbox, lib, it)
		}
		if err != nil {
			moved = moved || errors.Is(err, errInboxCopyLeft)
			log.Warn(ctx, "Could not promote a File", "id", job.ID, "path", it.Path, err)
			res.Failed = append(res.Failed, PromotedFile{ItemID: it.ID, Error: err.Error()})
			it.Error = "Promote failed: " + err.Error()
			r.saveItem(ctx, it)
			continue
		}
		promoted[it.ID] = true
		res.Promoted = append(res.Promoted, PromotedFile{ItemID: it.ID, Path: rel})
	}
	if len(promoted) == 0 && len(res.Failed) == 0 {
		return nil, ErrNothingToPromote
	}
	log.Info(ctx, "Promoted a Rip", "id", job.ID, "promoted", len(res.Promoted), "failed", len(res.Failed))

	left := r.afterPromote(ctx, job, inbox, promoted)
	if len(promoted) > 0 || moved {
		r.rescanPromoted(ctx, inbox.ID, lib)
	}
	if left != nil {
		if res.Review, err = r.review(ctx, left, inbox); err != nil {
			log.Warn(ctx, "Could not read what is left of a promoted Rip", "id", job.ID, err)
		}
	}
	return res, nil
}

// errInboxCopyLeft is a File whose Track points at its promoted copy already, but whose Inbox
// copy could not be deleted. Its item keeps PromoteDest, so promoting it again (or a restart)
// deletes it.
var errInboxCopyLeft = errors.New("the File was promoted, but its Inbox copy could not be deleted; promote it again to retry")

// promoteItem moves one item's File into the promote folder, and returns its path relative to
// lib. On failure the File is still in the Inbox, and nothing is left at its destination.
func (r *Ripper) promoteItem(ctx context.Context, inbox, lib *model.Library, it *model.RipItem) (string, error) {
	src, err := inboxPath(inbox.Path, it.Path)
	if err != nil {
		return "", err
	}
	tags, err := readFileTags(src)
	if err != nil {
		return "", err
	}
	dest, err := promoteDest(r.fs(), r.opts.PromoteRoot, promoteLayout(tags, filepath.Base(src)))
	if err != nil {
		return "", err
	}
	// Recorded before anything is written, so a restart can always find this move.
	it.PromoteDest, it.Error = dest, ""
	if err := r.ds.RipItem(ctx).Put(it); err != nil {
		it.PromoteDest = ""
		return "", fmt.Errorf("recording the move: %w", err)
	}
	if err := placeCopy(r.fs(), src, dest); err != nil {
		r.clearPromote(ctx, it)
		return "", err
	}
	return r.finishPromote(ctx, inbox, it, src, dest, true)
}

// finishPromote points the item's Track at the File now at dest, then deletes the Inbox copy at
// src (unless src is "") and the item. It is safe to run again on a move it already finished
// part of. fresh is true when this call's own placeCopy just made the copy at dest.
//
// Normally only the Inbox has a Track for the File. After an interruption both may: a scan can
// have read the copy at dest before the Track moved, or the Inbox copy left behind after it
// moved. The older of the two is the Track the Listeners know; the other is merged into it.
func (r *Ripper) finishPromote(ctx context.Context, inbox *model.Library, it *model.RipItem, src, dest string, fresh bool) (string, error) {
	var atDest *model.MediaFile
	lookedUp := false
	lib, rel, err := promoteLibrary(ctx, r.ds, dest)
	if err == nil {
		atDest, err = trackAt(r.ds, ctx, lib.ID, rel)
		lookedUp = err == nil
	}
	if err == nil {
		// Rerun while the database is busy: a scan (often the watcher's, set off by this Promote
		// deleting Inbox copies) can be writing, and this transaction reads before it writes.
		err = r.ds.WithTxRetry(ctx, func(ctx context.Context, tx model.DataStore) error {
			inInbox, err := trackAt(tx, ctx, inbox.ID, it.Path)
			if err != nil {
				return err
			}
			keep := olderTrack(inInbox, atDest)
			if keep == nil {
				return nil // never scanned, so there is no identity to keep
			}
			if inInbox != nil && keep.ID != inInbox.ID {
				if err := tx.MediaFile(ctx).ReassignReferences(inInbox.ID, keep.ID); err != nil {
					return err
				}
				if err := tx.MediaFile(ctx).Delete(inInbox.ID); err != nil {
					return err
				}
			}
			// Relocate merges any other Track at dest into keep.
			if err := tx.MediaFile(ctx).Relocate(keep.ID, lib.ID, rel, model.FolderID(*lib, path.Dir(rel))); err != nil {
				return fmt.Errorf("moving the Track: %w", err)
			}
			return tx.RipItem(ctx).MoveMatches(keep.ID, lib.Name)
		}, "rip promote")
	}
	if err != nil {
		// Undo, unless the copy at dest may be a Track's File: the move is then finished later.
		// A copy this call just made is no Track's (yet), so it always goes.
		if atDest != nil || (!fresh && !lookedUp) {
			return "", err
		}
		if rmErr := r.fs().Remove(dest); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
			log.Error(ctx, "Could not remove the copy of a File that failed to promote", "path", dest, rmErr)
		}
		r.clearPromote(ctx, it)
		return "", err
	}
	if src != "" {
		if err := r.fs().Remove(src); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("%w: %v", errInboxCopyLeft, err)
		}
	}
	if err := r.ds.RipItem(ctx).Delete(it.ID); err != nil && !errors.Is(err, model.ErrNotFound) {
		log.Warn(ctx, "Could not delete a promoted Rip item", "item", it.ID, err)
	}
	return rel, nil
}

// olderTrack returns whichever of a and b (either may be nil) was created first.
func olderTrack(a, b *model.MediaFile) *model.MediaFile {
	switch {
	case a == nil:
		return b
	case b == nil, !b.CreatedAt.Before(a.CreatedAt):
		return a
	default:
		return b
	}
}

// trackAt returns the (non-missing or missing) Track scanned from the File at path in the
// library libID, or nil.
func trackAt(ds model.DataStore, ctx context.Context, libID int, p string) (*model.MediaFile, error) {
	mfs, err := ds.MediaFile(ctx).GetAll(model.QueryOptions{Filters: squirrel.And{
		squirrel.Eq{"media_file.library_id": libID},
		squirrel.Eq{"media_file.path": p},
	}})
	if err != nil {
		return nil, err
	}
	for i := range mfs {
		if mfs[i].LibraryID == libID && mfs[i].Path == p {
			return &mfs[i], nil
		}
	}
	return nil, nil
}

// clearPromote forgets the item's move, once it is undone.
func (r *Ripper) clearPromote(ctx context.Context, it *model.RipItem) {
	it.PromoteDest = ""
	r.saveItem(ctx, it)
}

// resumePromote finishes or undoes a move the app stopped in the middle of, from where the
// File and its Track are. It returns the promoted File's path relative to its new library, or ""
// if the move was undone and the File is still in the Inbox. A move it can't settle safely (its
// library is gone, or its Track moved but its File can't be seen) stays recorded, with an
// error, to be tried again.
func (r *Ripper) resumePromote(ctx context.Context, inbox *model.Library, it *model.RipItem) (string, error) {
	dest := it.PromoteDest
	lib, rel, err := promoteLibrary(ctx, r.ds, dest)
	if err != nil {
		return "", fmt.Errorf("finishing an interrupted Promote: %w", err)
	}
	if err := r.fs().Remove(promoteTemp(dest)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Warn(ctx, "Could not remove an interrupted Promote's temp File", "path", promoteTemp(dest), err)
	}
	src, err := inboxPath(inbox.Path, it.Path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	srcThere := src != "" && exists(r.fs(), src)
	destThere := exists(r.fs(), dest)
	atDest, err := trackAt(r.ds, ctx, lib.ID, rel)
	if err != nil {
		return "", err
	}

	switch {
	case !destThere && atDest != nil:
		return "", fmt.Errorf("the File was promoted to %q, but it can't be seen there (is the read-write mount there?)", rel)
	case !destThere:
		// The copy never got there: the move is undone.
	case atDest != nil || !srcThere || sameFile(r.fs(), src, dest):
		// The copy is complete, or a Track points at it already: finish the move.
		if !srcThere {
			src = ""
		}
		return r.finishPromote(ctx, inbox, it, src, dest, false)
	default:
		// What is at dest isn't the File: leave it alone.
	}
	log.Info(ctx, "Undid an interrupted Promote; the File stays in the Inbox", "path", it.Path, "dest", dest)
	r.clearPromote(ctx, it)
	return "", nil
}

// afterPromote deletes the job, its items and its empty Inbox subfolder once no File is left in
// it, as Discard does, and returns the job as it is left, or nil if it is gone.
func (r *Ripper) afterPromote(ctx context.Context, job *model.RipJob, inbox *model.Library, promoted map[string]bool) *model.RipJob {
	var left model.RipItems
	hasFiles := false
	for _, it := range job.Items {
		if promoted[it.ID] {
			continue
		}
		left = append(left, it)
		if it.Path != "" {
			hasFiles = true
		}
	}
	if hasFiles {
		job.Items = left
		return job
	}
	err := r.ds.WithTx(func(tx model.DataStore) error {
		for _, it := range left {
			if err := tx.RipItem(ctx).Delete(it.ID); err != nil && !errors.Is(err, model.ErrNotFound) {
				return err
			}
		}
		return tx.RipJob(ctx).Delete(job.ID)
	}, "rip promote done")
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		log.Warn(ctx, "Could not delete a promoted Rip", "id", job.ID, err)
	}
	r.removeFolderIfEmpty(ctx, inbox.Path, job)
	return nil
}

// rescanPromoted scans the Inbox and the promote folder in the background, so the promoted
// Tracks show at their new place (with the album and IDs of their new library) and leave the
// Inbox's lists.
func (r *Ripper) rescanPromoted(ctx context.Context, inboxID int, lib *model.Library) {
	if r.scanner == nil {
		return
	}
	_, rel, err := promoteLibrary(ctx, r.ds, r.opts.PromoteRoot)
	if err != nil {
		rel = "" // the whole library, then
	}
	ctx = context.WithoutCancel(ctx)
	go func() {
		targets := []model.ScanTarget{{LibraryID: inboxID}, {LibraryID: lib.ID, FolderPath: rel}}
		if err := r.scanWithRetry(ctx, targets...); err != nil {
			log.Warn(ctx, "Scan after a Promote failed; the Tracks move after the next scan", err)
		}
	}()
}

// ResumePromotes finishes or undoes every Promote the app stopped in the middle of, before any
// new one can start. A finished job is then deleted, as Promote does, and the libraries scanned.
func (r *Ripper) ResumePromotes(ctx context.Context) {
	r.reviewMu.Lock()
	defer r.reviewMu.Unlock()
	items, err := r.ds.RipItem(ctx).PromotesInterrupted()
	if err != nil {
		log.Error(ctx, "Could not look for interrupted Promotes", err)
		return
	}
	if len(items) == 0 {
		return
	}
	if !r.PromoteEnabled() {
		// Its folder may just not be mounted: undoing now could orphan a copy there.
		log.Warn(ctx, "Interrupted Promotes are left as they are until Promote is on again", "count", len(items))
		return
	}
	inbox, err := core.FindInboxLibrary(ctx, r.ds)
	if err != nil || inbox == nil {
		log.Error(ctx, "Interrupted Promotes are left as they are: there is no Inbox", err)
		return
	}
	byJob := map[string]map[string]bool{}
	var lib *model.Library
	for i := range items {
		it := &items[i]
		rel, err := r.resumePromote(ctx, inbox, it)
		if err != nil {
			log.Error(ctx, "Could not finish an interrupted Promote; it is tried again with the next Promote", "path", it.Path, "dest", it.PromoteDest, err)
			continue
		}
		if rel == "" {
			continue
		}
		log.Info(ctx, "Finished an interrupted Promote", "path", it.Path, "dest", it.PromoteDest)
		if byJob[it.JobID] == nil {
			byJob[it.JobID] = map[string]bool{}
		}
		byJob[it.JobID][it.ID] = true
		if l, _, err := promoteLibrary(ctx, r.ds, it.PromoteDest); err == nil {
			lib = l
		}
	}
	for jobID, promoted := range byJob {
		job, err := r.ds.RipJob(ctx).Get(jobID)
		if err != nil {
			continue
		}
		if job.Items, err = r.ds.RipItem(ctx).ForJobs(job.ID); err != nil {
			continue
		}
		// The promoted items are deleted already; the rest decide whether the job stays.
		r.afterPromote(ctx, job, inbox, promoted)
	}
	if lib != nil {
		r.rescanPromoted(ctx, inbox.ID, lib)
	}
}

func exists(fsys promoteFS, name string) bool {
	_, err := fsys.Lstat(name)
	return err == nil
}

// sameFile reports whether the Files at a and b have the same bytes.
func sameFile(fsys promoteFS, a, b string) bool {
	ha, na, errA := hashFile(fsys, a)
	hb, nb, errB := hashFile(fsys, b)
	return errA == nil && errB == nil && na == nb && bytes.Equal(ha, hb)
}

func hashFile(fsys promoteFS, name string) ([]byte, int64, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return h.Sum(nil), n, err
}

// fs is the filesystem Promote works on: the real one, unless a test set another.
func (r *Ripper) fs() promoteFS {
	if r.promoteFS != nil {
		return r.promoteFS
	}
	return osFS{}
}
