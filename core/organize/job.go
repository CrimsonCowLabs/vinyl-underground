package organize

import (
	"context"
	"encoding/json"
	"path/filepath"

	"github.com/Masterminds/squirrel"
	"path"
	"slices"
	"strings"

	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/safefile"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// maxScanTargets bounds the folders a job rescans one by one; more, and it rescans the library.
const maxScanTargets = 200

// JobKinds are Organize's job kind, for the edit job queue (editjob.Queue.Handle).
func JobKinds(ds model.DataStore, editor *tagedit.Editor) map[model.EditKind]editjob.JobKind {
	return map[model.EditKind]editjob.JobKind{model.EditOrganize: jobKind{ds: ds, editor: editor}}
}

// jobKind is what an Organize job does beyond moving its Files: once they have moved (or moved
// back, for its undo), it removes the folders they left empty, and rescans their old and new
// folders, so the moved Tracks are read at their new place into the same rows, and the albums
// and folders the app lists are brought up to date.
type jobKind struct {
	ds     model.DataStore
	editor *tagedit.Editor
}

func (k jobKind) Start(context.Context, *model.EditJob) error { return nil }

func (k jobKind) Done(ctx context.Context, job *model.EditJob, _ model.EditJobSteps) error {
	var a jobArgs
	if job.Args != "" {
		if err := json.Unmarshal([]byte(job.Args), &a); err != nil {
			return err
		}
	}
	for _, f := range a.Folders {
		if err := k.sweep(ctx, job, f); err != nil {
			return err
		}
	}
	return k.tidy(ctx, job)
}

// jobArgs are an Organize job's Args: the album folders whose other Files move with their Tracks.
type jobArgs struct {
	Folders []FolderMove `json:"folders,omitempty"`
}

func (a jobArgs) String() string {
	b, _ := json.Marshal(a)
	return string(b)
}

// sweep moves the Files left in the album folder f once its Tracks have moved (see FolderMove):
// every File that is no Track's, to the same place under f.NewPath, each through the write path
// and journaled with the job, so the job's undo puts it back too. While a Track is still under
// f.Path (its move was skipped), the folder is the album's still, and keeps them. A File that
// can't move (its new path is taken) stays. It is safe to run again.
func (k jobKind) sweep(ctx context.Context, job *model.EditJob, f FolderMove) error {
	lib, err := k.ds.Library(ctx).Get(f.LibraryID)
	if err != nil {
		return err
	}
	left, err := k.ds.MediaFile(ctx).CountAll(model.QueryOptions{Filters: squirrel.And{
		squirrel.Eq{"media_file.library_id": f.LibraryID, "media_file.missing": false},
		squirrel.Expr("(media_file.path >= ? collate nocase and media_file.path < ? collate nocase)", f.Path+"/", f.Path+"0"),
	}})
	if err != nil {
		return err
	}
	if left > 0 {
		log.Info(ctx, "Organize left an album folder's other Files, as some of its Tracks didn't move", "job", job.ID, "folder", f.Path)
		return nil
	}
	// A Track's old copy a move couldn't remove (it is removed at the next start) isn't the
	// album's other File.
	entries, err := k.ds.EditJournal(ctx).ForJob(job.ID)
	if err != nil {
		return err
	}
	moved := map[string]bool{}
	for _, e := range entries {
		if e.LibraryID == f.LibraryID && e.MediaFileID != "" {
			moved[e.Path] = true
		}
	}
	fsys := k.editor.FS()
	dirs := []string{f.Path}
	for i := 0; i < len(dirs); i++ {
		entries, err := fsys.ReadDir(filepath.Join(lib.Path, filepath.FromSlash(dirs[i])))
		if err != nil {
			continue // gone already
		}
		for _, e := range entries {
			rel := path.Join(dirs[i], e.Name())
			switch {
			case e.IsDir():
				dirs = append(dirs, rel)
			case !e.Type().IsRegular(), strings.Contains(e.Name(), ".vu-"), slices.Contains(f.Keep, rel), moved[rel]:
				// A link, a write's hidden temp, a File the plan flagged, or a Track's old copy.
			default:
				m := tagedit.Move{LibraryID: f.LibraryID, Path: rel, NewPath: path.Join(f.NewPath, strings.TrimPrefix(rel, f.Path+"/"))}
				if _, err := k.editor.MoveInJob(ctx, job.ID, m); err != nil {
					log.Warn(ctx, "Organize could not move one of an album's other Files; it stays", "job", job.ID, "path", rel, "newPath", m.NewPath, err)
				}
			}
		}
	}
	return nil
}

func (k jobKind) Undone(ctx context.Context, job *model.EditJob, _ model.EditJobSteps) error {
	return k.tidy(ctx, job)
}

// tidy removes the folders the job's moves (or their undo) left empty, and rescans the folders
// its Files were in and are in. It is safe to run again: only empty folders are removed.
func (k jobKind) tidy(ctx context.Context, job *model.EditJob) error {
	entries, err := k.ds.EditJournal(ctx).ForJob(job.ID)
	if err != nil {
		return err
	}
	dirs := map[int][]string{}
	for _, e := range entries {
		if e.Kind != model.EditMove || e.State != model.EditWritten {
			continue
		}
		for _, p := range []string{e.Path, e.NewPath} {
			if d := dirOf(p); !slices.Contains(dirs[e.LibraryID], d) {
				dirs[e.LibraryID] = append(dirs[e.LibraryID], d)
			}
		}
	}
	var targets []model.ScanTarget
	for libID, ds := range dirs {
		lib, err := k.ds.Library(ctx).Get(libID)
		if err != nil {
			return err
		}
		// Deepest first, so a folder's subfolders go before it.
		slices.SortFunc(ds, func(a, b string) int { return strings.Count(b, "/") - strings.Count(a, "/") })
		gone := map[string]bool{}
		for _, d := range ds {
			removed := safefile.RemoveEmptyDirs(k.editor.FS(), lib.Path, d)
			if len(removed) > 0 {
				log.Info(ctx, "Organize removed the folders it left empty", "job", job.ID, "folders", removed)
			}
			for _, r := range removed {
				gone[r] = true
			}
		}
		// A folder that is gone is rescanned from the folder it was in, so the scanner sees it,
		// and every folder it was in that went with it, missing.
		scan := make([]string, 0, len(ds))
		for _, d := range ds {
			for gone[d] {
				d = dirOf(d)
			}
			scan = append(scan, d)
		}
		targets = append(targets, scanTargets(libID, scan)...)
	}
	if len(targets) == 0 {
		return nil
	}
	if err := k.editor.RescanTargets(ctx, targets); err != nil {
		log.Warn(ctx, "Scan after Organize failed; the moved Tracks show at their new place after the next scan", "job", job.ID, err)
	}
	return nil
}

// scanTargets are the folders dirs of the library libID to rescan: each one not inside another,
// or the whole library when there are many (or one is its root).
func scanTargets(libID int, dirs []string) []model.ScanTarget {
	sorted := slices.Clone(dirs)
	slices.Sort(sorted)
	var keep []string
	for _, d := range sorted {
		if d == "" {
			return []model.ScanTarget{{LibraryID: libID}}
		}
		if len(keep) > 0 && (d == keep[len(keep)-1] || strings.HasPrefix(d, keep[len(keep)-1]+"/")) {
			continue
		}
		keep = append(keep, d)
	}
	if len(keep) > maxScanTargets {
		return []model.ScanTarget{{LibraryID: libID}}
	}
	res := make([]model.ScanTarget, len(keep))
	for i, d := range keep {
		res[i] = model.ScanTarget{LibraryID: libID, FolderPath: path.Clean(d)}
	}
	return res
}
