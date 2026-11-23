package safefile

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/navidrome/navidrome/core/libpath"
)

var (
	// ErrDestTaken is a move whose destination holds a File already, in any letter case. That
	// File is never replaced.
	ErrDestTaken = errors.New("a File is already at the new path")
	// ErrSourceLeft is a move that put the File at its new path, and recorded it there, but
	// couldn't remove it from its old one. The move is done; SettleMove removes the old copy.
	ErrSourceLeft = errors.New("the File was moved, but its old copy could not be removed")
	// ErrCaseTwin is a folder with a twin beside it whose name differs only in letter case: on
	// the case-insensitive NAS a path into either may reach the other, so nothing is moved
	// through it until the two are merged.
	ErrCaseTwin = errors.New("a folder on the way has a twin whose name differs only in letter case; merge them first")
	// ErrUnrecorded is a move whose Placed hook failed and that couldn't be undone (the File
	// couldn't be renamed back, or its copy at the new path removed), or a rename whose outcome
	// can't be told: the File isn't lost, and the move's settle (SettleRename, SettleMove)
	// finishes it from what is on disk.
	ErrUnrecorded = errors.New("the move couldn't be recorded, nor undone; it is settled from what is on disk")
	// ErrGone is a rename after which the File is at neither its old path nor its new one (or
	// what is at the new one isn't it): something else moved or removed it meanwhile.
	ErrGone = errors.New("the File is at neither its old path nor its new one after the rename")
	// ErrOutside is a folder that leads out of the root it should be in (a symlink).
	ErrOutside = errors.New("the path leads out of its library")
)

// MoveTemp is the hidden copy a move across filesystems makes beside dest, until it is checked
// and placed.
func MoveTemp(dest string) string { return hidden(dest, "vu-move") }

// MoveHooks let the caller record a move as it goes (its Edit journal entry, its Track's path).
// Any may be nil.
type MoveHooks struct {
	// Renaming gets the File's info just before it is renamed to dest (a move on one filesystem):
	// a restart tells the File by it (RenameMark, SettleRename). An error stops the move.
	Renaming func(info fs.FileInfo) error
	// Copied gets the File's SHA-256 once its copy beside dest is checked, before the copy is
	// placed (a move across filesystems): a restart tells the copy by it (SettleMove). An error
	// stops the move.
	Copied func(sum []byte) error
	// Placed runs once the File (or its copy) is at dest, before anything is removed: e.g. to point
	// the File's Track at dest. An error undoes the move: the File is renamed back, or the copy at
	// dest removed.
	Placed func() error
}

// noReplaceRenamer is a filesystem that can rename without replacing a File at the new name
// (OS, by renameat2's RENAME_NOREPLACE on Linux).
type noReplaceRenamer interface {
	RenameNoReplace(oldname, newname string) error
}

// renameNoReplace renames oldname to newname once newname is checked free, in any letter case,
// with a rename that refuses a File there where the filesystem can (see OS.RenameNoReplace).
func renameNoReplace(fsys FS, oldname, newname string) error {
	if err := checkFree(fsys, newname); err != nil {
		return err
	}
	var err error
	if r, ok := fsys.(noReplaceRenamer); ok {
		err = r.RenameNoReplace(oldname, newname)
	} else {
		err = fsys.Rename(oldname, newname)
	}
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%w: %s", ErrDestTaken, filepath.Base(newname))
	}
	return err
}

