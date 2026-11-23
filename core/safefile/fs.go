// Package safefile is how the app writes Files on the NAS without ever losing one: every
// filesystem call goes through FS (so tests can make any of them fail), a File is copied to a
// hidden temp beside its destination and checked before it takes its place, and a File is only
// ever replaced by a sequence that leaves either it or a checked copy in place at every step.
// Promote (core/rip) and tag edits (core/tagedit) share it.
package safefile

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// FS is every filesystem call the app makes when it writes the Library. OS is the real one.
type FS interface {
	ReadDir(name string) ([]fs.DirEntry, error)
	Mkdir(name string) error
	Lstat(name string) (fs.FileInfo, error)
	Stat(name string) (fs.FileInfo, error)
	// Create makes a new File, failing if one is there already.
	Create(name string) (File, error)
	Open(name string) (fs.File, error)
	// Link makes newname another name for oldname, failing if newname exists.
	Link(oldname, newname string) error
	Rename(oldname, newname string) error
	Remove(name string) error
	Chtimes(name string, mtime time.Time) error
	EvalSymlinks(name string) (string, error)
}

// File is a File being written.
type File interface {
	io.Writer
	Sync() error
	Close() error
}

// OS is the real filesystem.
type OS struct{}

func (OS) ReadDir(name string) ([]fs.DirEntry, error) { return os.ReadDir(name) }
func (OS) Mkdir(name string) error                    { return os.Mkdir(name, 0o755) }
func (OS) Lstat(name string) (fs.FileInfo, error)     { return os.Lstat(name) }
func (OS) Stat(name string) (fs.FileInfo, error)      { return os.Stat(name) }
func (OS) Create(name string) (File, error) {
	return os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
}
func (OS) Open(name string) (fs.File, error)        { return os.Open(name) }
func (OS) Link(oldname, newname string) error       { return os.Link(oldname, newname) }
func (OS) Rename(oldname, newname string) error     { return os.Rename(oldname, newname) }
func (OS) Remove(name string) error                 { return os.Remove(name) }
func (OS) Chtimes(name string, t time.Time) error   { return os.Chtimes(name, t, t) }
func (OS) EvalSymlinks(name string) (string, error) { return filepath.EvalSymlinks(name) }

// Exists reports whether there is anything at name.
func Exists(fsys FS, name string) bool {
	_, err := fsys.Lstat(name)
	return err == nil
}
