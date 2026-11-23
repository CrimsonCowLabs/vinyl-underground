package safefile

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

var (
	// ErrInterrupted is a Replace that stopped with the File moved aside to its backup (or that
	// found an earlier one's backup still there). The File isn't lost: Settle finishes or undoes
	// it.
	ErrInterrupted = errors.New("the File was left moved aside; it is put back when the edit is settled")
	// ErrBackupLeft is a Replace that put the copy in place, but couldn't remove the original's
	// backup. The edit is done; Settle removes the backup later.
	ErrBackupLeft = errors.New("the File was replaced, but its old copy could not be removed")
)

// hidden is the name of a hidden File beside path, keeping path's extension (TagLib picks a
// File's format by it): ".<name>.<tag><ext>". The scanner skips hidden Files, and the name is
// fixed by path, so an interrupted write's Files are found again.
func hidden(path, tag string) string {
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(filepath.Base(path), ext)
	return filepath.Join(filepath.Dir(path), "."+base+"."+tag+ext)
}

// TempName is the hidden copy of the File at path that an edit writes to and checks, before it
// replaces the File.
func TempName(path string) string { return hidden(path, "vu-edit") }

// BackupName is where Replace moves the File at path aside while its copy takes its place.
func BackupName(path string) string { return hidden(path, "vu-original") }

// Replace puts the checked copy at temp in the place of the File at path. A rename over an
// existing File isn't atomic on the NAS (SMB1, where CIFS unlinks the target first), or may not
// be allowed at all, so it never relies on one: the File is moved aside to BackupName(path), the
// copy is renamed into place, and only then is the backup removed. At every step either the File
// or the copy is at path, or the File is at its backup:
//
//   - the File can't be moved aside: nothing changed (the caller removes temp);
//   - the copy can't take its place: the File is moved back (the caller removes temp), or, if
//     even that fails, ErrInterrupted, and Settle moves it back later;
//   - the backup can't be removed: ErrBackupLeft, and Settle removes it later.
func Replace(fsys FS, path, temp string) error {
	backup := BackupName(path)
	if Exists(fsys, backup) {
		// An earlier edit's, not settled yet: it may be the only copy of the File.
		return fmt.Errorf("%w: %s is still there", ErrInterrupted, filepath.Base(backup))
	}
	if err := fsys.Rename(path, backup); err != nil {
		return fmt.Errorf("moving the File aside: %w", err)
	}
	if err := fsys.Rename(temp, path); err != nil {
		if rerr := fsys.Rename(backup, path); rerr != nil {
			return fmt.Errorf("%w (%v, then %v)", ErrInterrupted, err, rerr)
		}
		return fmt.Errorf("moving the copy into place: %w", err)
	}
	if err := fsys.Remove(backup); err != nil {
		return fmt.Errorf("%w: %v", ErrBackupLeft, err)
	}
	return nil
}

// Settle finishes or undoes a Replace (or an edit about to make one) that the app stopped in the
// middle of, from what is beside path. sum is the SHA-256 of the checked copy the edit was
// putting in place. It reports whether the copy is now at path (the edit was written) or the
// original is (it was undone). It never removes the only copy of a File: when it can't tell
// what is left, it leaves everything and fails.
func Settle(fsys FS, path string, sum []byte) (written bool, err error) {
	temp, backup := TempName(path), BackupName(path)
	isCopy := func(name string) bool {
		got, _, err := Hash(fsys, name)
		return err == nil && bytes.Equal(got, sum)
	}
	switch {
	case Exists(fsys, path) && Exists(fsys, backup):
		// The copy took the File's place; only the backup is left to remove. Unless what is at
		// path isn't the copy: then the backup may be the File's only copy.
		if !isCopy(path) {
			return false, fmt.Errorf("both %s and its old copy %s are there, and neither is the edit's copy: leaving both",
				filepath.Base(path), filepath.Base(backup))
		}
		if err := fsys.Remove(backup); err != nil {
			return false, fmt.Errorf("removing the old copy of the File: %w", err)
		}
	case Exists(fsys, backup):
		// The File is moved aside: put it back.
		if err := fsys.Rename(backup, path); err != nil {
			return false, fmt.Errorf("putting the File back: %w", err)
		}
	case Exists(fsys, path):
		// Never moved aside, or the replace finished.
	case Exists(fsys, temp) && isCopy(temp):
		// Only the checked copy is left.
		if err := fsys.Rename(temp, path); err != nil {
			return false, fmt.Errorf("moving the copy into place: %w", err)
		}
		return true, nil
	default:
		return false, fmt.Errorf("the File is not at %s, and there is no checked copy of it", path)
	}
	if err := fsys.Remove(temp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("removing the edit's copy: %w", err)
	}
	return isCopy(path), nil
}
