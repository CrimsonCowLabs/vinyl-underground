package tagedit

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/quarantine"
	"github.com/navidrome/navidrome/core/safefile"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
)

// Quarantine moves (#168, ADR-0011). A File sent to Quarantine is moved, by one rename, to the
// same path beneath the Quarantine folder (quarantine.Target: folders differing only in letter
// case are reused, and a File already there is never overwritten), journaled as an
// EditQuarantine entry first, so a restart settles it and an undo moves it back. Its Track's row
// stays, missing and quarantined, so the File's return to its path (an undo, or a restore) brings
// back the same Track, with its favorites, Playlist entries and plays. Only Empty Quarantine
// (EmptyInJob) deletes Files.
//
// This is the minimal move Quarantine needs. Organize (#167) brings the general journaled move of
// a File within the Library (core/safefile); once it is in, these moves should go through it.

var (
	// ErrInQuarantine is a Track whose File is in Quarantine already.
	ErrInQuarantine = errors.New("this File is in Quarantine already")
	// ErrNotInQuarantine is a restore of a File that isn't in Quarantine.
	ErrNotInQuarantine = errors.New("this File isn't in Quarantine")
	// ErrDeletedForGood is an undo of a move into Quarantine whose File Empty Quarantine deleted.
	ErrDeletedForGood = errors.New("this File was deleted for good, by Empty Quarantine")
	// ErrMoveUnsettled is a move after which the File was found in neither place, or in both: the
	// entry stays pending, and the next start settles it.
	ErrMoveUnsettled = errors.New("the move could not be checked; the next start settles it")
)

// QuarantineInJob moves the File of the Track trackID into Quarantine, as part of the edit job
// jobID, and the Track leaves the Library (missing, and quarantined, so its row is kept). It is
// refused while the Track's library is read-only (ErrEditingOff), when its File isn't there
// (ErrChangedOnDisk), when it is in Quarantine already (ErrInQuarantine), and when a File is at
// its place in Quarantine already (quarantine.ErrTaken).
func (e *Editor) QuarantineInJob(ctx context.Context, jobID, trackID string) (*Result, error) {
	writeMu.Lock()
	defer writeMu.Unlock()
	ctx = context.WithoutCancel(ctx)

	mf, lib, err := e.quarantineTrack(ctx, trackID)
	if err != nil {
		return nil, err
	}
	return e.move(ctx, jobID, lib, mf.ID, mf.Path, quarantine.Into(mf.Path), "")
}

// CheckQuarantine is the refusal a Track's move into Quarantine meets before its File is looked
// at, if any (see QuarantineInJob), and the Track.
func (e *Editor) CheckQuarantine(ctx context.Context, trackID string) (*model.MediaFile, error) {
	mf, _, err := e.quarantineTrack(ctx, trackID)
	return mf, err
}

func (e *Editor) quarantineTrack(ctx context.Context, trackID string) (*model.MediaFile, *model.Library, error) {
	mf, err := e.ds.MediaFile(ctx).Get(trackID)
	if err != nil {
		return nil, nil, err
	}
	switch {
	case !writable(mf.LibraryID):
		return nil, nil, ErrEditingOff
	case quarantine.Within(mf.Path) || mf.Quarantined:
		return nil, nil, ErrInQuarantine
	case mf.Missing:
		return nil, nil, fmt.Errorf("%w: the File is missing", ErrChangedOnDisk)
	}
	lib, err := e.ds.Library(ctx).Get(mf.LibraryID)
	if err != nil {
		return nil, nil, err
	}
	return mf, lib, nil
}

