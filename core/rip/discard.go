package rip

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// Discard deletes all of the job's Files for good (never to Quarantine, which is only for
// Duplicates), removes the job's Inbox subfolder if that leaves it empty, and deletes the job and
// its items. The Inbox is then rescanned in the background, and the Tracks of the deleted Files
// are deleted rather than left as missing Files. A job still queued or downloading is
// ErrNotReady. Every path is checked to be inside the Inbox before anything is deleted.
func (r *Ripper) Discard(ctx context.Context, jobID string) error {
	r.reviewMu.Lock()
	defer r.reviewMu.Unlock()
	job, inbox, err := r.editable(ctx, jobID)
	if err != nil {
		return err
	}
	paths, err := r.filesOf(inbox.Path, job.Items...)
	if err != nil {
		return err
	}
	deleted, err := deleteFiles(paths)
	if err == nil {
		r.removeFolderIfEmpty(ctx, inbox.Path, job)
		err = r.ds.WithTx(func(tx model.DataStore) error {
			for _, it := range job.Items {
				if err := tx.RipItem(ctx).Delete(it.ID); err != nil && !errors.Is(err, model.ErrNotFound) {
					return err
				}
			}
			return tx.RipJob(ctx).Delete(job.ID)
		}, "rip discard")
	}
	if len(deleted) > 0 {
		log.Info(ctx, "Discarded a Rip", "id", job.ID, "files", len(deleted))
		r.rescan(ctx, inbox.ID, deleted)
	}
	return err
}

// DiscardItem deletes one Track's File for good and deletes its item. When that leaves the job
// with no File at all, the job goes too, as Discard would. The Inbox is then rescanned in the
// background, as for Discard.
func (r *Ripper) DiscardItem(ctx context.Context, jobID, itemID string) error {
	r.reviewMu.Lock()
	defer r.reviewMu.Unlock()
	job, inbox, err := r.editable(ctx, jobID)
	if err != nil {
		return err
	}
	var item *model.RipItem
	hasFiles := false
	for i := range job.Items {
		switch {
		case job.Items[i].ID == itemID:
			item = &job.Items[i]
		case job.Items[i].Path != "":
			hasFiles = true
		}
	}
	if item == nil {
		return fmt.Errorf("%w: the Track is not in this Rip", model.ErrNotFound)
	}
	paths, err := r.filesOf(inbox.Path, *item)
	if err != nil {
		return err
	}
	deleted, err := deleteFiles(paths)
	if err == nil {
		err = r.ds.WithTx(func(tx model.DataStore) error {
			if err := tx.RipItem(ctx).Delete(item.ID); err != nil {
				return err
			}
			if hasFiles {
				if item.Status != model.RipFailed {
					return nil
				}
				// The job's note counted the failed Track just discarded.
				job.Error = failedNote(job.Items, item.ID)
				return tx.RipJob(ctx).Put(job)
			}
			for _, it := range job.Items {
				if err := tx.RipItem(ctx).Delete(it.ID); err != nil && !errors.Is(err, model.ErrNotFound) {
					return err
				}
			}
			return tx.RipJob(ctx).Delete(job.ID)
		}, "rip discard item")
		if err == nil && !hasFiles {
			r.removeFolderIfEmpty(ctx, inbox.Path, job)
		}
	}
	if len(deleted) > 0 {
		log.Info(ctx, "Discarded a Track of a Rip", "id", job.ID, "item", item.Position, "path", item.Path)
		r.rescan(ctx, inbox.ID, deleted)
	}
	return err
}

// failedNote is a ready job's note ("N of M Tracks failed") once the item skip is gone.
func failedNote(items model.RipItems, skip string) string {
	total, failed := 0, 0
	for _, it := range items {
		if it.ID == skip {
			continue
		}
		total++
		if it.Status == model.RipFailed {
			failed++
		}
	}
	if failed == 0 {
		return ""
	}
	return fmt.Sprintf("%d of %d Tracks failed", failed, total)
}

// discardFile is a File to delete: its path relative to the Inbox, and its checked full path.
type discardFile struct {
	rel, full string
}

// filesOf checks the items' Files are all inside the Inbox before any is deleted.
func (r *Ripper) filesOf(root string, items ...model.RipItem) ([]discardFile, error) {
	var res []discardFile
	for _, it := range items {
		if it.Path == "" {
			continue
		}
		full, err := inboxPath(root, it.Path)
		if errors.Is(err, fs.ErrNotExist) {
			continue // its folder is gone already, and so is the File
		}
		if err != nil {
			return nil, err
		}
		res = append(res, discardFile{it.Path, full})
	}
	return res, nil
}

// deleteFiles deletes the Files, and returns the paths (relative to the Inbox) of those it
// deleted or found gone already.
func deleteFiles(files []discardFile) ([]string, error) {
	var deleted []string
	for _, f := range files {
		if err := os.Remove(f.full); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return deleted, fmt.Errorf("deleting %s: %w", f.rel, err)
		}
		deleted = append(deleted, f.rel)
	}
	return deleted, nil
}

// removeFolderIfEmpty removes the job's Inbox subfolder if nothing is left in it. A single video's
// job has no subfolder, and the Inbox itself is never removed.
func (r *Ripper) removeFolderIfEmpty(ctx context.Context, root string, job *model.RipJob) {
	if job.Folder == "" {
		return
	}
	dir, err := inboxPath(root, job.Folder)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Warn(ctx, "Not removing the Rip's folder", "id", job.ID, "folder", job.Folder, err)
		}
		return
	}
	// A dropped folder (#35) can have folders of its own ("Album/CD1"): the empty ones go first,
	// deepest first. Symlinks are never followed.
	var subdirs []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && p != dir {
			subdirs = append(subdirs, p)
		}
		return nil
	})
	for _, sub := range slices.Backward(subdirs) {
		_ = os.Remove(sub)
	}
	// os.Remove removes only an empty folder: anything else left in it (a cover, a File dropped in
	// by hand) keeps it.
	if err := os.Remove(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Debug(ctx, "Keeping the Rip's folder: it isn't empty", "folder", job.Folder, err)
	}
}

// rescan scans the Inbox in the background, so the Tracks follow the Files, without holding up
// the admin while another scan runs. The Tracks of the deleted Files, which the scan marks as
// missing, are then deleted, as a Discard is for good.
func (r *Ripper) rescan(ctx context.Context, libID int, deleted []string) {
	if r.scanner == nil {
		return
	}
	ctx = context.WithoutCancel(ctx)
	go func() {
		if err := r.scanInbox(ctx, libID); err != nil {
			log.Warn(ctx, "Inbox scan after a review failed; the changes show up after the next scan", err)
			return
		}
		if len(deleted) == 0 {
			return
		}
		mfs, err := r.ds.MediaFile(ctx).GetAll(model.QueryOptions{Filters: squirrel.And{
			squirrel.Eq{"media_file.library_id": libID},
			squirrel.Eq{"media_file.path": deleted},
			squirrel.Eq{"media_file.missing": true},
		}})
		if err != nil {
			log.Warn(ctx, "Could not find the Tracks of discarded Files", err)
			return
		}
		if len(mfs) == 0 {
			return
		}
		ids := make([]string, len(mfs))
		for i := range mfs {
			ids[i] = mfs[i].ID
		}
		if err := core.NewMaintenance(r.ds).DeleteMissingFiles(ctx, ids); err != nil {
			log.Warn(ctx, "Could not delete the Tracks of discarded Files", err)
		}
	}()
}