// Move moves the File at src to dest (which must be free, in any letter case: ErrDestTaken), in
// a folder that exists, without ever losing it or leaving half of it anywhere. It is how the app
// moves a File within the Library (Organize, Quarantine).
//
// On one filesystem (the share), it is a rename, made on the server: no byte is copied. The NAS
// is case-insensitive, and a plain rename over SMB may replace a File at dest, so dest is checked
// free (in any letter case) just before, and the rename itself refuses a File there where the
// kernel can (RENAME_NOREPLACE). hooks.Renaming records the File first; hooks.Placed then records
// the move, and if it fails the File is renamed back (ErrUnrecorded if it can't be). A failed
// rename moved nothing, unless the File is found at dest only, which is taken as renamed. There is
// never a moment with two copies.
//
// Across filesystems (the rename fails with EXDEV), the File is copied to a hidden temp beside
// dest (MoveTemp), flushed, read back and checked (size and SHA-256), given the File's
// modification time and permissions, then placed at dest by a hard link (which fails if dest
// exists) or, without hard links (SMB1), a rename once dest is checked free again. Only once
// hooks.Placed has recorded the move is the File removed from src. At every step the File is whole
// at src, or at dest, or both:
//
//   - a failure before the copy is placed leaves the File at src, and the temp is removed;
//   - hooks.Placed failing removes the copy from dest again (ErrUnrecorded if it can't:
//     SettleMove finishes the move);
//   - src that can't be removed is ErrSourceLeft: the move is done, and SettleMove removes it.
//
// A restart in the middle is settled by SettleRename (from the mark of what hooks.Renaming got),
// or SettleMove (from the checksum hooks.Copied got).
func Move(fsys FS, src, dest string, hooks MoveHooks) error {
	if err := CheckReplaceable(fsys, src); err != nil {
		return err
	}
	if err := checkFree(fsys, dest); err != nil {
		return err
	}
	err := rename(fsys, src, dest, hooks)
	if !errors.Is(err, errCrossDevice) {
		return err
	}
	return copyMove(fsys, src, dest, hooks)
}

// errCrossDevice is a rename across filesystems: the File is copied instead.
var errCrossDevice = errors.New("across filesystems")

// rename moves the File at src to dest by a rename (see Move), or returns errCrossDevice, having
// done nothing, when src and dest are on two filesystems.
func rename(fsys FS, src, dest string, hooks MoveHooks) error {
	info, err := fsys.Lstat(src)
	if err != nil {
		return err
	}
	if hooks.Renaming != nil {
		if err := hooks.Renaming(info); err != nil {
			return err
		}
	}
	if err := renameNoReplace(fsys, src, dest); err != nil {
		if isCrossDevice(err) {
			return errCrossDevice
		}
		if Exists(fsys, src) {
			return err // nothing moved
		}
		// The rename may have happened, though it reported an error (SMB): the File is at dest if
		// what is there is it.
		destInfo, lerr := fsys.Lstat(dest)
		if lerr != nil || RenameMark(destInfo) != RenameMark(info) {
			return fmt.Errorf("%w: %v", ErrGone, err)
		}
	}
	if hooks.Placed != nil {
		if err := hooks.Placed(); err != nil {
			if berr := renameNoReplace(fsys, dest, src); berr != nil {
				return fmt.Errorf("%w: %v, then %v", ErrUnrecorded, err, berr)
			}
			return err
		}
	}
	return nil
}

// copyMove moves the File at src to dest across filesystems: copy, check, place, delete (see Move).
func copyMove(fsys FS, src, dest string, hooks MoveHooks) (err error) {
	temp := MoveTemp(dest)
	if err := fsys.Remove(temp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("removing an earlier move's copy: %w", err)
	}
	defer func() {
		if err != nil {
			_ = fsys.Remove(temp)
		}
	}()
	sum, size, info, err := CopyToTemp(fsys, src, temp)
	if err != nil {
		return err
	}
	if err := CheckCopy(fsys, temp, sum, size); err != nil {
		return err
	}
	// The scanner tells a changed File by its modification time; best effort, as the owner.
	_ = fsys.Chtimes(temp, info.ModTime())
	KeepAttrs(fsys, info, temp)
	if hooks.Copied != nil {
		if err := hooks.Copied(sum); err != nil {
			return err
		}
	}
	if err := place(fsys, temp, dest); err != nil {
		return err
	}
	if hooks.Placed != nil {
		if err := hooks.Placed(); err != nil {
			if rmErr := fsys.Remove(dest); rmErr != nil {
				// Both copies are there: SettleMove finishes the move from them.
				return fmt.Errorf("%w: %v, then %v", ErrUnrecorded, err, rmErr)
			}
			return err
		}
	}
	if err := fsys.Remove(src); err != nil {
		return fmt.Errorf("%w: %v", ErrSourceLeft, err)
	}
	return nil
}

