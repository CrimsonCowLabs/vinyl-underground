package core

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/navidrome/navidrome/core/storage"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// InboxLibraryName is the name the Inbox library is created with. An admin may rename it later.
const InboxLibraryName = "Inbox"

// inboxLibraryIDKey is the property holding the Inbox library's ID, so the Inbox is still found
// after it is renamed or its folder moves, and another library is never mistaken for it.
const inboxLibraryIDKey = "InboxLibraryID"

// EnsureInboxLibrary makes sure the Inbox folder at path is a library, so the scanner picks up
// new music dropped there. It is safe to call on every start, and reports whether it created the
// Inbox library or moved it to a new path, in which case the Inbox needs a scan.
//
// The Inbox is only for admins: it is never a default library for new Listeners (this is reset
// on every start), and libraryRepository.Put grants every library to every admin. If path is
// empty, isn't an existing folder, or overlaps another library, no Inbox library is created or
// moved. An Inbox library that already exists is left in place; an admin can delete it under
// Libraries.
func EnsureInboxLibrary(ctx context.Context, ds model.DataStore, path string) (bool, error) {
	if path == "" {
		log.Debug(ctx, "Inbox is off (no Inbox.Path set)")
		return false, nil
	}
	path, err := checkInboxFolder(path)
	if err != nil {
		log.Warn(ctx, "Inbox is off: its folder is not usable", "path", path, err)
		return false, nil
	}

	repo := ds.Library(ctx)
	libs, err := repo.GetAll()
	if err != nil {
		return false, fmt.Errorf("loading libraries: %w", err)
	}
	inbox, err := findInboxLibrary(ds.Property(ctx), libs)
	if err != nil {
		return false, err
	}
	for _, l := range libs {
		if (inbox == nil || l.ID != inbox.ID) && pathsOverlap(l.Path, path) {
			log.Error(ctx, "Inbox is off: its folder overlaps another library", "path", path, "library", l.Name, "libraryPath", l.Path)
			return false, nil
		}
	}

	if inbox != nil {
		var cols []string
		moved := inbox.Path != path
		if moved {
			log.Info(ctx, "Moving the Inbox library to a new path", "from", inbox.Path, "to", path)
			inbox.Path = path
			cols = append(cols, "path")
		}
		if inbox.DefaultNewUsers {
			log.Warn(ctx, "The Inbox library was a default for new users; turning that off", "library", inbox.Name)
			inbox.DefaultNewUsers = false
			cols = append(cols, "default_new_users")
		}
		if len(cols) > 0 {
			if err := repo.Put(inbox, cols...); err != nil {
				return false, fmt.Errorf("updating the Inbox library: %w", err)
			}
		}
		return moved, nil
	}

	newInbox := model.Library{Name: InboxLibraryName, Path: path, DefaultNewUsers: false}
	if err := repo.Put(&newInbox); err != nil {
		return false, fmt.Errorf("creating the Inbox library: %w", err)
	}
	if err := ds.Property(ctx).Put(inboxLibraryIDKey, strconv.Itoa(newInbox.ID)); err != nil {
		return false, fmt.Errorf("remembering the Inbox library: %w", err)
	}
	log.Info(ctx, "Created the Inbox library", "id", newInbox.ID, "path", path)
	return true, nil
}

// FindInboxLibrary returns the Inbox library, or nil if there is none (it is off, or an admin
// deleted it).
func FindInboxLibrary(ctx context.Context, ds model.DataStore) (*model.Library, error) {
	libs, err := ds.Library(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("loading libraries: %w", err)
	}
	return findInboxLibrary(ds.Property(ctx), libs)
}

// findInboxLibrary returns the library recorded as the Inbox, or nil if there is none (yet, or
// any more, because an admin deleted it).
func findInboxLibrary(props model.PropertyRepository, libs model.Libraries) (*model.Library, error) {
	idStr, err := props.DefaultGet(inboxLibraryIDKey, "")
	if err != nil {
		return nil, fmt.Errorf("looking up the Inbox library: %w", err)
	}
	if idStr == "" {
		return nil, nil
	}
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return nil, nil
	}
	for i := range libs {
		if libs[i].ID == id {
			return &libs[i], nil
		}
	}
	return nil, nil
}

// checkInboxFolder normalizes a local path the way the Libraries page does and checks that the
// folder exists. Paths with a storage scheme (scheme://...) are used as given.
func checkInboxFolder(path string) (string, error) {
	if !strings.Contains(path, "://") {
		if !filepath.IsAbs(path) {
			return path, errors.New("path must be absolute")
		}
		path = filepath.Clean(path)
	}
	store, err := storage.For(path)
	if err != nil {
		return path, err
	}
	fsys, err := store.FS()
	if err != nil {
		return path, err
	}
	info, err := fs.Stat(fsys, ".")
	if err != nil {
		return path, err
	}
	if !info.IsDir() {
		return path, errors.New("not a directory")
	}
	return path, nil
}

// pathsOverlap reports whether one folder is the other or inside it, so the same Files would be
// scanned into both libraries.
func pathsOverlap(a, b string) bool {
	a, b = strings.TrimSuffix(a, "/"), strings.TrimSuffix(b, "/")
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}
