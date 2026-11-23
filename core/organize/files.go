package organize

import (
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/navidrome/navidrome/core/libpath"
)

// FolderMove is an album's own folder, all of whose Tracks the plan moves to one new folder: its
// other Files (cover.jpg, a booklet, a cue sheet, hidden Files too) go with them, to the same
// place under NewPath, so Path is left empty and removed. An album's own folder is the one its
// Tracks are in, or the one above its disc folders. A folder holding any other Track (another
// album's, one not selected, or one that can't be placed) keeps its other Files.
//
// The plan lists the images the scan found there (folder.image_files) as moves of their own; it
// doesn't read the folder from disk (over SMB1, a listing of each album's folder would make a
// Library-wide plan take minutes). The job moves every other File left in Path once its Tracks
// have moved (see jobKind.Done), each journaled with the job, unless one of the album's Tracks
// didn't move. Keep are the Files the plan flagged (ReasonSamePath, ReasonPathTaken), which stay.
type FolderMove struct {
	LibraryID int      `json:"libraryId"`
	Path      string   `json:"path"`
	NewPath   string   `json:"newPath"`
	Keep      []string `json:"keep,omitempty"`
}

// otherFiles works out the album folders the plan moves whole (see FolderMove), and the images
// in them, as rows of their own.
func (lp *libraryPlan) otherFiles(places []placement) ([]FolderMove, []Row) {
	// The folders the Tracks leave, by the folder they go to.
	sources := map[string][]string{}
	dest := map[string]string{} // a moving Track's new folder, by its ID
	for _, p := range places {
		if p.status != StatusMove {
			continue
		}
		to := dirOf(p.newPath)
		dest[p.mf.ID] = to
		from := dirOf(p.mf.Path)
		if !slices.Contains(sources[to], from) {
			sources[to] = append(sources[to], from)
		}
	}
	var folders []FolderMove
	var rows []Row
	for _, to := range slices.Sorted(maps.Keys(sources)) {
		for _, root := range lp.albumRoots(sources[to], to, dest) {
			folders = append(folders, FolderMove{LibraryID: lp.lib.ID, Path: root, NewPath: to})
			rows = append(rows, lp.imagesUnder(root, to)...)
		}
	}
	rows = lp.flagFileCollisions(rows, places)
	for i := range folders {
		f := &folders[i]
		for _, r := range rows {
			if r.Status != StatusFile && strings.HasPrefix(r.Path, f.Path+"/") {
				f.Keep = append(f.Keep, r.Path)
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
	if dir == "" || inside(dir, to) || inside(to, dir) || lp.hasTwin(dir) {
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

// imagesUnder plans each image the scan found under the folder root to the same place under the
// folder to.
func (lp *libraryPlan) imagesUnder(root, to string) []Row {
	var rows []Row
	for _, dir := range lp.subtree(root) {
		for _, name := range slices.Sorted(slices.Values(lp.images[dir])) {
			rel := path.Join(dir, name)
			rows = append(rows, Row{LibraryID: lp.lib.ID, Path: rel,
				NewPath: path.Join(to, strings.TrimPrefix(rel, root+"/")), Status: StatusFile})
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
