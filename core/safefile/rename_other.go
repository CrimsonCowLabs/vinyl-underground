//go:build !linux

package safefile

import "os"

// RenameNoReplace renames oldname to newname. Here it is a plain rename: the caller has checked
// newname free (see the Linux one).
func (OS) RenameNoReplace(oldname, newname string) error { return os.Rename(oldname, newname) }
