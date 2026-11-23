// Package quarantine is where Quarantine is on the drive (#168, ADR-0011): the folder of each
// library holding the Files removed from the Library, which the scanner skips (its .ndignore),
// and how a File's path beneath it is planned. The moves themselves are journaled writes
// (tagedit's quarantine.go), queued as edit jobs (editjob's quarantine.go).
//
// The NAS is case-insensitive (SMB1): two names differing only in letter case reach the same
// folder, so a path in Quarantine reuses the folders already there in any letter case, and a
// File already at a target, in any letter case, is never overwritten.
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

// ErrTaken is a target where something is already, in some letter case: the File isn't moved,
// so nothing is overwritten.
var ErrTaken = errors.New("a File is already there (in some letter case); nothing was moved")

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

// Into is where the File at rel goes in Quarantine, before letter case is reused (see Target):
// the same relative path beneath it.
func Into(rel string) string { return Folder() + "/" + rel }

// Original is where the File at rel in Quarantine came from, taking its path beneath Quarantine
// as the path it had (as Into does): for a File the Edit journal has no move of.
func Original(rel string) string {
	if !Within(rel) || len(rel) <= len(Folder())+1 {
		return rel
	}
	return rel[len(Folder())+1:]
}

// Target plans the File's move to rel (relative to the library at root): each folder of rel
// already there under a name differing only in letter case is reused, under its own name, and a
// File (or folder) already at rel's name, in any letter case, is ErrTaken. It changes nothing.
func Target(fsys safefile.FS, root, rel string) (string, error) {
	parts := strings.Split(rel, "/")
	dir := ""
	for i, name := range parts {
		last := i == len(parts)-1
		found, isDir, err := lookup(fsys, filepath.Join(root, filepath.FromSlash(dir)), name)
		if err != nil {
			return "", err
		}
		switch {
		case found != "" && (last || !isDir):
			return "", fmt.Errorf("%w: %s", ErrTaken, path.Join(dir, found))
		case found != "":
			name = found
		}
		dir = path.Join(dir, name)
	}
	return dir, nil
}

// lookup finds the entry of the folder dir named name in any letter case: its own name, and
// whether it is a folder. A folder not there yet has no entries.
func lookup(fsys safefile.FS, dir, name string) (string, bool, error) {
	entries, err := fsys.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	// The exact name first: a case-sensitive share may hold both.
	if i := slices.IndexFunc(entries, func(e fs.DirEntry) bool { return e.Name() == name }); i >= 0 {
		return entries[i].Name(), entries[i].IsDir(), nil
	}
	for _, e := range entries {
		if strings.EqualFold(e.Name(), name) {
			return e.Name(), e.IsDir(), nil
		}
	}
	return "", false, nil
}

// MkdirAll makes the folders of rel (relative to root) that aren't there. Plan rel with Target
// first, so it names the folders already there as they are.
func MkdirAll(fsys safefile.FS, root, rel string) error {
	dir := root
	for _, name := range strings.Split(rel, "/") {
		dir = filepath.Join(dir, name)
		if safefile.Exists(fsys, dir) {
			continue
		}
		if err := fsys.Mkdir(dir); err != nil && !errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("making the folder %s: %w", dir, err)
		}
	}
	return nil
}

// Present reports whether a listing of the folder of rel (relative to root) shows rel's name,
// exactly: on a case-insensitive share a Stat finds a File in any letter case, a listing doesn't.
func Present(fsys safefile.FS, root, rel string) bool {
	entries, err := fsys.ReadDir(filepath.Join(root, filepath.FromSlash(path.Dir(rel))))
	if err != nil {
		return false
	}
	name := path.Base(rel)
	return slices.ContainsFunc(entries, func(e fs.DirEntry) bool { return e.Name() == name })
}

// Ensure makes the library at root's Quarantine folder, reusing one in another letter case, with
// the .ndignore that keeps the scanner out of it. It returns the folder, relative to root.
func Ensure(fsys safefile.FS, root string) (string, error) {
	dir, err := folderAt(fsys, root)
	if err != nil {
		return "", err
	}
	if err := MkdirAll(fsys, root, dir); err != nil {
		return "", err
	}
	ignore, err := Target(fsys, root, dir+"/"+ignoreFile)
	if errors.Is(err, ErrTaken) {
		return dir, nil
	}
	if err != nil {
		return "", err
	}
	f, err := fsys.Create(filepath.Join(root, filepath.FromSlash(ignore)))
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

// folderAt is Quarantine's folder in the library at root, under the name it has there if it is
// there in another letter case.
func folderAt(fsys safefile.FS, root string) (string, error) {
	dir, err := Target(fsys, root, Folder()+"/"+ignoreFile)
	if errors.Is(err, ErrTaken) {
		// The .ndignore is there: the folders before it are as they are.
		return reuse(fsys, root, Folder())
	}
	if err != nil {
		return "", err
	}
	return path.Dir(dir), nil
}

// reuse is rel with each folder named as it is there, in any letter case.
func reuse(fsys safefile.FS, root, rel string) (string, error) {
	dir := ""
	for _, name := range strings.Split(rel, "/") {
		found, _, err := lookup(fsys, filepath.Join(root, filepath.FromSlash(dir)), name)
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

// Files lists every File in the library at root's Quarantine, in path order, leaving out the
// .ndignore that keeps the scanner out. With no Quarantine folder, there are none.
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
			case rel == dir && e.Name() == ignoreFile:
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
