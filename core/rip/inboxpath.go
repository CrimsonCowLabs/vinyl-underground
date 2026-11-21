package rip

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ErrOutsideInbox is returned for a path that doesn't stay inside the Inbox: an absolute path, one
// with "..", one that goes through a symlink, or a File that is a symlink itself.
var ErrOutsideInbox = errors.New("the path is not inside the Inbox")

// inboxPath turns rel, a path relative to the Inbox folder at root (as in RipItem.Path or
// RipJob.Folder), into an absolute path that is surely inside the Inbox, so review and Discard
// never touch anything else. The File itself may not exist, but its folder must.
func inboxPath(root, rel string) (string, error) {
	rel = filepath.FromSlash(rel)
	if rel == "" || filepath.IsAbs(rel) || strings.HasPrefix(rel, `\`) || filepath.VolumeName(rel) != "" {
		return "", fmt.Errorf("%w: %q", ErrOutsideInbox, rel)
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == ".." {
			return "", fmt.Errorf("%w: %q", ErrOutsideInbox, rel)
		}
	}
	rel = filepath.Clean(rel)
	if rel == "." {
		return "", fmt.Errorf("%w: %q", ErrOutsideInbox, rel)
	}

	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("finding the Inbox: %w", err)
	}
	// The folder may go through symlinks only if it ends up inside the Inbox all the same.
	dir, err := filepath.EvalSymlinks(filepath.Join(realRoot, filepath.Dir(rel)))
	if err != nil {
		return "", err
	}
	if dir != realRoot && !strings.HasPrefix(dir, realRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q", ErrOutsideInbox, rel)
	}
	full := filepath.Join(dir, filepath.Base(rel))
	if info, err := os.Lstat(full); err == nil && info.Mode()&fs.ModeSymlink != 0 {
		// Never follow a link: a temp-and-rename would replace the link, and a delete would
		// delete the wrong thing.
		return "", fmt.Errorf("%w: %q is a symlink", ErrOutsideInbox, rel)
	}
	return full, nil
}
