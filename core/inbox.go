package core

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/core/storage"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// InboxLibraryName is the name of the library the app keeps for the Inbox folder.
const InboxLibraryName = "Inbox"

// EnsureInboxLibrary makes sure the Inbox folder at path is a library, so the scanner picks up
// new music dropped there. It is safe to call on every start.
//
// The Inbox is only for the admin: it is never a default library for new Listeners, and
// libraryRepository.Put grants every library to every admin. If path is empty or the folder
// doesn't exist, the Inbox is off and nothing changes. If the Inbox library already exists under
// another path, it is moved to path.
func EnsureInboxLibrary(ctx context.Context, ds model.DataStore, path string) error {
	if path == "" {
		log.Debug(ctx, "Inbox is disabled (no Inbox.Path set)")
		return nil
	}
	if err := checkInboxFolder(path); err != nil {
		log.Warn(ctx, "Inbox is disabled: its folder is not available", "path", path, err)
		return nil
	}

	repo := ds.Library(ctx)
	existing, err := repo.GetAll(model.QueryOptions{Filters: squirrel.Eq{"name": InboxLibraryName}})
	if err != nil {
		return fmt.Errorf("looking up the Inbox library: %w", err)
	}
	if len(existing) > 0 {
		inbox := existing[0]
		if inbox.Path == path {
			return nil
		}
		log.Info(ctx, "Moving the Inbox library to a new path", "from", inbox.Path, "to", path)
		inbox.Path = path
		return repo.Put(&inbox, "path")
	}

	inbox := model.Library{Name: InboxLibraryName, Path: path, DefaultNewUsers: false}
	if err := repo.Put(&inbox); err != nil {
		return fmt.Errorf("creating the Inbox library: %w", err)
	}
	log.Info(ctx, "Created the Inbox library", "id", inbox.ID, "path", path)
	return nil
}

func checkInboxFolder(path string) error {
	store, err := storage.For(path)
	if err != nil {
		return err
	}
	fsys, err := store.FS()
	if err != nil {
		return err
	}
	info, err := fs.Stat(fsys, ".")
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("not a directory")
	}
	return nil
}
