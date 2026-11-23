//go:build windows

package safefile

import (
	"errors"
	"syscall"
)

// errorNotSameDevice is ERROR_NOT_SAME_DEVICE: a rename (MoveFileEx) across volumes.
const errorNotSameDevice = syscall.Errno(17)

// isCrossDevice reports whether a rename failed because its two paths are on two volumes.
func isCrossDevice(err error) bool {
	return errors.Is(err, syscall.EXDEV) || errors.Is(err, errorNotSameDevice)
}
