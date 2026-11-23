//go:build linux

package safefile

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// RenameNoReplace renames oldname to newname, failing (fs.ErrExist) rather than replacing a File
// at newname: renameat2's RENAME_NOREPLACE, which the kernel checks against the server's own view
// of newname (CIFS takes it), so a File that appears there after Move checked it free isn't
// replaced, as a plain rename over SMB may. Where the filesystem doesn't take the flag, it falls
// back to a plain rename, which relies on the caller having checked newname free just before.
func (OS) RenameNoReplace(oldname, newname string) error {
	err := unix.Renameat2(unix.AT_FDCWD, oldname, unix.AT_FDCWD, newname, unix.RENAME_NOREPLACE)
	if errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.ENOTSUP) {
		return os.Rename(oldname, newname)
	}
	if err != nil {
		return &os.LinkError{Op: "rename", Old: oldname, New: newname, Err: err}
	}
	return nil
}
