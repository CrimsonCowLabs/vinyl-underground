package tagedit

import (
	"cmp"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"

	"github.com/navidrome/navidrome/core/safefile"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
)

// Move is one File to move within its library (see MoveInJob). Paths are relative to the
// library, slash-separated.
type Move struct {
	LibraryID int `json:"libraryId"`
	// TrackID is the File's Track, or empty for a File that is no Track's (a cover.jpg).
	TrackID string `json:"trackId,omitempty"`
	Path    string `json:"path"`
	NewPath string `json:"newPath"`
	// Kind is the kind the move is journaled as: model.EditMove when empty. Another kind must be
	// registered with RegisterMoveKind first.
	Kind model.EditKind `json:"kind,omitempty"`
}

// moveKinds are the journal kinds that are moves made by MoveInJob, each with whether the move
// points the File's Track at its new path (Organize) or leaves the Track's row alone (a move the
// Track leaves the Library with, which deals with the row itself: Quarantine).
var moveKinds = map[model.EditKind]bool{model.EditMove: true}

// RegisterMoveKind makes kind a move MoveInJob journals, settles at startup and undoes, as
// EditMove is. relocate says whether the move points the File's Track at its new path (and the
// undo back at the old one), or leaves the Track's row as it is. Call it at init.
func RegisterMoveKind(kind model.EditKind, relocate bool) {
	moveKinds[kind] = relocate
	undoers[kind] = (*Editor).undoMove
}

func init() { undoers[model.EditMove] = (*Editor).undoMove }

// CheckMove is the refusal a move meets before anything is read, if any: its library isn't
// writable (ErrEditingOff), or its Track is gone (model.ErrNotFound).
func (e *Editor) CheckMove(ctx context.Context, m Move) error {
	if !writable(m.LibraryID) {
		return ErrEditingOff
	}
	if m.TrackID != "" {
		if _, err := e.ds.MediaFile(ctx).Get(m.TrackID); err != nil {
			return err
		}
	}
	return nil
}

// MoveInJob moves the File at m.Path to m.NewPath within its library, as one write of the edit job
// jobID, through the write path (ADR-0011). It is the one way the app moves a File in the
// Library, and keeps the File's Track (its ID, and so its favorites, ratings, plays, Playlist
// entries and bookmarks) as Promote does:
//
//  1. Guard: a Track's File whose size or modification time differs from the last scan's is
//     refused (ErrChangedOnDisk), as is a Track not at m.Path.
//  2. The new path's folders are made, reusing any whose name differs only in letter case
//     (safefile.EnsureDir), so the NAS never gets two such folders; one with such a twin already
//     is refused (safefile.ErrCaseTwin). The final path is m.NewPath as it is on disk.
//  3. Journal: the move (Path, NewPath) is recorded in the Edit journal before anything is
//     written, and the File's checksum once its copy is checked.
//  4. The File is copied beside its new path, checked and placed (safefile.Move), never over
//     another File (safefile.ErrDestTaken).
//  5. Re-key: in one transaction, the Track's row is pointed at the new path and folder, keeping
//     its ID (model.MediaFileRepository.Relocate); only then is the old copy removed. The next scan
//     of the new folder re-reads the File into the same row. Moves don't rescan: the job does,
//     once (RescanTargets).
//
// A restart in the middle is settled by Settle: the move is finished or undone. Undo moves the
// File back, the same way.
func (e *Editor) MoveInJob(ctx context.Context, jobID string, m Move) (*Result, error) {
	writeMu.Lock()
	defer writeMu.Unlock()
	return e.move(context.WithoutCancel(ctx), jobID, m, "", nil)
}