// RestoreInJob moves the File at rel in Quarantine (in the library libID), which no journaled
// move put there (the library-cleanup recipes moved it by hand), to where it came from: its path
// beneath Quarantine (quarantine.Original). Its Track, if a missing row is still at that path,
// comes back with it. A File the app moved into Quarantine is restored by undoing that move.
func (e *Editor) RestoreInJob(ctx context.Context, jobID string, libID int, rel string) (*Result, error) {
	writeMu.Lock()
	defer writeMu.Unlock()
	ctx = context.WithoutCancel(ctx)

	if !writable(libID) {
		return nil, ErrEditingOff
	}
	if !quarantine.Within(rel) || path.Clean(rel) != rel || rel == quarantine.Folder() {
		return nil, ErrNotInQuarantine
	}
	lib, err := e.ds.Library(ctx).Get(libID)
	if err != nil {
		return nil, err
	}
	orig := quarantine.Original(rel)
	var trackID string
	mfs, err := e.ds.MediaFile(ctx).GetAll(model.QueryOptions{Max: 1,
		Filters: squirrel.Eq{"media_file.library_id": libID, "media_file.path": orig, "media_file.missing": true}})
	if err != nil {
		return nil, err
	}
	if len(mfs) > 0 {
		trackID = mfs[0].ID
	}
	return e.move(ctx, jobID, lib, trackID, rel, orig, "")
}

// undoQuarantine undoes a move into or out of Quarantine (see undoers): the File goes back from
// NewPath to Path, as a move of its own, journaled with UndoOf.
func (e *Editor) undoQuarantine(ctx context.Context, jobID string, entry *model.EditEntry) (*Result, error) {
	err := e.checkLater(ctx, entry, func(l model.EditEntry) error {
		if l.Kind == model.EditDelete {
			return ErrDeletedForGood
		}
		return fmt.Errorf("%w: the File was moved or written since", ErrChangedLater)
	})
	if err != nil {
		return nil, err
	}
	if !writable(entry.LibraryID) {
		return nil, ErrEditingOff
	}
	lib, err := e.ds.Library(ctx).Get(entry.LibraryID)
	if err != nil {
		return nil, err
	}
	return e.move(ctx, jobID, lib, entry.MediaFileID, entry.NewPath, entry.Path, entry.ID)
}

// move moves the File at from to to (both relative to the library lib), journaled: the target is
// planned first (quarantine.Target), the entry recorded pending, then the File renamed and the
// move checked by listing both folders. Into Quarantine, its folder (with its .ndignore) is made
// first, so the scanner never sees the File there.
func (e *Editor) move(ctx context.Context, jobID string, lib *model.Library, trackID, from, to, undoOf string) (*Result, error) {
	if !quarantine.Present(e.fsys, lib.Path, from) {
		return nil, fmt.Errorf("%w: the File isn't at %s", ErrChangedOnDisk, from)
	}
	if quarantine.Within(to) {
		if _, err := quarantine.Ensure(e.fsys, lib.Path); err != nil {
			return nil, err
		}
	}
	target, err := quarantine.Target(e.fsys, lib.Path, to)
	if err != nil {
		return nil, err
	}
	entry := &model.EditEntry{JobID: jobID, Kind: model.EditQuarantine, State: model.EditPending,
		LibraryID: lib.ID, MediaFileID: trackID, Path: from, NewPath: target, UndoOf: undoOf}
	if u, ok := request.UserFrom(ctx); ok {
		entry.UserID = u.ID
	}
	if err := e.ds.EditJournal(ctx).Put(entry); err != nil {
		return nil, fmt.Errorf("recording the move in the Edit journal: %w", err)
	}

	err = quarantine.MkdirAll(e.fsys, lib.Path, path.Dir(target))
	if err == nil {
		err = e.fsys.Rename(e.abs(lib, from), e.abs(lib, target))
	}
	written, known := e.moved(lib, entry)
	switch {
	case !known:
		entry.Error = fmt.Sprintf("%v (%v)", ErrMoveUnsettled, err)
		_ = e.ds.EditJournal(ctx).Put(entry)
		log.Error(ctx, "A move into or out of Quarantine could not be checked; the next start settles it", "from", from, "to", target, err)
		return nil, ErrMoveUnsettled
	case !written:
		err = fmt.Errorf("moving the File: %w", errors.Join(err, errors.New("the File was left where it was")))
		e.close(ctx, entry, false, err)
		log.Warn(ctx, "Could not move a File into or out of Quarantine", "from", from, "to", target, err)
		return nil, err
	}
	e.close(ctx, entry, true, nil)
	log.Info(ctx, "Moved a File", "from", from, "to", target, "quarantine", quarantine.Within(target))
	return e.moveDone(ctx, lib, entry), nil
}

