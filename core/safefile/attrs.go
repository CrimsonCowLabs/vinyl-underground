package safefile

import (
	"errors"
	"fmt"
	"io/fs"
)

// ErrLinked is a File with more than one name (a symlink, or a hard link). Replacing it with a
// copy would split it from its other names, so it isn't replaced.
var ErrLinked = errors.New("the File is a link (a symlink, or one of several hard links), so it can't be replaced")

// CheckReplaceable checks the File at name can be replaced by a copy without losing what it is:
// it must be a regular File with one name.
func CheckReplaceable(fsys FS, name string) error {
	info, err := fsys.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || links(info) > 1 {
		return fmt.Errorf("%w: %s", ErrLinked, name)
	}
	return nil
}

// KeepAttrs gives the File at name the permissions, and where it may the owner and group, of
// the File whose info is src, so a copy that replaces a File keeps them. It is best effort: a
// share may allow neither.
func KeepAttrs(fsys FS, src fs.FileInfo, name string) {
	_ = fsys.Chmod(name, src.Mode().Perm())
	if uid, gid, ok := owner(src); ok {
		_ = fsys.Lchown(name, uid, gid)
	}
}