// move makes the move m (see MoveInJob). undoOf is the entry it undoes, if it is an undo, whose
// check of the File replaces the guard.
func (e *Editor) move(ctx context.Context, jobID string, m Move, undoOf string, check func(abs string) error) (*Result, error) {
	kind := cmp.Or(m.Kind, model.EditMove)
	relocate, ok := moveKinds[kind]
	if !ok {
		return nil, fmt.Errorf("%q is no kind of move", kind)
	}
	if !writable(m.LibraryID) {
		return nil, ErrEditingOff
	}
	if m.Path == m.NewPath {
		return &Result{Changed: false}, nil
	}
	lib, err := e.ds.Library(ctx).Get(m.LibraryID)
	if err != nil {
		return nil, err
	}
	src := absPath(lib, m.Path)
	var mf *model.MediaFile
	if m.TrackID != "" {
		mf, err = e.ds.MediaFile(ctx).Get(m.TrackID)
		switch {
		case errors.Is(err, model.ErrNotFound) && undoOf != "":
			// The Track is gone (its File was out of the Library): the File goes back alone.
			mf, err = nil, nil
		case err != nil:
			return nil, err
		}
	}
	if mf != nil {
		switch {
		case mf.LibraryID == m.LibraryID && mf.Path == m.Path:
		case undoOf != "" && mf.LibraryID == m.LibraryID && mf.Path == m.NewPath:
			// The move being undone left the Track's row where it was.
			relocate = false
		default:
			return nil, fmt.Errorf("%w: the Track isn't at %s", ErrChangedOnDisk, m.Path)
		}
	}
	switch {
	case check != nil:
		if err := check(src); err != nil {
			return nil, err
		}
	case mf != nil:
		if err := e.guard(&file{mf: mf, lib: lib, abs: src}); err != nil {
			return nil, err
		}
	case !safefile.Exists(e.fsys, src):
		return nil, fmt.Errorf("%w: the File isn't there", ErrChangedOnDisk)
	}

	newDir, err := safefile.EnsureDir(e.fsys, lib.Path, path.Dir(m.NewPath))
	if err != nil {
		return nil, err
	}
	newPath := path.Join(newDir, path.Base(m.NewPath))
	entry := &model.EditEntry{
		JobID: jobID, Kind: kind, State: model.EditPending, LibraryID: m.LibraryID,
		MediaFileID: m.TrackID, Path: m.Path, NewPath: newPath, UndoOf: undoOf,
	}
	if u, ok := request.UserFrom(ctx); ok {
		entry.UserID = u.ID
	}
	journal := e.ds.EditJournal(ctx)
	if err := journal.Put(entry); err != nil {
		return nil, fmt.Errorf("recording the move in the Edit journal: %w", err)
	}
	err = safefile.Move(e.fsys, src, absPath(lib, newPath), safefile.MoveHooks{
		Copied: func(sum []byte) error {
			entry.CopySum = hex.EncodeToString(sum)
			if err := journal.Put(entry); err != nil {
				return fmt.Errorf("recording the move in the Edit journal: %w", err)
			}
			return nil
		},
		Placed: func() error {
			if mf == nil || !relocate {
				return nil
			}
			return e.relocate(ctx, mf.ID, lib, newPath)
		},
	})
	res := &Result{Changed: true, Entry: entry.ID, Track: mf}
	switch {
	case errors.Is(err, safefile.ErrSourceLeft):
		// Moved; the entry stays pending, so the next start removes the old copy (Settle).
		log.Warn(ctx, "A moved File's old copy could not be removed; it is removed at the next start", "path", src, err)
		entry.Error = err.Error()
		_ = journal.Put(entry)
		res.Warning = "The File was moved, but its old copy is still there; it is removed when the app next starts."
	case errors.Is(err, safefile.ErrBothLeft):
		// The File is at both paths: the entry stays pending, so the next start finishes the move
		// (Settle), and the Track follows it.
		log.Error(ctx, "A move left its File at both paths; the next start finishes it", "path", src, err)
		entry.Error = err.Error()
		_ = journal.Put(entry)
		return nil, err
	case err != nil:
		log.Warn(ctx, "Could not move a File", "path", m.Path, "newPath", newPath, err)
		e.close(ctx, entry, false, err)
		return nil, err
	default:
		e.close(ctx, entry, true, nil)
	}
	log.Info(ctx, "Moved a File", "path", m.Path, "newPath", newPath, "track", m.TrackID)
	if mf != nil {
		if got, err := e.ds.MediaFile(ctx).Get(mf.ID); err == nil {
			res.Track = got
		}
	}
	return res, nil
}

