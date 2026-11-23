package tagedit

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/quarantine"
	"github.com/navidrome/navidrome/core/safefile"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
)

// Quarantine moves (#168, ADR-0011). A File sent to Quarantine goes to the same path beneath the
// Quarantine folder by the Library's one move (MoveInJob, safefile.Move: a server-side rename
// that never replaces a File, in any letter case, reusing folders that differ only in letter
// case), journaled as an EditQuarantine entry, so a restart settles it (settleMove) and an undo
// moves it back (undoMove). The move leaves the Track's row where it is (RegisterMoveKind's
// relocate false): the row stays, missing and quarantined, so the File's return to its path (an
// undo, or a restore) brings back the same Track, with its favorites, Playlist entries and plays.
// Only Empty Quarantine (EmptyInJob) deletes Files.

func init() {
	RegisterMoveKind(model.EditQuarantine, false)
	// Its undo is a move's, with the Track marked in or out of Quarantine around it.
	undoers[model.EditQuarantine] = (*Editor).undoQuarantine
}

var (
	// ErrInQuarantine is a Track whose File is in Quarantine already.
	ErrInQuarantine = errors.New("this File is in Quarantine already")
	// ErrNotInQuarantine is a restore of a File that isn't in Quarantine.
	ErrNotInQuarantine = errors.New("this File isn't in Quarantine")
	// ErrDeletedForGood is an undo of a move into Quarantine whose File Empty Quarantine deleted.
	ErrDeletedForGood = errors.New("this File was deleted for good, by Empty Quarantine")
)

// QuarantineInJob moves the File of the Track trackID into Quarantine, as part of the edit job
// jobID, and the Track leaves the Library (missing, and quarantined, so its row is kept). It is
// refused while the Track's library is read-only (ErrEditingOff), when its File changed on disk
// or isn't there (ErrChangedOnDisk), when it is in Quarantine already (ErrInQuarantine), and when
// a File is at its place in Quarantine already, in any letter case (safefile.ErrDestTaken).
func (e *Editor) QuarantineInJob(ctx context.Context, jobID, trackID string) (*Result, error) {
	writeMu.Lock()
	defer writeMu.Unlock()
	ctx = context.WithoutCancel(ctx)

	mf, lib, err := e.quarantineTrack(ctx, trackID)
	if err != nil {
		return nil, err
	}
	// The scanner is kept out of Quarantine before any File goes there.
	if _, err := quarantine.Ensure(e.fsys, lib.Path); err != nil {
		return nil, err
	}
	m := Move{LibraryID: lib.ID, TrackID: mf.ID, Path: mf.Path, NewPath: quarantine.Into(mf.Path), Kind: model.EditQuarantine}
	return e.quarantineMove(ctx, func(before func() error) (*Result, error) {
		m.Before = before
		return e.move(ctx, jobID, m, "", nil)
	}, mf.ID, true)
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

	trackID, err := e.CheckRestore(ctx, libID, rel)
	if err != nil {
		return nil, err
	}
	// The Track's row is at the File's old path already, and stays there for the File to come
	// back to (the move leaves the row alone).
	m := Move{LibraryID: libID, TrackID: trackID, Path: rel, NewPath: quarantine.Original(rel), Kind: model.EditQuarantine}
	return e.quarantineMove(ctx, func(before func() error) (*Result, error) {
		m.Before = before
		return e.move(ctx, jobID, m, "", nil)
	}, trackID, false)
}

// CheckRestore is the refusal a restore of the File at rel in Quarantine meets before the File is
// looked at, if any (see RestoreInJob), and the Track that comes back with it, if any: the missing
// row still at its original path.
func (e *Editor) CheckRestore(ctx context.Context, libID int, rel string) (string, error) {
	if !writable(libID) {
		return "", ErrEditingOff
	}
	if !quarantine.Within(rel) || path.Clean(rel) != rel || rel == quarantine.Folder() || quarantine.IsIgnoreFile(rel) {
		return "", ErrNotInQuarantine
	}
	mfs, err := e.ds.MediaFile(ctx).GetAll(model.QueryOptions{Max: 1, Filters: squirrel.Eq{
		"media_file.library_id": libID, "media_file.path": quarantine.Original(rel), "media_file.missing": true}})
	if err != nil || len(mfs) == 0 {
		return "", err
	}
	return mfs[0].ID, nil
}

// undoQuarantine undoes a move into or out of Quarantine (see undoers): undoMove moves the File
// from NewPath back to Path, journaled with UndoOf, and the Track is marked in or out of
// Quarantine around it.
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
	into := quarantine.Within(entry.Path)
	if into {
		lib, err := e.ds.Library(ctx).Get(entry.LibraryID)
		if err != nil {
			return nil, err
		}
		if _, err := quarantine.Ensure(e.fsys, lib.Path); err != nil {
			return nil, err
		}
	}
	return e.quarantineMove(ctx, func(before func() error) (*Result, error) {
		return e.undoMoveWith(ctx, jobID, entry, before)
	}, entry.MediaFileID, into)
}

