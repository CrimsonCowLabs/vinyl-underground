package rip

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// promoteFS is every filesystem call Promote makes, so tests can make any of them fail, or
// behave like the case-insensitive NAS. osFS is the real one.
type promoteFS interface {
	ReadDir(name string) ([]fs.DirEntry, error)
	Mkdir(name string) error
	Lstat(name string) (fs.FileInfo, error)
	Stat(name string) (fs.FileInfo, error)
	// Create makes a new File, failing if one is there already.
	Create(name string) (promoteFile, error)
	Open(name string) (fs.File, error)
	// Link makes newname another name for oldname, failing if newname exists.
	Link(oldname, newname string) error
	Rename(oldname, newname string) error
	Remove(name string) error
	Chtimes(name string, mtime time.Time) error
	EvalSymlinks(name string) (string, error)
}

// promoteFile is a File being written.
type promoteFile interface {
	io.Writer
	Sync() error
	Close() error
}

type osFS struct{}

func (osFS) ReadDir(name string) ([]fs.DirEntry, error) { return os.ReadDir(name) }
func (osFS) Mkdir(name string) error                    { return os.Mkdir(name, 0o755) }
func (osFS) Lstat(name string) (fs.FileInfo, error)     { return os.Lstat(name) }
func (osFS) Stat(name string) (fs.FileInfo, error)      { return os.Stat(name) }
func (osFS) Create(name string) (promoteFile, error) {
	return os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
}
func (osFS) Open(name string) (fs.File, error)        { return os.Open(name) }
func (osFS) Link(oldname, newname string) error       { return os.Link(oldname, newname) }
func (osFS) Rename(oldname, newname string) error     { return os.Rename(oldname, newname) }
func (osFS) Remove(name string) error                 { return os.Remove(name) }
func (osFS) Chtimes(name string, t time.Time) error   { return os.Chtimes(name, t, t) }
func (osFS) EvalSymlinks(name string) (string, error) { return filepath.EvalSymlinks(name) }