// RenameMark is what a restart tells a renamed File by (see SettleRename): its size and
// modification time, which a rename keeps. It is kept where a copy's checksum would be (an Edit
// journal entry's CopySum), and told from one by IsRenameMark.
func RenameMark(info fs.FileInfo) string {
	return fmt.Sprintf("%s%d:%d", renameMarkPrefix, info.Size(), info.ModTime().Unix())
}

const renameMarkPrefix = "moved:"

// IsRenameMark reports whether s is a RenameMark, not a copy's checksum.
func IsRenameMark(s string) bool { return strings.HasPrefix(s, renameMarkPrefix) }

// SettleRename tells how a rename from src to dest the app stopped in the middle of ended, from
// where the File is and the mark (RenameMark) recorded before it: at src only, it wasn't renamed;
// at dest only, it was, if the File there matches the mark (else it fails, as it can't vouch for
// it); at both, the File at dest is another, and it wasn't; at neither, it fails. The File is never
// touched; only when it wasn't moved is a temp a copy across filesystems left (MoveTemp) removed:
// a move is journaled as a rename until the rename turns out to cross filesystems.
func SettleRename(fsys FS, src, dest, mark string) (moved bool, err error) {
	srcThere, destThere := Exists(fsys, src), Exists(fsys, dest)
	switch {
	case srcThere:
		if err := fsys.Remove(MoveTemp(dest)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return false, fmt.Errorf("removing the move's copy: %w", err)
		}
		return false, nil
	case !destThere:
		return false, fmt.Errorf("the File is at neither %s nor %s", src, dest)
	}
	info, err := fsys.Lstat(dest)
	if err != nil {
		return false, err
	}
	if RenameMark(info) != mark {
		return false, fmt.Errorf("the File at %s isn't the one renamed there (%s, not %s)", dest, RenameMark(info), mark)
	}
	return true, nil
}

// checkFree refuses dest when anything is there, or in its folder under a name differing only in
// letter case (which the case-insensitive NAS takes for the same name).
func checkFree(fsys FS, dest string) error {
	if Exists(fsys, dest) {
		return fmt.Errorf("%w: %s", ErrDestTaken, filepath.Base(dest))
	}
	entries, err := fsys.ReadDir(filepath.Dir(dest))
	if err != nil {
		return err
	}
	for _, e := range entries {
		if libpath.SameName(e.Name(), filepath.Base(dest)) {
			return fmt.Errorf("%w: %s", ErrDestTaken, e.Name())
		}
	}
	return nil
}

// place puts the checked copy at temp at dest, never over a File there.
func place(fsys FS, temp, dest string) error {
	err := fsys.Link(temp, dest)
	switch {
	case err == nil:
		_ = fsys.Remove(temp)
		return nil
	case errors.Is(err, fs.ErrExist):
		return fmt.Errorf("%w: %s", ErrDestTaken, filepath.Base(dest))
	}
	// No hard links (SMB1): a rename, which would replace dest, so dest is checked free again.
	if err := checkFree(fsys, dest); err != nil {
		return err
	}
	if err := fsys.Rename(temp, dest); err != nil {
		return fmt.Errorf("moving the copy into place: %w", err)
	}
	return nil
}