// quarantineMove runs a move into Quarantine (into) or out of it, of the Track trackID's File
// (if it has one), and brings the Track up to date. Into Quarantine, the Track is marked once the
// move is journaled and before its File goes (Move.Before), so no scan in between takes it for an
// ordinary missing Track, which the Missing files page and PurgeMissing delete; the mark is taken
// off again if the File stays (or by Settle, after a stop).
func (e *Editor) quarantineMove(ctx context.Context, move func(before func() error) (*Result, error), trackID string, into bool) (*Result, error) {
	repo := e.ds.MediaFile(ctx)
	var before func() error
	if into && trackID != "" {
		before = func() error {
			if err := repo.MarkQuarantined(true, trackID); err != nil {
				return fmt.Errorf("marking the Track as in Quarantine: %w", err)
			}
			return nil
		}
	}
	res, err := move(before)
	if err != nil {
		if into && trackID != "" && !errors.Is(err, safefile.ErrUnrecorded) {
			if merr := repo.MarkQuarantined(false, trackID); merr != nil {
				log.Error(ctx, "Could not take a Track whose File stayed put back out of Quarantine", "id", trackID, merr)
			}
		}
		return nil, err
	}
	if trackID == "" || !res.Changed {
		return res, nil
	}
	entry, err := e.ds.EditJournal(ctx).Get(res.Entry)
	if err != nil {
		return res, nil
	}
	err = repo.MarkQuarantined(into, trackID)
	if err == nil && into {
		// It leaves the Library now, whether or not the scan below gets to run.
		err = repo.MarkMissing(true, &model.MediaFile{ID: trackID})
	}
	if err != nil {
		log.Error(ctx, "Could not mark a Track in or out of Quarantine", "id", trackID, err)
	}
	// The folder in the Library: the one the File left, or the one it came back to.
	inLibrary := entry.Path
	if !into {
		inLibrary = entry.NewPath
	}
	if err := e.rekey(ctx, &file{mf: &model.MediaFile{LibraryID: entry.LibraryID, Path: inLibrary}}); err != nil {
		log.Warn(ctx, "Scan after a move into or out of Quarantine failed", "path", inLibrary, err)
		res.Warning = "The File was moved; the app shows it after the next scan."
	}
	if mf, err := repo.Get(trackID); err == nil {
		res.Track = mf
	}
	return res, nil
}

// settleQuarantine settles a move into or out of Quarantine the app stopped in the middle of, as
// any move (settleMove), then marks its Track where its File ended up: into Quarantine, out of
// the Library (the mark was set before the move); out of it, no longer quarantined (the next scan
// finds it). A move that didn't happen takes the mark back off.
func (e *Editor) settleQuarantine(ctx context.Context, lib *model.Library, entry *model.EditEntry) {
	e.settleMove(ctx, lib, entry)
	if entry.MediaFileID == "" || entry.State == model.EditPending {
		return
	}
	into := quarantine.Within(entry.NewPath) == (entry.State == model.EditWritten)
	repo := e.ds.MediaFile(ctx)
	err := repo.MarkQuarantined(into, entry.MediaFileID)
	if err == nil && into && entry.State == model.EditWritten {
		err = repo.MarkMissing(true, &model.MediaFile{ID: entry.MediaFileID})
	}
	if err != nil {
		log.Error(ctx, "Could not mark the Track of an interrupted move in or out of Quarantine", "id", entry.MediaFileID, err)
	}
}

// settleDelete settles a delete by Empty Quarantine the app stopped in the middle of (see
// Settle): the File gone is a delete written, and its Track goes too.
func (e *Editor) settleDelete(ctx context.Context, lib *model.Library, entry *model.EditEntry) {
	gone := !safefile.Exists(e.fsys, e.abs(lib, entry.Path))
	e.close(ctx, entry, gone, errors.New("interrupted by a restart"))
	log.Info(ctx, "Settled an interrupted delete", "path", entry.Path, "deleted", gone)
	if gone && entry.MediaFileID != "" {
		e.removeTracks(ctx, []string{entry.MediaFileID})
	}
}

func (e *Editor) abs(lib *model.Library, rel string) string { return absPath(lib, rel) }

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
		if !quarantine.Within(f.Path) || path.Clean(f.Path) != f.Path || quarantine.IsIgnoreFile(f.Path) {
			left++
			continue
		}
		switch info, err := e.fsys.Lstat(e.abs(lib, f.Path)); {
		case errors.Is(err, fs.ErrNotExist):
			// Gone already (an earlier run a stop cut off deleted it): its Track goes still.
			if trackID := e.quarantinedTrack(ctx, lib, f.Path); trackID != "" {
				tracks = append(tracks, trackID)
			}
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
	e.removeTracks(ctx, tracks)
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
	trackID := e.quarantinedTrack(ctx, lib, rel)
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

// quarantinedTrack is the Track whose File the app's last move to rel in Quarantine put there,
// while it is still kept for a restore (missing and quarantined); "" if none.
func (e *Editor) quarantinedTrack(ctx context.Context, lib *model.Library, rel string) string {
	last, err := e.ds.EditJournal(ctx).LastWritten(lib.ID, rel)
	if err != nil || last.Kind != model.EditQuarantine || last.MediaFileID == "" {
		return ""
	}
	mf, err := e.ds.MediaFile(ctx).Get(last.MediaFileID)
	if err != nil || !mf.Missing || !mf.Quarantined {
		return ""
	}
	return mf.ID
}

// removeTracks deletes the Tracks of Files Empty Quarantine deleted: they can't come back.
func (e *Editor) removeTracks(ctx context.Context, tracks []string) {
	if len(tracks) == 0 {
		return
	}
	if err := e.ds.MediaFile(ctx).MarkQuarantined(false, tracks...); err != nil {
		log.Error(ctx, "Could not take the deleted Files' Tracks out of Quarantine", err)
	} else if err := core.NewMaintenance(e.ds).DeleteMissingFiles(ctx, tracks); err != nil {
		log.Error(ctx, "Could not remove the Tracks of the Files Empty Quarantine deleted", err)
	}
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
