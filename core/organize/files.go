package organize

import (
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/navidrome/navidrome/core/libpath"
)

// FolderMove is an album's own folder, all of whose Tracks the plan moves into one new album
// folder: its other Files (cover.jpg, a booklet, a cue sheet, hidden Files too) go with them, to
// the same place under NewPath, so Path is left empty and removed. Dirs are the folders under
// Path (or Path itself) whose Files go elsewhere: to the folder their Tracks went to, e.g. a
// disc folder's to its album's Disc folder; a folder under one of those goes to the same place
// under it. An album's own folder is the one its Tracks are in, or the one above its disc
// folders. A folder holding any other Track (another album's, one not selected, or one that
// can't be placed) keeps its other Files.
//
// The plan lists the images the scan found there (folder.image_files) as moves of their own; it
// doesn't read the folder from disk (over SMB1, a listing of each album's folder would make a
// Library-wide plan take minutes). The job moves every other File left in Path once its Tracks
// have moved (see jobKind.Done), each journaled with the job, unless one of the album's Tracks
// didn't move. Keep are the Files the plan flagged (ReasonSamePath, ReasonPathTaken), which stay.
type FolderMove struct {
	LibraryID int    `json:"libraryId"`
	Path      string `json:"path"`
	NewPath   string `json:"newPath"`
	// NewLibraryID is the library NewPath is in, for a Promote (Scope.Into); 0 means LibraryID.
	NewLibraryID int               `json:"newLibraryId,omitempty"`
	Keep         []string          `json:"keep,omitempty"`
	Dirs         map[string]string `json:"dirs,omitempty"`
}

// Target is where the Files of the folder dir (Path, or one under it) go.
func (f FolderMove) Target(dir string) string {
	if t, ok := f.Dirs[dir]; ok {
		return t
	}
	if dir == f.Path || !strings.HasPrefix(dir, f.Path+"/") {
		return f.NewPath
	}
	return path.Join(f.Target(dirOf(dir)), path.Base(dir))
}

// otherFiles works out the album folders the plan moves whole (see FolderMove), and the images
// in them, as rows of their own.
func (lp *libraryPlan) otherFiles(places []placement, keep []string) ([]FolderMove, []Row) {
	// The folders the Tracks leave, by the album folder they go to.
	sources := map[string][]string{}
	dest := map[string]string{}   // a moving Track's new album folder, by its ID
	folder := map[string]string{} // and the folder it goes to in there (a Disc folder, or the same)
	for _, p := range places {
		if p.status != StatusMove {
			continue
		}
		to := p.albumDir
		dest[p.mf.ID], folder[p.mf.ID] = to, dirOf(p.newPath)
		from := dirOf(p.mf.Path)
		if !slices.Contains(sources[to], from) {
			sources[to] = append(sources[to], from)
		}
	}
	var folders []FolderMove
	var rows []Row
	for _, to := range slices.Sorted(maps.Keys(sources)) {
		for _, root := range lp.albumRoots(sources[to], to, dest) {
			f := lp.folderMove(root, to, folder)
			folders = append(folders, f)
			rows = append(rows, lp.imagesUnder(f)...)
		}
	}
	rows = lp.flagFileCollisions(rows, places)
	moving := map[string]bool{}
	for _, p := range places {
		if p.status == StatusMove {
			moving[p.mf.Path] = true
		}
	}
	for i := range folders {
		f := &folders[i]
		for _, r := range rows {
			if r.Status != StatusFile && strings.HasPrefix(r.Path, f.Path+"/") {
				f.Keep = append(f.Keep, r.Path)
			}
		}
		for _, k := range keep {
			if strings.HasPrefix(k, f.Path+"/") && !moving[k] {
				f.Keep = append(f.Keep, k)
			}
		}
	}
	return folders, rows
}

// albumRoots are the folders whose other Files move to the folder to: the common folder of the
// Tracks' folders (the one above their disc folders), or else each of them on its own, as long as
// every Track under it moves to to (see ownedBy).
func (lp *libraryPlan) albumRoots(from []string, to string, dest map[string]string) []string {
	if common := commonDir(from); len(from) > 1 && lp.ownedBy(common, to, dest) {
		return []string{common}
	}
	var res []string
	for _, f := range from {
		if lp.ownedBy(f, to, dest) {
			res = append(res, f)
		}
	}
	return res
}

