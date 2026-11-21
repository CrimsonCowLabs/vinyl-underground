package rip

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/model"
)

// promoteProbe is the File CheckPromoteFolder makes and removes to check it can write.
const promoteProbe = ".vu-promote-check"

// CheckPromoteFolder checks the promote folder (Inbox.PromotePath, e.g. /music/Vinyl Underground)
// at startup, and returns it cleaned, or "" when it is not set and Promote is off. The folder
// must exist, be strictly inside a library that isn't the Inbox (so promoted Files are scanned
// into it at their final place, ADR-0008), and be writable, which is checked by making and
// removing a hidden File. Any other folder is an error, and Promote stays off.
func CheckPromoteFolder(ctx context.Context, ds model.DataStore, path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("the promote folder must be an absolute path")
	}
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%q is not a folder", path)
	}
	if _, _, err := promoteLibrary(ctx, ds, path); err != nil {
		return "", err
	}
	probe := filepath.Join(path, promoteProbe)
	f, err := os.OpenFile(probe, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return "", fmt.Errorf("the promote folder is not writable (is its read-write mount there?): %w", err)
	}
	_ = f.Close()
	if err := os.Remove(probe); err != nil {
		return "", fmt.Errorf("the promote folder is not writable: %w", err)
	}
	return path, nil
}

// promoteLibrary returns the library the promote folder root is in (never the Inbox), and root's
// path relative to that library's folder.
func promoteLibrary(ctx context.Context, ds model.DataStore, root string) (*model.Library, string, error) {
	libs, err := ds.Library(ctx).GetAll()
	if err != nil {
		return nil, "", fmt.Errorf("loading libraries: %w", err)
	}
	inbox, err := core.FindInboxLibrary(ctx, ds)
	if err != nil {
		return nil, "", err
	}
	for i := range libs {
		lib := &libs[i]
		if inbox != nil && lib.ID == inbox.ID {
			continue
		}
		if strings.Contains(lib.Path, "://") {
			continue // not a local folder
		}
		base := filepath.Clean(lib.Path)
		if strings.HasPrefix(root, base+string(filepath.Separator)) {
			return lib, filepath.ToSlash(strings.TrimPrefix(root, base+string(filepath.Separator))), nil
		}
	}
	return nil, "", fmt.Errorf("the promote folder %q must be inside a library's folder (not the Inbox, and not the library's folder itself)", root)
}
