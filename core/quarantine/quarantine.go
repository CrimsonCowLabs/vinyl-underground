// Package quarantine is where Quarantine is on the drive (#168, ADR-0011): the folder of each
// library holding the Files removed from the Library, which the scanner skips (its .ndignore),
// and what is in it. The moves themselves are the Library's one move (tagedit.Editor.MoveInJob,
// safefile.Move), journaled as EditQuarantine (tagedit's quarantine.go), queued as edit jobs
// (editjob's quarantine.go).
//
// The NAS is case-insensitive (SMB1): two names differing only in letter case reach the same
// folder, so Quarantine's folders are reused in any letter case (safefile.EnsureDir).
package quarantine

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/core/safefile"
	"github.com/navidrome/navidrome/log"
)

// DefaultFolder is Quarantine's folder unless conf.Server.QuarantineFolder says otherwise: the
// top folder of a library the library-cleanup recipes move Files into.
const DefaultFolder = "_Quarantine"

// ignoreFile keeps the scanner out of Quarantine.
const ignoreFile = ".ndignore"

// Folder is Quarantine's folder, relative to each library's folder, slash-separated: the
// QuarantineFolder setting, or DefaultFolder when it is empty or would leave the library.
func Folder() string {
	set := strings.Trim(filepath.ToSlash(conf.Server.QuarantineFolder), "/")
	if set == "" {
		return DefaultFolder
	}
	clean := path.Clean(set)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		log.Warn("QuarantineFolder must be a folder inside the library; using the default", "QuarantineFolder", conf.Server.QuarantineFolder, "default", DefaultFolder)
		return DefaultFolder
	}
	return clean
}

// Within reports whether the path rel (relative to its library) is Quarantine's folder or in
// it, in any letter case.
func Within(rel string) bool {
	q := Folder()
	return strings.EqualFold(rel, q) || (len(rel) > len(q) && strings.EqualFold(rel[:len(q)+1], q+"/"))
}

// Into is where the File at rel goes in Quarantine: the same relative path beneath it (the move
// reuses folders there in any letter case).
func Into(rel string) string { return Folder() + "/" + rel }

// Original is where the File at rel in Quarantine came from, taking its path beneath Quarantine
// as the path it had (as Into does): for a File the Edit journal has no move of.
func Original(rel string) string {
	if !Within(rel) || len(rel) <= len(Folder())+1 {
		return rel
	}
	return rel[len(Folder())+1:]
}

// lookup finds the entry of the folder dir named name in any letter case: its own name. A folder
// not there yet has no entries.
func lookup(fsys safefile.FS, dir, name string) (string, error) {
	entries, err := fsys.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	// The exact name first: a case-sensitive share may hold both.
	if i := slices.IndexFunc(entries, func(e fs.DirEntry) bool { return e.Name() == name }); i >= 0 {
		return entries[i].Name(), nil
	}
	for _, e := range entries {
		if strings.EqualFold(e.Name(), name) {
			return e.Name(), nil
		}
	}
	return "", nil
}

// Ensure makes the library at root's Quarantine folder, reusing one in another letter case
// (safefile.EnsureDir), with the .ndignore that keeps the scanner out of it. It returns the
// folder, relative to root, as it is on disk.
func Ensure(fsys safefile.FS, root string) (string, error) {
	dir, err := safefile.EnsureDir(fsys, root, Folder())
	if err != nil {
		return "", err
	}
	abs := filepath.Join(root, filepath.FromSlash(dir))
	found, err := lookup(fsys, abs, ignoreFile)
	if err != nil || found != "" {
		return dir, err
	}
	f, err := fsys.Create(filepath.Join(abs, ignoreFile))
	if err != nil && !errors.Is(err, fs.ErrExist) {
		return "", fmt.Errorf("keeping the scanner out of Quarantine: %w", err)
	}
	if err == nil {
		if err := f.Close(); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// reuse is rel with each folder named as it is there, in any letter case.
func reuse(fsys safefile.FS, root, rel string) (string, error) {
	dir := ""
	for _, name := range strings.Split(rel, "/") {
		found, err := lookup(fsys, filepath.Join(root, filepath.FromSlash(dir)), name)
		if err != nil {
			return "", err
		}
		if found != "" {
			name = found
		}
		dir = path.Join(dir, name)
	}
	return dir, nil
}

// File is a File in Quarantine: its path, relative to its library, and its size in bytes.
type File struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// IsIgnoreFile reports whether a File named name is a .ndignore, which keeps the scanner out of
// its folder: one is never listed in Quarantine, restored or deleted, as moving one into the
// Library would hide a folder of it, and removing Quarantine's own would have it scanned.
func IsIgnoreFile(name string) bool { return strings.EqualFold(path.Base(name), ignoreFile) }

// Files lists every File in the library at root's Quarantine, in path order, leaving out every
// .ndignore (see IsIgnoreFile). With no Quarantine folder, there are none.
func Files(fsys safefile.FS, root string) ([]File, error) {
	dir, err := reuse(fsys, root, Folder())
	if err != nil {
		return nil, err
	}
	var res []File
	var walk func(rel string) error
	walk = func(rel string) error {
		entries, err := fsys.ReadDir(filepath.Join(root, filepath.FromSlash(rel)))
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, e := range entries {
			p := rel + "/" + e.Name()
			switch {
			case e.IsDir():
				if err := walk(p); err != nil {
					return err
				}
			case IsIgnoreFile(e.Name()):
			case e.Type().IsRegular():
				info, err := e.Info()
				if err != nil {
					return err
				}
				res = append(res, File{Path: p, Size: info.Size()})
			}
		}
		return nil
	}
	if err := walk(dir); err != nil {
		return nil, err
	}
	slices.SortFunc(res, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	return res, nil
}
