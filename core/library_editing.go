package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"

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

// checkFolderWritable makes and removes a hidden File in path. A library that isn't a local
// folder (scheme://...) can't be edited.
func checkFolderWritable(path string) error {
	if strings.Contains(path, "://") {
		return errors.New("not a local folder")
	}
	f, err := os.CreateTemp(path, ".vu-write-check-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(name)
}
