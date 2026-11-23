//go:build !windows

package safefile

import (
	"io/fs"
	"syscall"
)

// links is how many names the File with info has (hard links).
func links(info fs.FileInfo) uint64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Nlink) //nolint:unconvert // Nlink is uint16 on some platforms
	}
	return 1
}

// owner is the File's owner and group, if the platform tells.
func owner(info fs.FileInfo) (uid, gid int, ok bool) {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return int(st.Uid), int(st.Gid), true
	}
	return 0, 0, false
}
