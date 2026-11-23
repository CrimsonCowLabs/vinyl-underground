//go:build windows

package safefile

import "io/fs"

func links(fs.FileInfo) uint64 { return 1 }

func owner(fs.FileInfo) (uid, gid int, ok bool) { return 0, 0, false }
