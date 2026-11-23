package safefile

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
)

// ErrRenameUnverified is a rename that was made (the new name is there) but whose old name can't
// be told from its folder's listing afterwards: nothing is lost, and the next scan reads what is
// there.
var ErrRenameUnverified = errors.New("the rename was made, but which name it took can't be told")

// RenameEntry renames the File or folder src to dest (another name in its folder, or in another
// folder of the same filesystem), with all it holds, by one server-side rename that never replaces
// anything at dest, in any letter case (ErrDestTaken). A link is refused (ErrLinked). It is how
// the case merge (#169) renames and moves folders: a rename can't lose data, whichever real folder
// the server applies it to.
//
// It returns the path that was renamed. On the case-insensitive NAS over SMB1, a name with a twin
// differing only in letter case (`Atb` beside `ATB`) may reach the twin, so the folder of src is
// listed before and after: the name that left it is the one renamed, src's twin if the server
// applied the rename to it. When that can't be told, the rename is done and ErrRenameUnverified
// says so, with src.
func RenameEntry(fsys FS, src, dest string) (string, error) {
	return renameEntry(fsys, src, dest, func() error { return renameNoReplace(fsys, src, dest) })
}

// exclusiveRenamer is a filesystem whose rename can refuse anything at the new name by itself,
// with no fallback (OS, by renameat2's RENAME_NOREPLACE on Linux): errors.ErrUnsupported where it
// can't.
type exclusiveRenamer interface {
	RenameExclusive(oldname, newname string) error
}

// RenameBack is RenameEntry for an undo that puts back a name a case merge took away: dest may
// have a twin beside it differing only in letter case (`Atb` beside `ATB`), which only a
// filesystem holding both names allows. Then the rename must refuse anything at dest by itself
// (RENAME_NOREPLACE, with no fallback): on the case-insensitive NAS it refuses the twin
// (ErrDestTaken), and where the filesystem can't refuse by itself, the twin is refused before.
// Anything at dest itself is always refused.
func RenameBack(fsys FS, src, dest string) (string, error) {
	if Exists(fsys, dest) {
		return "", fmt.Errorf("%w: %s", ErrDestTaken, filepath.Base(dest))
	}
	twins, err := sameNames(fsys, filepath.Dir(dest), filepath.Base(dest))
	if err != nil {
		return "", err
	}
	if len(twins) == 0 {
		return RenameEntry(fsys, src, dest)
	}
	r, ok := fsys.(exclusiveRenamer)
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrDestTaken, twins[0])
	}
	return renameEntry(fsys, src, dest, func() error {
		err := r.RenameExclusive(src, dest)
		if errors.Is(err, fs.ErrExist) || errors.Is(err, errors.ErrUnsupported) {
			return fmt.Errorf("%w: %s", ErrDestTaken, twins[0])
		}
		return err
	})
}

func renameEntry(fsys FS, src, dest string, rename func() error) (string, error) {
	info, err := fsys.Lstat(src)
	if err != nil {
		return "", err
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: %s", ErrLinked, src)
	}
	dir := filepath.Dir(src)
	twins, err := sameNames(fsys, dir, filepath.Base(src))
	if err != nil {
		return "", err
	}
	if err := rename(); err != nil {
		if isCrossDevice(err) || errors.Is(err, ErrDestTaken) || !Exists(fsys, dest) || Exists(fsys, src) && len(twins) == 1 {
			return "", err
		}
		// The rename may have happened, though it reported an error (SMB): told below.
	}
	if !Exists(fsys, dest) {
		return "", fmt.Errorf("%w: %s isn't there after the rename", ErrGone, filepath.Base(dest))
	}
	left, err := sameNames(fsys, dir, filepath.Base(src))
	if err != nil {
		return src, fmt.Errorf("%w: %v", ErrRenameUnverified, err)
	}
	var gone []string
	for _, t := range twins {
		if !slices.Contains(left, t) {
			gone = append(gone, t)
		}
	}
	if len(gone) != 1 {
		return src, fmt.Errorf("%w: %d of %v left %s", ErrRenameUnverified, len(gone), twins, dir)
	}
	return filepath.Join(dir, gone[0]), nil
}
