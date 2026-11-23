package tagedit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/core/quarantine"
	"github.com/navidrome/navidrome/core/safefile"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
)

// The case merge's writes (#169, ADR-0011): a File or folder renamed or moved with all it holds,
// by one server-side rename (RenameInJob), and a Duplicate sent to Quarantine once it is confirmed
// byte-identical to the File it duplicates (QuarantineDuplicateInJob). Each is journaled with its
// job, settled after a stop, and undone from the Edit history.

func init() { undoers[model.EditRename] = (*Editor).undoRename }

// ErrNotDuplicate is a File the case merge would send to Quarantine as a Duplicate whose bytes
// aren't those of the File it would duplicate: it stays, for the admin to decide.
var ErrNotDuplicate = errors.New("the File isn't byte-identical to the one it would duplicate; it stays, for you to decide")

// Rename is a File or folder to rename or move within its library, with all it holds (see
// RenameInJob). Paths are relative to the library, slash-separated.
type Rename struct {
	LibraryID int    `json:"libraryId"`
	Path      string `json:"path"`
	NewPath   string `json:"newPath"`
}

// CheckRename is the refusal a rename meets before anything is read: its library isn't writable.
func (e *Editor) CheckRename(r Rename) error {
	if !writable(r.LibraryID) {
		return ErrEditingOff
	}
	return nil
}

// RenameInJob renames the File or folder r.Path to r.NewPath (whose folder must exist), with all it
// holds, as one write of the edit job jobID: one server-side rename that never replaces anything
// at the new path, in any letter case (safefile.RenameEntry), journaled (EditRename) before it is
// made. Every Track under the path renamed is pointed at its place under the new one, keeping its
// ID (and so its favorites, ratings, plays and Playlist entries); Files no scan found yet go with
// the folder, and the job's rescan finds them.
//
// On the NAS over SMB1 a name with a case twin beside it may reach the twin: the rename then
// renames the twin, which loses nothing, and the entry records the name it took (told by which
// name left the folder), with a warning, so its Tracks follow it and its undo puts back that one.
func (e *Editor) RenameInJob(ctx context.Context, jobID string, r Rename) (*Result, error) {
	writeMu.Lock()
	defer writeMu.Unlock()
	return e.rename(context.WithoutCancel(ctx), jobID, r, "")
}

func (e *Editor) rename(ctx context.Context, jobID string, r Rename, undoOf string) (*Result, error) {
	if err := e.CheckRename(r); err != nil {
		return nil, err
	}
	if r.Path == r.NewPath {
		return &Result{}, nil
	}
	lib, err := e.ds.Library(ctx).Get(r.LibraryID)
	if err != nil {
		return nil, err
	}
	src := absPath(lib, r.Path)
	if !safefile.Exists(e.fsys, src) {
		return nil, fmt.Errorf("%w: %s isn't there", ErrChangedOnDisk, r.Path)
	}
	// The new path's folder is made if it is gone (an undo's, which a later step of the job it
	// undoes removed once empty), reusing one whose name differs only in letter case.
	newDir, err := safefile.EnsureDir(e.fsys, lib.Path, path.Dir(r.NewPath))
	if err != nil {
		return nil, err
	}
	r.NewPath = path.Join(newDir, path.Base(r.NewPath))
	entry := &model.EditEntry{JobID: jobID, Kind: model.EditRename, State: model.EditPending, LibraryID: r.LibraryID,
		Path: r.Path, NewPath: r.NewPath, UndoOf: undoOf}
	if u, ok := request.UserFrom(ctx); ok {
		entry.UserID = u.ID
	}
	journal := e.ds.EditJournal(ctx)
	if err := journal.Put(entry); err != nil {
		return nil, fmt.Errorf("recording the rename in the Edit journal: %w", err)
	}
	renameFn := safefile.RenameEntry
	if undoOf != "" {
		// An undo puts back a name the merge took away, beside its twin where the filesystem
		// holds both.
		renameFn = safefile.RenameBack
	}
	renamed, err := renameFn(e.fsys, src, absPath(lib, r.NewPath))
	res := &Result{Changed: true, Entry: entry.ID}
	switch {
	case errors.Is(err, safefile.ErrRenameUnverified):
		// Renamed, but which twin went can't be told: the Tracks stay where they are, and the
		// job's rescan finds the Files at their new place (matching each to its Track).
		log.Warn(ctx, "Renamed, but could not tell which name the rename took", "path", r.Path, "newPath", r.NewPath, err)
		res.Warning = "Renamed; the Tracks follow at the next scan."
		e.close(ctx, entry, true, nil)
		return res, nil
	case err != nil:
		log.Warn(ctx, "Could not rename", "path", r.Path, "newPath", r.NewPath, err)
		e.close(ctx, entry, false, err)
		return nil, err
	}
	if got := relPath(lib, renamed); got != r.Path {
		log.Warn(ctx, "The NAS renamed the case twin of the name asked for", "asked", r.Path, "renamed", got, "newPath", r.NewPath)
		res.Warning = fmt.Sprintf("The NAS renamed %s, the case twin of %s; nothing is lost.", got, r.Path)
		entry.Path = got
	}
	if err := e.relocateUnder(ctx, lib, entry.Path, entry.NewPath); err != nil {
		log.Error(ctx, "Renamed, but could not point the Tracks at their new place; the next scan does", "path", entry.Path, err)
		res.Warning = strings.TrimSpace(res.Warning + " The Tracks follow at the next scan.")
	}
	e.close(ctx, entry, true, nil)
	log.Info(ctx, "Renamed", "path", entry.Path, "newPath", entry.NewPath, "job", jobID)
	return res, nil
}