// moved tells, by listing both folders, whether the move of entry happened (the File is at
// NewPath, and not at Path) or not (the other way round); known is false when it is in both
// places, or neither.
func (e *Editor) moved(lib *model.Library, entry *model.EditEntry) (written, known bool) {
	at, to := quarantine.Present(e.fsys, lib.Path, entry.Path), quarantine.Present(e.fsys, lib.Path, entry.NewPath)
	return to, at != to
}

// moveDone brings the Track of a written move up to date: into Quarantine it leaves the Library
// (missing and quarantined, its row kept); out of it, it is no longer quarantined, and the scan
// of the File's folder finds it again on the same row, matched by its path.
func (e *Editor) moveDone(ctx context.Context, lib *model.Library, entry *model.EditEntry) *Result {
	into := quarantine.Within(entry.NewPath)
	res := &Result{Changed: true, Entry: entry.ID}
	if entry.MediaFileID != "" {
		if err := e.ds.MediaFile(ctx).MarkQuarantined(into, entry.MediaFileID); err != nil {
			log.Error(ctx, "Could not mark a Track in or out of Quarantine", "id", entry.MediaFileID, err)
		}
	}
	// The folder in the Library: the one the File left, or the one it came back to.
	inLibrary := entry.Path
	if !into {
		inLibrary = entry.NewPath
	}
	if err := e.rekey(ctx, &file{mf: &model.MediaFile{LibraryID: lib.ID, Path: inLibrary}}); err != nil {
		log.Warn(ctx, "Scan after a move into or out of Quarantine failed", "path", inLibrary, err)
		res.Warning = "The File was moved; the app shows it after the next scan."
	}
	if mf, err := e.ds.MediaFile(ctx).Get(entry.MediaFileID); err == nil {
		res.Track = mf
	}
	return res
}

// settleQuarantine settles a move, or a delete, the app stopped in the middle of (see Settle).
func (e *Editor) settleQuarantine(ctx context.Context, lib *model.Library, entry *model.EditEntry) {
	interrupted := errors.New("interrupted by a restart")
	if entry.Kind == model.EditDelete {
		gone := !safefile.Exists(e.fsys, e.abs(lib, entry.Path))
		e.close(ctx, entry, gone, interrupted)
		log.Info(ctx, "Settled an interrupted delete", "path", entry.Path, "deleted", gone)
		return
	}
	written, known := e.moved(lib, entry)
	if !known {
		log.Error(ctx, "Could not settle an interrupted move: the File is in both places, or neither; it is tried again at the next start",
			"entry", entry.ID, "from", entry.Path, "to", entry.NewPath)
		return
	}
	log.Info(ctx, "Settled an interrupted move", "from", entry.Path, "to", entry.NewPath, "moved", written)
	e.close(ctx, entry, written, interrupted)
	if written {
		e.moveDone(ctx, lib, entry)
	}
}

func (e *Editor) abs(lib *model.Library, rel string) string {
	return filepath.Join(lib.Path, filepath.FromSlash(rel))
}

