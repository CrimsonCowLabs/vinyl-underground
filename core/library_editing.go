package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// LibraryEditing is whether the app may edit the Library's Files (ADR-0011): editing is on when
// a Library folder is writable. It is decided once at startup.
type LibraryEditing struct {
	Enabled   bool              `json:"enabled"`
	Libraries []LibraryWritable `json:"libraries"`
}

// LibraryWritable is whether one library's folder could be written to at startup.
type LibraryWritable struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Writable bool   `json:"writable"`
}

var libraryEditing atomic.Pointer[LibraryEditing]

// SetLibraryEditing records the result of DetectLibraryEditing for this run.
func SetLibraryEditing(e LibraryEditing) {
	libraryEditing.Store(&e)
}

// CurrentLibraryEditing returns what SetLibraryEditing recorded, or editing off if nothing was.
func CurrentLibraryEditing() LibraryEditing {
	if e := libraryEditing.Load(); e != nil {
		return *e
	}
	return LibraryEditing{}
}

// DetectLibraryEditing checks whether each library's folder is writable by making and removing
// a hidden File in it, not by reading the mount options. The Inbox is left out: it is not part
// of the Library.
func DetectLibraryEditing(ctx context.Context, ds model.DataStore) (LibraryEditing, error) {
	libs, err := ds.Library(ctx).GetAll()
	if err != nil {
		return LibraryEditing{}, fmt.Errorf("loading libraries: %w", err)
	}
	inbox, err := findInboxLibrary(ds.Property(ctx), libs)
	if err != nil {
		return LibraryEditing{}, err
	}
	var res LibraryEditing
	for _, l := range libs {
		if inbox != nil && l.ID == inbox.ID {
			continue // not part of the Library; it is always writable
		}
		err := checkFolderWritable(l.Path)
		if err != nil {
			log.Warn(ctx, "Library is read-only: editing its Files is off", "library", l.Name, "path", l.Path, err)
		}
		w := err == nil
		res.Libraries = append(res.Libraries, LibraryWritable{ID: l.ID, Name: l.Name, Writable: w})
		res.Enabled = res.Enabled || w
	}
	return res, nil
}

// libraryCheckFile is the File checkFolderWritable makes and removes. Its name is fixed, so a
// check File a crash left behind is replaced rather than joined by another.
const libraryCheckFile = ".vu-library-check"

// checkFolderWritable makes and removes a hidden File in path, then puts the folder's
// modification time back, so the next scan doesn't see the folder as changed. A library that
// isn't a local folder (scheme://...) can't be edited.
func checkFolderWritable(path string) error {
	if strings.Contains(path, "://") {
		return errors.New("not a local folder")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	probe := filepath.Join(path, libraryCheckFile)
	f, err := os.OpenFile(probe, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_ = f.Close()
	if err := os.Remove(probe); err != nil {
		return fmt.Errorf("could make a File but not remove it: %w", err)
	}
	_ = os.Chtimes(path, time.Time{}, info.ModTime())
	return nil
}