// relocate points the Track id at the File now at rel in lib, keeping its ID, in one transaction.
func (e *Editor) relocate(ctx context.Context, id string, lib *model.Library, rel string) error {
	return e.ds.WithTxRetry(ctx, func(ctx context.Context, tx model.DataStore) error {
		if err := tx.MediaFile(ctx).Relocate(id, lib.ID, rel, model.FolderID(*lib, path.Dir(rel))); err != nil {
			return fmt.Errorf("moving the Track: %w", err)
		}
		return nil
	}, "move a File")
}

func absPath(lib *model.Library, rel string) string {
	return filepath.Join(lib.Path, filepath.FromSlash(rel))
}

// undoMove undoes a move (see undoers): the File goes back from NewPath to Path, the same way it
// was moved, and its Track with it. It is refused while any later write to the File stands
// (ErrChangedLater), when the File at NewPath isn't the one moved (ErrChangedOnDisk), and when
// Path is taken (safefile.ErrDestTaken).
func (e *Editor) undoMove(ctx context.Context, jobID string, entry *model.EditEntry) (*Result, error) {
	err := e.checkLater(ctx, entry, func(l model.EditEntry) error {
		return fmt.Errorf("%w: the File was written or moved since", ErrChangedLater)
	})
	if err != nil {
		return nil, err
	}
	check := func(abs string) error {
		got, _, err := safefile.Hash(e.fsys, abs)
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%w: the File isn't where the move left it", ErrChangedOnDisk)
		}
		if err != nil {
			return err
		}
		if hex.EncodeToString(got) != entry.CopySum {
			return fmt.Errorf("%w: the File changed since it was moved", ErrChangedOnDisk)
		}
		return nil
	}
	m := Move{LibraryID: entry.LibraryID, TrackID: entry.MediaFileID, Path: entry.NewPath, NewPath: entry.Path, Kind: entry.Kind}
	return e.move(ctx, jobID, m, entry.ID, check)
}

// settleMove finishes or undoes a move a stop interrupted (see Settle), from what is on disk
// (safefile.SettleMove), and points its Track at where the File is.
func (e *Editor) settleMove(ctx context.Context, lib *model.Library, entry *model.EditEntry) {
	src, dest := absPath(lib, entry.Path), absPath(lib, entry.NewPath)
	moved, err := safefile.SettleMove(e.fsys, src, dest, sumOf(entry))
	if err != nil && !errors.Is(err, safefile.ErrSourceLeft) {
		log.Error(ctx, "Could not settle an interrupted move; it is tried again at the next start", "entry", entry.ID, "path", src, "newPath", dest, err)
		return
	}
	if moved && moveKinds[entry.Kind] && entry.MediaFileID != "" {
		mf, gerr := e.ds.MediaFile(ctx).Get(entry.MediaFileID)
		if gerr == nil && mf.LibraryID == entry.LibraryID && mf.Path == entry.Path {
			if rerr := e.relocate(ctx, mf.ID, lib, entry.NewPath); rerr != nil {
				log.Error(ctx, "Could not move the Track of an interrupted move; it is tried again at the next start", "entry", entry.ID, rerr)
				return
			}
		}
	}
	if err != nil {
		// Moved, but the old copy is still there: removed at the next start.
		log.Warn(ctx, "A moved File's old copy could not be removed; it is tried again at the next start", "path", src, err)
		return
	}
	log.Info(ctx, "Settled an interrupted move", "path", entry.Path, "newPath", entry.NewPath, "moved", moved)
	e.close(ctx, entry, moved, errors.New("interrupted by a restart"))
}

// RescanTargets rescans the given folders (after a job's moves, which don't rescan each File), so
// the moved Tracks are read at their new place into the same rows, and folders the moves emptied
// are seen gone. A busy scanner is waited for, a little.
func (e *Editor) RescanTargets(ctx context.Context, targets []model.ScanTarget) error {
	writeMu.Lock()
	defer writeMu.Unlock()
	return e.scan(ctx, targets)
}

// FS is the filesystem the Editor writes through (the real one, unless a test set another).
func (e *Editor) FS() safefile.FS { return e.fsys }
