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
	// ErrBothLeft is a move whose Placed hook failed, and whose copy at the new path couldn't be
	// removed again: the File is whole at both paths, and SettleMove finishes the move.
	ErrBothLeft = errors.New("the move couldn't be recorded, and its copy at the new path couldn't be removed")
	// ErrOutside is a folder that leads out of the root it should be in (a symlink).
	ErrOutside = errors.New("the path leads out of its library")
)

// MoveTemp is the hidden copy a move makes beside dest, until it is checked and placed.
func MoveTemp(dest string) string { return hidden(dest, "vu-move") }

// MoveHooks let the caller record a move as it goes (its Edit journal entry, its Track's path).
// Either may be nil.
type MoveHooks struct {
	// Copied gets the File's SHA-256 once its copy beside dest is checked, before the copy is
	// placed: a restart tells the copy by it (SettleMove). An error stops the move.
	Copied func(sum []byte) error
	// Placed runs once the copy is at dest, before the File is removed from src: e.g. to point
	// the File's Track at dest. An error undoes the move (the copy at dest is removed).
	Placed func() error
}

// Move moves the File at src to dest (which must be free, in any letter case: ErrDestTaken), in
// a folder that exists, without ever losing it or leaving half of it anywhere. It is how the app
// moves a File within the Library (Organize, Quarantine): a rename can't be trusted across mounts
// (CIFS can't take one from another filesystem), and on the NAS it may replace a File, so the File
// is copied to a hidden temp beside dest (MoveTemp), flushed, read back and checked (size and
// SHA-256), given the File's modification time and permissions, then placed at dest by a hard link
// (which fails if dest exists) or, without hard links (SMB1), a rename once dest is checked free
// again. Only once hooks.Placed has recorded the move is the File removed from src.
//
// At every step the File is whole at src, or at dest, or both:
//
//   - a failure before the copy is placed leaves the File at src, and the temp is removed;
//   - hooks.Placed failing removes the copy from dest again (ErrBothLeft if it can't: SettleMove
//     finishes the move);
//   - src that can't be removed is ErrSourceLeft: the move is done, and SettleMove removes it.
//
// A restart in the middle is settled by SettleMove, from the checksum hooks.Copied got.
func Move(fsys FS, src, dest string, hooks MoveHooks) (err error) {
	if err := CheckReplaceable(fsys, src); err != nil {
		return err
	}
	if err := checkFree(fsys, dest); err != nil {
		return err
	}
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
				return fmt.Errorf("%w: %v, then %v", ErrBothLeft, err, rmErr)
			}
			return err
		}
	}
	if err := fsys.Remove(src); err != nil {
		return fmt.Errorf("%w: %v", ErrSourceLeft, err)
	}
	return nil
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
			return "", fmt.Errorf("%q is in the way: it is not a folder", dir)
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
