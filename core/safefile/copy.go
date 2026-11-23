package safefile

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
)

// CopyToTemp copies src into a new File at temp (which must not exist), flushed to disk (fsync),
// and returns the source's SHA-256, size and info. On failure the caller removes temp.
func CopyToTemp(fsys FS, src, temp string) (sum []byte, size int64, info fs.FileInfo, err error) {
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

// CheckCopy reads the copy at temp back and checks it has the given size and SHA-256.
func CheckCopy(fsys FS, temp string, sum []byte, size int64) error {
	got, n, err := Hash(fsys, temp)
	if err != nil {
		return fmt.Errorf("reading the copy back: %w", err)
	}
	if n != size || !bytes.Equal(got, sum) {
		return fmt.Errorf("the copy doesn't match the File (%d of %d bytes)", n, size)
	}
	return nil
}

// Hash returns the SHA-256 and size of the File at name.
func Hash(fsys FS, name string) ([]byte, int64, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return h.Sum(nil), n, err
}
