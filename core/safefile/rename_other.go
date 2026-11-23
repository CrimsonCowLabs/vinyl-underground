//go:build !linux

package safefile

import (
	"errors"
	"os"
)

// RenameNoReplace renames oldname to newname. Here it is a plain rename: the caller has checked
// newname free (see the Linux one).
func (OS) RenameNoReplace(oldname, newname string) error { return os.Rename(oldname, newname) }

// RenameExclusive is errors.ErrUnsupported here: only Linux's renameat2 refuses a File at the new
// name by itself (see the Linux one).
func (OS) RenameExclusive(string, string) error { return errors.ErrUnsupported }