// QuarantinedFile is a File in Quarantine, as the admin confirmed Empty Quarantine for it: its
// library, path and size.
type QuarantinedFile struct {
	LibraryID int    `json:"libraryId"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
}

// EmptyInJob is Empty Quarantine, as part of the edit job jobID: it deletes the Files for good.
// It is the only way the app deletes a File from a library (ADR-0011). Each delete is journaled
// (EditDelete) and can't be undone. A File is only deleted while it is in Quarantine at the size
// the admin confirmed: one that changed, or isn't in Quarantine, is left (the Warning says how
// many). One gone already (a stop cut the job off) is passed over, so the job can run again. The
// Tracks of the deleted Files, kept so a restore could bring them back, are deleted too, and
// with them their favorites and Playlist entries. Folders it empties beneath Quarantine go.
func (e *Editor) EmptyInJob(ctx context.Context, jobID string, files []QuarantinedFile) (*Result, error) {
	writeMu.Lock()
	defer writeMu.Unlock()
	ctx = context.WithoutCancel(ctx)

	libs := map[int]*model.Library{}
	for _, f := range files {
		if libs[f.LibraryID] != nil {
			continue
		}
		if !writable(f.LibraryID) {
			return nil, ErrEditingOff
		}
		lib, err := e.ds.Library(ctx).Get(f.LibraryID)
		if err != nil {
			return nil, err
		}
		libs[f.LibraryID] = lib
	}
	res := &Result{}
	var tracks []string
	left, failed := 0, 0
	for _, f := range files {
		lib := libs[f.LibraryID]
		switch info, err := e.fsys.Lstat(e.abs(lib, f.Path)); {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case !quarantine.Within(f.Path) || path.Clean(f.Path) != f.Path || strings.HasSuffix(f.Path, "/.ndignore"):
			left++
			continue
		case err != nil || !info.Mode().IsRegular() || info.Size() != f.Size:
			left++
			continue
		}
		entry, trackID, err := e.deleteFile(ctx, jobID, lib, f.Path)
		if err != nil {
			failed++
			continue
		}
		res.Changed, res.Entry = true, entry
		if trackID != "" {
			tracks = append(tracks, trackID)
		}
		e.removeEmptyFolders(lib, path.Dir(f.Path))
	}
	if len(tracks) > 0 {
		if err := e.ds.MediaFile(ctx).MarkQuarantined(false, tracks...); err != nil {
			log.Error(ctx, "Could not take the deleted Files' Tracks out of Quarantine", err)
		} else if err := core.NewMaintenance(e.ds).DeleteMissingFiles(ctx, tracks); err != nil {
			log.Error(ctx, "Could not remove the Tracks of the Files Empty Quarantine deleted", err)
		}
	}
	var notes []string
	if left > 0 {
		notes = append(notes, fmt.Sprintf("%d Files changed since you confirmed, or aren't in Quarantine, and were left", left))
	}
	if failed > 0 {
		notes = append(notes, fmt.Sprintf("%d Files could not be deleted", failed))
	}
	res.Warning = strings.Join(notes, "; ")
	log.Info(ctx, "Emptied Quarantine", "job", jobID, "files", len(files), "left", left, "failed", failed)
	return res, nil
}

// deleteFile deletes the File at rel in Quarantine for good, journaled, and returns the entry and
// the Track the journal says the File was, if any (a missing, quarantined row).
func (e *Editor) deleteFile(ctx context.Context, jobID string, lib *model.Library, rel string) (string, string, error) {
	var trackID string
	if last, err := e.ds.EditJournal(ctx).LastWritten(lib.ID, rel); err == nil && last.Kind == model.EditQuarantine && last.MediaFileID != "" {
		if mf, err := e.ds.MediaFile(ctx).Get(last.MediaFileID); err == nil && mf.Missing && mf.Quarantined {
			trackID = mf.ID
		}
	}
	entry := &model.EditEntry{JobID: jobID, Kind: model.EditDelete, State: model.EditPending,
		LibraryID: lib.ID, MediaFileID: trackID, Path: rel}
	if u, ok := request.UserFrom(ctx); ok {
		entry.UserID = u.ID
	}
	if err := e.ds.EditJournal(ctx).Put(entry); err != nil {
		return "", "", fmt.Errorf("recording the delete in the Edit journal: %w", err)
	}
	// The only place the app deletes a File from a library.
	if err := e.fsys.Remove(e.abs(lib, rel)); err != nil {
		log.Warn(ctx, "Could not delete a File in Quarantine", "path", rel, err)
		e.close(ctx, entry, false, err)
		return "", "", err
	}
	e.close(ctx, entry, true, nil)
	return entry.ID, trackID, nil
}

// removeEmptyFolders removes dir (relative to the library) and the folders above it, up to
// Quarantine's own folder, while they are empty.
func (e *Editor) removeEmptyFolders(lib *model.Library, dir string) {
	for quarantine.Within(dir) && !strings.EqualFold(dir, quarantine.Folder()) {
		entries, err := e.fsys.ReadDir(e.abs(lib, dir))
		if err != nil || len(entries) > 0 || e.fsys.Remove(e.abs(lib, dir)) != nil {
			return
		}
		dir = path.Dir(dir)
	}
}