func relPath(lib *model.Library, abs string) string {
	rel, err := filepath.Rel(lib.Path, abs)
	if err != nil {
		return abs
	}
	return filepath.ToSlash(rel)
}

// relocateUnder points every Track at from, or under it (in its exact letter case), at its place
// under to, keeping its ID, in one transaction (model.MediaFileRepository.Repath: the Files are as
// they were). A Track in Quarantine stays: its File isn't there, and its restore brings it back
// where it was.
func (e *Editor) relocateUnder(ctx context.Context, lib *model.Library, from, to string) error {
	return e.ds.WithTxRetry(ctx, func(ctx context.Context, tx model.DataStore) error {
		repo := tx.MediaFile(ctx)
		mfs, err := repo.GetAll(model.QueryOptions{Filters: squirrel.And{
			squirrel.Eq{"media_file.library_id": lib.ID, "media_file.quarantined": false},
			squirrel.Or{
				squirrel.Expr("media_file.path = ? collate binary", from),
				squirrel.Expr("(media_file.path >= ? collate binary and media_file.path < ? collate binary)", from+"/", from+"0"),
			},
		}})
		if err != nil {
			return err
		}
		for _, mf := range mfs {
			rel := to + strings.TrimPrefix(mf.Path, from)
			if err := repo.Repath(mf.ID, rel, model.FolderID(*lib, path.Dir(rel))); err != nil {
				return fmt.Errorf("moving the Track %s: %w", mf.ID, err)
			}
		}
		return nil
	}, "rename a folder")
}

// undoRename undoes a rename (see undoers): what is at NewPath goes back to Path, its Tracks with
// it, journaled with UndoOf. It is refused while a later write to what it left at NewPath stands
// (ErrChangedLater), and when NewPath isn't there any more (ErrChangedOnDisk). Path beside a case
// twin is put back only where the filesystem holds both names: on the NAS it is refused
// (safefile.ErrDestTaken), and nothing moves.
func (e *Editor) undoRename(ctx context.Context, jobID string, entry *model.EditEntry) (*Result, error) {
	err := e.checkLater(ctx, entry, func(l model.EditEntry) error {
		return fmt.Errorf("%w: %s was moved or written since", ErrChangedLater, entry.NewPath)
	})
	if err != nil {
		return nil, err
	}
	return e.rename(ctx, jobID, Rename{LibraryID: entry.LibraryID, Path: entry.NewPath, NewPath: entry.Path}, entry.ID)
}

// settleRename settles a rename a stop interrupted (see Settle): the new name there means it was
// made, and the Tracks follow it (when the old name is gone; else the next scan matches them);
// otherwise it wasn't.
func (e *Editor) settleRename(ctx context.Context, lib *model.Library, entry *model.EditEntry) {
	renamed := safefile.Exists(e.fsys, absPath(lib, entry.NewPath))
	if renamed && !safefile.Exists(e.fsys, absPath(lib, entry.Path)) {
		if err := e.relocateUnder(ctx, lib, entry.Path, entry.NewPath); err != nil {
			log.Error(ctx, "Could not point an interrupted rename's Tracks at their new place; it is tried again at the next start", "entry", entry.ID, err)
			return
		}
	}
	log.Info(ctx, "Settled an interrupted rename", "path", entry.Path, "newPath", entry.NewPath, "renamed", renamed)
	e.close(ctx, entry, renamed, errors.New("interrupted by a restart"))
}