// ownedBy reports whether every Track under the folder dir moves to the folder to, and the two
// folders are apart (neither inside the other), so dir is left empty once its other Files move.
func (lp *libraryPlan) ownedBy(dir, to string, dest map[string]string) bool {
	// Folders in two libraries are apart.
	if dir == "" || (!lp.cross() && (inside(dir, to) || inside(to, dir))) || lp.hasTwin(dir) {
		return false
	}
	found := false
	for _, d := range lp.subtree(dir) {
		for _, mf := range lp.tracksIn[d] {
			if dest[mf.ID] != to {
				return false
			}
			found = true
		}
	}
	return found
}

// subtree is the folder dir and every folder under it, as the database has them.
func (lp *libraryPlan) subtree(dir string) []string {
	res := []string{dir}
	for i := 0; i < len(res); i++ {
		for _, c := range lp.children[res[i]] {
			res = append(res, path.Join(res[i], c))
		}
	}
	return res
}

// folderMove is the album folder root's move into the album folder to: a folder whose Tracks all
// go to one folder (folder, by Track ID) has its other Files go there too, a folder with none (or
// whose Tracks are split between Disc folders) to the same place under its parent's.
func (lp *libraryPlan) folderMove(root, to string, folder map[string]string) FolderMove {
	f := FolderMove{LibraryID: lp.lib.ID, Path: root, NewPath: to, NewLibraryID: lp.newLibraryID()}
	for _, dir := range lp.subtree(root) {
		went := ""
		for _, mf := range lp.tracksIn[dir] {
			switch d := folder[mf.ID]; {
			case went == "":
				went = d
			case d != went:
				went = "-"
			}
		}
		if went == "" || went == "-" || went == f.Target(dir) {
			continue
		}
		if dir == root {
			f.NewPath = went
			continue
		}
		if f.Dirs == nil {
			f.Dirs = map[string]string{}
		}
		f.Dirs[dir] = went
	}
	return f
}

// imagesUnder plans each image the scan found under the album folder f.Path to where its folder's
// Files go (FolderMove.Target).
func (lp *libraryPlan) imagesUnder(f FolderMove) []Row {
	var rows []Row
	for _, dir := range lp.subtree(f.Path) {
		for _, name := range slices.Sorted(slices.Values(lp.images[dir])) {
			rows = append(rows, Row{LibraryID: lp.lib.ID, Path: path.Join(dir, name), NewLibraryID: lp.newLibraryID(),
				NewPath: path.Join(f.Target(dir), name), Status: StatusFile})
		}
	}
	return rows
}

// flagFileCollisions leaves where they are the other Files planned for one path, or for a Track's
// new path, or for a path taken already.
func (lp *libraryPlan) flagFileCollisions(rows []Row, places []placement) []Row {
	count := map[string]int{}
	for _, r := range rows {
		count[libpath.Fold(r.NewPath)]++
	}
	for _, p := range places {
		if p.newPath != "" {
			count[libpath.Fold(p.newPath)]++
		}
	}
	for i := range rows {
		r := &rows[i]
		key := libpath.Fold(r.NewPath)
		switch {
		case count[key] > 1:
			r.Status = ReasonSamePath
		case lp.taken(r.NewPath):
			r.Status = ReasonPathTaken
		}
	}
	return rows
}

// commonDir is the folder the folders dirs are all in (or are), "" for the library's root.
func commonDir(dirs []string) string {
	if len(dirs) == 0 {
		return ""
	}
	common := strings.Split(dirs[0], "/")
	for _, d := range dirs[1:] {
		parts := strings.Split(d, "/")
		n := 0
		for n < len(common) && n < len(parts) && common[n] == parts[n] {
			n++
		}
		common = common[:n]
	}
	return strings.Join(common, "/")
}

// inside reports whether the folder a is b or inside it.
func inside(a, b string) bool {
	return b == "" || libpath.SameName(a, b) || strings.HasPrefix(libpath.Fold(a), libpath.Fold(b)+"/")
}
