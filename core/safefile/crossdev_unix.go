//go:build !windows

package safefile

import (
	"errors"
	"syscall"
)

// isCrossDevice reports whether a rename failed because its two paths are on two filesystems.
func isCrossDevice(err error) bool { return errors.Is(err, syscall.EXDEV) }
