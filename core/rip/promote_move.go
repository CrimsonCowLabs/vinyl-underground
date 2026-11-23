package rip

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/navidrome/navidrome/core/safefile"
)

// ErrDestTaken is returned when a File appeared at a promoted File's destination after it was
// picked. That File is never replaced.
var ErrDestTaken = errors.New("a File is already there")

// promoteTemp is the hidden name a File is copied under beside dest, until it is checked and
// moved into place. The scanner skips hidden Files, and the name is fixed by dest, so an
// interrupted Promote's temp is found again and removed.
func promoteTemp(dest string) string {
	return filepath.Join(filepath.Dir(dest), "."+filepath.Base(dest)+".vu-promote")
}

// placeCopy copies the File at src to dest, which must not exist, without ever replacing a File
// or leaving a half-written one at dest. The Inbox is on local disk and the NAS is CIFS, so a
// rename can't move the File across (EXDEV): it is copied to a hidden temp beside dest, flushed
// to disk (fsync), read back and checked against the source (size and SHA-256), and only then
// moved into place, by a hard link (which fails if dest exists) or, where SMB has none, by a
// rename after checking dest is still free. The source is left alone; on any failure the temp
// is removed and dest isn't there.
func placeCopy(fsys promoteFS, src, dest string) (err error) {
	temp := promoteTemp(dest)
	if err := fsys.Remove(temp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("removing an earlier try's temp File: %w", err)
	}
	defer func() {
		if err != nil {
			_ = fsys.Remove(temp)
		}
	}()

	sum, size, srcInfo, err := copyToTemp(fsys, src, temp)
	if err != nil {
		return err
	}
	if err := checkCopy(fsys, temp, sum, size); err != nil {
		return err
	}
	// Keep the File's date (best effort: the copy is good either way).
	_ = fsys.Chtimes(temp, srcInfo.ModTime())

	if err := fsys.Link(temp, dest); err == nil {
		_ = fsys.Remove(temp)
		return nil
	} else if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%w: %s", ErrDestTaken, filepath.Base(dest))
	}
	// No hard links here (SMB1): rename, which would replace dest, so check it is still free. On
	// the case-insensitive NAS this also finds a File whose name differs only by letter case.
	if _, err := fsys.Lstat(dest); err == nil {
		return fmt.Errorf("%w: %s", ErrDestTaken, filepath.Base(dest))
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := fsys.Rename(temp, dest); err != nil {
		return fmt.Errorf("moving the copy into place: %w", err)
	}
	return nil
}

// copyToTemp copies src into a new File at temp, flushed to disk, and returns the source's
// SHA-256, size and info.
func copyToTemp(fsys promoteFS, src, temp string) ([]byte, int64, fs.FileInfo, error) {
	return safefile.CopyToTemp(fsys, src, temp)
}

// checkCopy reads the copy back and checks it is the source's size and SHA-256.
func checkCopy(fsys promoteFS, temp string, sum []byte, size int64) error {
	return safefile.CheckCopy(fsys, temp, sum, size)
}
