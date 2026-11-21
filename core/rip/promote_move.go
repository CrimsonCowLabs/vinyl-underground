package rip

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
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
func copyToTemp(fsys promoteFS, src, temp string) (sum []byte, size int64, info fs.FileInfo, err error) {
	in, err := fsys.Open(src)
	if err != nil {
		return nil, 0, nil, err
	}
	defer in.Close()
	info, err = in.Stat()
	if err != nil {
		return nil, 0, nil, err
	}
	out, err := fsys.Create(temp)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("making the copy: %w", err)
	}
	h := sha256.New()
	n, err := io.Copy(out, io.TeeReader(in, h))
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, 0, nil, fmt.Errorf("copying the File: %w", err)
	}
	if n != info.Size() {
		return nil, 0, nil, fmt.Errorf("copying the File: read %d of %d bytes", n, info.Size())
	}
	return h.Sum(nil), n, info, nil
}

// checkCopy reads the copy back and checks it is the source's size and SHA-256.
func checkCopy(fsys promoteFS, temp string, sum []byte, size int64) error {
	f, err := fsys.Open(temp)
	if err != nil {
		return fmt.Errorf("reading the copy back: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return fmt.Errorf("reading the copy back: %w", err)
	}
	if n != size || !bytes.Equal(h.Sum(nil), sum) {
		return fmt.Errorf("the copy doesn't match the File (%d of %d bytes)", n, size)
	}
	return nil
}
