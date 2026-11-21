package rip

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// caseInsensitiveFS is the real disk under root, but resolving each path component
// case-insensitively, as the NAS does over SMB: "the beatles" opens "The Beatles", and making
// "the beatles" when "The Beatles" exists fails as it exists already.
type caseInsensitiveFS struct {
	osFS
	root string
}

func (c caseInsensitiveFS) resolve(name string) string {
	rel, err := filepath.Rel(c.root, name)
	if err != nil || strings.HasPrefix(rel, "..") {
		return name
	}
	cur := c.root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "." {
			continue
		}
		next := filepath.Join(cur, part)
		if entries, err := os.ReadDir(cur); err == nil {
			for _, e := range entries {
				if strings.EqualFold(e.Name(), part) {
					next = filepath.Join(cur, e.Name())
					break
				}
			}
		}
		cur = next
	}
	return cur
}

func (c caseInsensitiveFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return c.osFS.ReadDir(c.resolve(name))
}
func (c caseInsensitiveFS) Mkdir(name string) error { return c.osFS.Mkdir(c.resolve(name)) }
func (c caseInsensitiveFS) Lstat(name string) (fs.FileInfo, error) {
	return c.osFS.Lstat(c.resolve(name))
}
func (c caseInsensitiveFS) Stat(name string) (fs.FileInfo, error) {
	return c.osFS.Stat(c.resolve(name))
}
func (c caseInsensitiveFS) Create(name string) (promoteFile, error) {
	return c.osFS.Create(c.resolve(name))
}
func (c caseInsensitiveFS) Open(name string) (fs.File, error) { return c.osFS.Open(c.resolve(name)) }
func (c caseInsensitiveFS) Link(oldname, newname string) error {
	return c.osFS.Link(c.resolve(oldname), c.resolve(newname))
}
func (c caseInsensitiveFS) Rename(oldname, newname string) error {
	return c.osFS.Rename(c.resolve(oldname), c.resolve(newname))
}
func (c caseInsensitiveFS) Remove(name string) error { return c.osFS.Remove(c.resolve(name)) }
func (c caseInsensitiveFS) Chtimes(name string, t time.Time) error {
	return c.osFS.Chtimes(c.resolve(name), t)
}
func (c caseInsensitiveFS) EvalSymlinks(name string) (string, error) {
	return c.osFS.EvalSymlinks(c.resolve(name))
}

// faultyFS fails the first call of an operation whose path contains match, to check that every
// step of a move can fail without losing or half-writing a File.
type faultyFS struct {
	promoteFS
	mu    sync.Mutex
	op    string // "create", "write", "sync", "close", "open", "link", "rename", "remove", "chtimes"
	match string
	err   error
	// corrupt, if set, changes the bytes written to the copy, so the check after writing fails.
	corrupt bool
	hit     bool
}

func (f *faultyFS) fail(op, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.hit || op != f.op || !strings.Contains(name, f.match) {
		return nil
	}
	f.hit = true
	return f.err
}

func (f *faultyFS) Create(name string) (promoteFile, error) {
	if err := f.fail("create", name); err != nil {
		return nil, err
	}
	file, err := f.promoteFS.Create(name)
	if err != nil {
		return nil, err
	}
	return &faultyFile{promoteFile: file, fs: f, name: name}, nil
}
func (f *faultyFS) Open(name string) (fs.File, error) {
	if err := f.fail("open", name); err != nil {
		return nil, err
	}
	return f.promoteFS.Open(name)
}
func (f *faultyFS) Link(oldname, newname string) error {
	if err := f.fail("link", newname); err != nil {
		return err
	}
	return f.promoteFS.Link(oldname, newname)
}
func (f *faultyFS) Rename(oldname, newname string) error {
	if err := f.fail("rename", newname); err != nil {
		return err
	}
	return f.promoteFS.Rename(oldname, newname)
}
func (f *faultyFS) Remove(name string) error {
	if err := f.fail("remove", name); err != nil {
		return err
	}
	return f.promoteFS.Remove(name)
}

type faultyFile struct {
	promoteFile
	fs   *faultyFS
	name string
}

func (w *faultyFile) Write(p []byte) (int, error) {
	if err := w.fs.fail("write", w.name); err != nil {
		return 0, err
	}
	if w.fs.corrupt && len(p) > 0 {
		q := append([]byte(nil), p...)
		q[0] ^= 0xff
		return w.promoteFile.Write(q)
	}
	return w.promoteFile.Write(p)
}
func (w *faultyFile) Sync() error {
	if err := w.fs.fail("sync", w.name); err != nil {
		return err
	}
	return w.promoteFile.Sync()
}
func (w *faultyFile) Close() error {
	err := w.promoteFile.Close()
	if ferr := w.fs.fail("close", w.name); ferr != nil {
		return ferr
	}
	return err
}

var errInjected = errors.New("injected failure")