// ErrNotEmpty is a folder the case merge would remove that isn't empty: it stays.
var ErrNotEmpty = errors.New("the folder isn't empty; it stays")

// RemoveEmptyDir removes the folder rel of the library libID, which the moves before it left
// empty: one with anything in it (a hidden File too) stays (ErrNotEmpty), and one gone already is
// no change. It isn't journaled: undoing the moves that emptied it makes it again.
func (e *Editor) RemoveEmptyDir(ctx context.Context, libID int, rel string) (*Result, error) {
	writeMu.Lock()
	defer writeMu.Unlock()
	if !writable(libID) {
		return nil, ErrEditingOff
	}
	lib, err := e.ds.Library(ctx).Get(libID)
	if err != nil {
		return nil, err
	}
	abs := absPath(lib, rel)
	info, err := e.fsys.Lstat(abs)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return &Result{}, nil
	case err != nil:
		return nil, err
	case !info.IsDir():
		return nil, fmt.Errorf("%w: %s is no folder", ErrNotEmpty, rel)
	}
	entries, err := e.fsys.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	if len(entries) > 0 {
		return nil, fmt.Errorf("%w: %s holds %s", ErrNotEmpty, rel, entries[0].Name())
	}
	if err := e.fsys.Remove(abs); err != nil {
		return nil, err
	}
	log.Info(ctx, "Removed a folder the case merge emptied", "path", rel)
	return &Result{Changed: true}, nil
}

// Duplicate is a File the case merge sends to Quarantine (NewPath, beneath it) as a Duplicate of
// the File Keep, which stays: see QuarantineDuplicateInJob. TrackID is its Track, if a scan found
// it.
type Duplicate struct {
	LibraryID int    `json:"libraryId"`
	TrackID   string `json:"trackId,omitempty"`
	Path      string `json:"path"`
	NewPath   string `json:"newPath"`
	Keep      string `json:"keep"`
}

// CheckDuplicate is the refusal a Duplicate's move meets before its File is read, if any.
func (e *Editor) CheckDuplicate(ctx context.Context, d Duplicate) error {
	if !writable(d.LibraryID) {
		return ErrEditingOff
	}
	if !quarantine.Within(d.NewPath) || quarantine.Within(d.Path) || d.Keep == "" {
		return fmt.Errorf("%w: a Duplicate goes from the Library into Quarantine", ErrNotInQuarantine)
	}
	if d.TrackID != "" {
		if _, err := e.ds.MediaFile(ctx).Get(d.TrackID); err != nil {
			return err
		}
	}
	return nil
}

// QuarantineDuplicateInJob sends the File d.Path to d.NewPath in Quarantine, as part of the edit
// job jobID, once it is confirmed byte-identical to d.Keep (same size, same SHA-256), else it stays
// (ErrNotDuplicate). The move is Quarantine's (#168): the Library's one move, journaled as
// EditQuarantine, its Track (if it has one) leaving the Library marked quarantined, so its undo
// (or a restore) brings back the same Track.
func (e *Editor) QuarantineDuplicateInJob(ctx context.Context, jobID string, d Duplicate) (*Result, error) {
	writeMu.Lock()
	defer writeMu.Unlock()
	ctx = context.WithoutCancel(ctx)

	if err := e.CheckDuplicate(ctx, d); err != nil {
		return nil, err
	}
	lib, err := e.ds.Library(ctx).Get(d.LibraryID)
	if err != nil {
		return nil, err
	}
	if err := e.sameBytes(absPath(lib, d.Path), absPath(lib, d.Keep)); err != nil {
		return nil, err
	}
	if _, err := quarantine.Ensure(e.fsys, lib.Path); err != nil {
		return nil, err
	}
	m := Move{LibraryID: lib.ID, TrackID: d.TrackID, Path: d.Path, NewPath: d.NewPath, Kind: model.EditQuarantine}
	return e.quarantineMove(ctx, func(before func() error) (*Result, error) {
		m.Before = before
		return e.move(ctx, jobID, m, "", nil)
	}, d.TrackID, true)
}

// sameBytes refuses (ErrNotDuplicate) a File whose bytes aren't those of keep.
func (e *Editor) sameBytes(name, keep string) error {
	a, sizeA, err := safefile.Hash(e.fsys, name)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrChangedOnDisk, err)
	}
	b, sizeB, err := safefile.Hash(e.fsys, keep)
	if err != nil {
		return fmt.Errorf("%w: the File it duplicates: %v", ErrNotDuplicate, err)
	}
	if sizeA != sizeB || !bytes.Equal(a, b) {
		return ErrNotDuplicate
	}
	return nil
}