// SettleMove finishes or undoes a Move the app stopped in the middle of, from what is at src,
// dest and the temp beside dest. sum is the checksum Move's Copied hook got, or nil if it stopped
// before. It reports whether the File is now at dest (moved) or still at src. It never removes
// the only copy of a File, and never one it can't vouch for: when it can't tell, it leaves
// everything and fails.
func SettleMove(fsys FS, src, dest string, sum []byte) (moved bool, err error) {
	temp := MoveTemp(dest)
	isCopy := func(name string) bool {
		if len(sum) == 0 {
			return false
		}
		got, _, err := Hash(fsys, name)
		return err == nil && bytes.Equal(got, sum)
	}
	removeTemp := func() error {
		if err := fsys.Remove(temp); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("removing the move's copy: %w", err)
		}
		return nil
	}
	srcThere := Exists(fsys, src)
	switch {
	case Exists(fsys, dest) && isCopy(dest):
		// Placed: only the old copy may be left to remove.
		if srcThere && isCopy(src) {
			if err := fsys.Remove(src); err != nil {
				return true, fmt.Errorf("%w: %v", ErrSourceLeft, err)
			}
		}
		return true, removeTemp()
	case srcThere:
		// Never placed (or what is at dest is another File): the File stays.
		return false, removeTemp()
	case Exists(fsys, temp) && isCopy(temp) && !Exists(fsys, dest):
		// Only the checked copy is left.
		if err := fsys.Rename(temp, dest); err != nil {
			return false, fmt.Errorf("moving the copy into place: %w", err)
		}
		return true, nil
	}
	return false, fmt.Errorf("the File is neither at %s nor at %s, and there is no checked copy of it", src, dest)
}

// EnsureDir makes the folder rel (slash-separated) under root, folder by folder, and returns its
// path as it is on disk: a folder whose name differs from the one asked for only in letter case
// is reused, so the case-insensitive NAS never gets two such folders. A folder that has a twin
// already is refused (ErrCaseTwin), as is one leading out of root (ErrOutside) or a File in the
// way.
func EnsureDir(fsys FS, root, rel string) (string, error) {
	realRoot, err := fsys.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	var got []string
	dir := root
	for _, name := range strings.Split(strings.Trim(rel, "/"), "/") {
		if name == "" || name == "." {
			continue
		}
		existing, err := sameNames(fsys, dir, name)
		if err != nil {
			return "", err
		}
		switch len(existing) {
		case 0:
			if err := fsys.Mkdir(filepath.Join(dir, name)); err != nil && !errors.Is(err, fs.ErrExist) {
				return "", fmt.Errorf("making the folder %q: %w", name, err)
			}
		case 1:
			name = existing[0]
		default:
			return "", fmt.Errorf("%w: %s", ErrCaseTwin, strings.Join(existing, ", "))
		}
		dir = filepath.Join(dir, name)
		info, err := fsys.Stat(dir)
		if err != nil {
			return "", err
		}
		if !info.IsDir() {
			return "", fmt.Errorf("%w: %q is in the way: it is not a folder", ErrDestTaken, dir)
		}
		real, err := fsys.EvalSymlinks(dir)
		if err != nil {
			return "", err
		}
		if real != realRoot && !strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
			return "", fmt.Errorf("%w: %q", ErrOutside, dir)
		}
		got = append(got, name)
	}
	return strings.Join(got, "/"), nil
}

// sameNames are the names in dir the NAS takes for name.
func sameNames(fsys FS, dir, name string) ([]string, error) {
	entries, err := fsys.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var res []string
	for _, e := range entries {
		if libpath.SameName(e.Name(), name) {
			res = append(res, e.Name())
		}
	}
	return res, nil
}

// RemoveEmptyDirs removes the folder rel (slash-separated) under root if it is empty, then each
// folder above it that is left empty, up to root (never removed). It returns the folders it
// removed. Anything in a folder (a hidden File too) keeps it.
func RemoveEmptyDirs(fsys FS, root, rel string) []string {
	var removed []string
	for rel = strings.Trim(rel, "/"); rel != "" && rel != "."; rel = parentDir(rel) {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		info, err := fsys.Lstat(abs)
		if err != nil || !info.IsDir() {
			break
		}
		entries, err := fsys.ReadDir(abs)
		if err != nil || len(entries) > 0 {
			break
		}
		if err := fsys.Remove(abs); err != nil {
			break
		}
		removed = append(removed, rel)
	}
	return removed
}

func parentDir(rel string) string {
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		return rel[:i]
	}
	return ""
}
