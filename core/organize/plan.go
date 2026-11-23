// Package organize is Organize (#167, ADR-0011): it makes the Library's paths match its tags, in
// one scheme for the whole Library (core/libpath, which Promote shares):
//
//	<Album Artist>/<Album> (<Year>)/<NN> <Title>.<ext>, or <D>-<NN> <Title>.<ext> on a multi-disc album
//
// It never runs by itself. Plan works out every move for an album, a selection of Tracks or the
// whole Library, and the Tracks it can't place, with why, so the admin can fix their tags first.
// Run queues the plan the admin reviewed as one edit job of moves (tagedit.Editor.MoveInJob), each
// journaled, each keeping its Track's ID; the job's kind removes the folders it left empty and
// rescans, and its whole-job undo puts every File back.
package organize

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/libpath"
	"github.com/navidrome/navidrome/core/librarymanager"
	"github.com/navidrome/navidrome/core/retag"
	"github.com/navidrome/navidrome/core/safefile"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
)

// What a plan does with a File (Row.Status): it moves a Track's File (StatusMove), or another
// File of its album's folder with it (StatusFile, e.g. cover.jpg), or it leaves the File where it
// is, for a reason (one of the codes below), which the UI translates.
const (
	StatusMove = "move"
	StatusFile = "file"

	// The Track has no album artist, or one that is unknown, or none of its own while the other
	// Tracks of its album in its folder have other artists (it would be split from them).
	ReasonNoAlbumArtist = "noAlbumArtist"
	// The Track's album is empty, or unknown.
	ReasonNoAlbum = "noAlbum"
	// The Track's title is empty, only a number, or "Track NN" (retag.FlagNoTitle).
	ReasonNoTitle = "noTitle"
	// The Track's title starts with a number, which `retag` would take off or couldn't tell from
	// the title (retag.FlagNumberKept): it would be named "07 05 Song".
	ReasonTitleNumber   = "titleNumber"
	ReasonNoTrackNumber = "noTrackNumber"
	// The Track is on a multi-disc album, with no disc number.
	ReasonNoDiscNumber = "noDiscNumber"
	// The File's path, or its new one, goes through a folder with a twin whose name differs only
	// in letter case: on the NAS either may be reached. Merging such folders is #169's.
	ReasonCaseTwin = "caseTwin"
	// Two Files are planned for one path: neither moves.
	ReasonSamePath = "samePath"
	// A File is at the new path already: it is never moved over.
	ReasonPathTaken = "pathTaken"
	// The Track's library is read-only.
	ReasonEditingOff = "editingOff"
)

var (
	// ErrPlanChanged is a Run whose plan isn't the one the admin reviewed any more: the Library
	// changed in between, so the plan is shown again.
	ErrPlanChanged = errors.New("the Library changed since the plan was made; review it again")
	// ErrNothingToMove is a Run whose plan moves no File.
	ErrNothingToMove = errors.New("every File is in place already, or can't be placed")
)

// Code is the code an Organize error is shown with (see editjob.Code).
func Code(err error) string {
	switch {
	case errors.Is(err, ErrPlanChanged):
		return "planChanged"
	case errors.Is(err, ErrNothingToMove):
		return "nothingToMove"
	}
	return librarymanager.Code(err)
}

// Scope is what a plan organizes: an album (AlbumID, from the album page), or the selected Tracks
// (Selection: by ID, or everything matching the Library manager's filter; an empty filter is the
// whole Library).
type Scope struct {
	AlbumID string `json:"albumId,omitempty"`
	librarymanager.Selection
}

// Row is one File of a plan: where it is and where it goes, or why it stays.
type Row struct {
	TrackID   string `json:"trackId,omitempty"`
	LibraryID int    `json:"libraryId"`
	Path      string `json:"path"`
	NewPath   string `json:"newPath,omitempty"`
	Title     string `json:"title,omitempty"`
	Artist    string `json:"artist,omitempty"`
	Album     string `json:"album,omitempty"`
	Status    string `json:"status"`
}

// Plan is what Organize would do: Moves are the Files it moves, the scope's Tracks first (in their
// new paths' order), then the other Files of their albums' folders; Unplaced are the Files it
// leaves where they are, each with its reason. InPlace counts the Tracks already where the scheme
// puts them. Token names the plan, so Run moves only what was reviewed.
type Plan struct {
	Token    string `json:"token"`
	Moves    []Row  `json:"moves"`
	Unplaced []Row  `json:"unplaced"`
	InPlace  int    `json:"inPlace"`
	// Folders are the album folders the plan moves whole (see FolderMove).
	Folders []FolderMove `json:"folders"`
}

// Organizer plans and runs Organize.
type Organizer struct {
	ds      model.DataStore
	queue   *editjob.Queue
	fsys    safefile.FS
	manager *librarymanager.Manager
}

// New returns an Organizer that queues its jobs on queue, and reads the folders Tracks go to
// through fsys (the real filesystem when nil).
func New(ds model.DataStore, queue *editjob.Queue, fsys safefile.FS) *Organizer {
	if fsys == nil {
		fsys = safefile.OS{}
	}
	return &Organizer{ds: ds, queue: queue, fsys: fsys, manager: librarymanager.New(ds, queue)}
}

// Run queues the plan of the scope as one edit job (model.EditOrganize), with one move step per
// Track's File (the album folders' other Files move with them, see FolderMove), if it is still the
// plan the admin reviewed (token, else ErrPlanChanged).
func (o *Organizer) Run(ctx context.Context, scope Scope, token string) (*model.EditJob, error) {
	p, err := o.Plan(ctx, scope)
	if err != nil {
		return nil, err
	}
	if p.Token != token {
		return nil, ErrPlanChanged
	}
	if len(p.Moves) == 0 {
		return nil, ErrNothingToMove
	}
	steps := make([]model.EditJobStep, 0, len(p.Moves))
	for _, r := range p.Moves {
		if r.Status == StatusFile {
			continue // moved with its folder, once its album's Tracks have (see FolderMove)
		}
		s, err := editjob.MoveStep(tagedit.Move{LibraryID: r.LibraryID, TrackID: r.TrackID, Path: r.Path, NewPath: r.NewPath})
		if err != nil {
			return nil, err
		}
		steps = append(steps, s)
	}
	return o.queue.EnqueueWith(ctx, model.EditOrganize, jobArgs{Folders: p.Folders}.String(), steps)
}

// Plan works out what Organize would do to the scope (see Plan), from the Tracks' tags as
// scanned and the Library's folders and their images as the database has them; only an existing
// folder a Track goes to is read from disk, to find a File in the way. Nothing is written.
func (o *Organizer) Plan(ctx context.Context, scope Scope) (*Plan, error) {
	tracks, err := o.scopeTracks(ctx, scope)
	if err != nil {
		return nil, err
	}
	byLib := map[int]model.MediaFiles{}
	for _, mf := range tracks {
		byLib[mf.LibraryID] = append(byLib[mf.LibraryID], mf)
	}
	res := &Plan{Moves: []Row{}, Unplaced: []Row{}, Folders: []FolderMove{}}
	for _, libID := range slices.Sorted(maps.Keys(byLib)) {
		lp, err := o.planLibrary(ctx, libID, byLib[libID])
		if err != nil {
			return nil, err
		}
		res.Moves = append(res.Moves, lp.moves...)
		res.Unplaced = append(res.Unplaced, lp.unplaced...)
		res.InPlace += lp.inPlace
		res.Folders = append(res.Folders, lp.folders...)
	}
	res.Token = token(res.Moves)
	return res, nil
}

func (o *Organizer) scopeTracks(ctx context.Context, scope Scope) (model.MediaFiles, error) {
	if scope.AlbumID != "" {
		return o.ds.MediaFile(ctx).GetAll(model.QueryOptions{Sort: "path",
			Filters: squirrel.And{squirrel.Eq{"media_file.album_id": scope.AlbumID}, squirrel.Eq{"media_file.missing": false}}})
	}
	return o.manager.Tracks(ctx, scope.Selection)
}

// token names the moves: the same moves, the same token.
func token(moves []Row) string {
	h := sha256.New()
	for _, m := range moves {
		_, _ = fmt.Fprintf(h, "%d\x00%s\x00%s\x00%s\n", m.LibraryID, m.TrackID, m.Path, m.NewPath)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// libraryPlan is the plan of one library's Tracks.
type libraryPlan struct {
	o   *Organizer
	lib *model.Library
	// children are the names of each folder's subfolders (by its path, "" for the root), as the
	// database has them.
	children map[string][]string
	// planned are the folders the plan makes, by the Fold of their path, with their name.
	planned map[string]string
	// tracksIn are the library's Tracks by folder; trackAt its Tracks by the Fold of their path.
	tracksIn map[string][]*model.MediaFile
	trackAt  map[string]*model.MediaFile
	// images are the image Files the scan found in each folder, by its path; folderAt the Folds
	// of the folders' paths.
	images   map[string][]string
	folderAt map[string]bool
	// listed are the folders read from disk: the Folds of their entries' names, or nil when the
	// folder isn't there.
	listed map[string]map[string]bool

	moves, unplaced []Row
	folders         []FolderMove
	inPlace         int
}

// placement is where a Track goes, or why it stays.
type placement struct {
	mf      *model.MediaFile
	newPath string
	status  string
}

func (o *Organizer) planLibrary(ctx context.Context, libID int, scope model.MediaFiles) (*libraryPlan, error) {
	lib, err := o.ds.Library(ctx).Get(libID)
	if err != nil {
		return nil, err
	}
	all, err := o.ds.MediaFile(ctx).GetAll(model.QueryOptions{
		Filters: squirrel.And{squirrel.Eq{"media_file.library_id": libID}, squirrel.Eq{"media_file.missing": false}}})
	if err != nil {
		return nil, err
	}
	folders, err := o.ds.Folder(ctx).GetAll(model.QueryOptions{
		Filters: squirrel.And{squirrel.Eq{"folder.library_id": libID}, squirrel.Eq{"folder.missing": false}}})
	if err != nil {
		return nil, err
	}
	lp := &libraryPlan{o: o, lib: lib, children: map[string][]string{}, planned: map[string]string{},
		tracksIn: map[string][]*model.MediaFile{}, trackAt: map[string]*model.MediaFile{}, listed: map[string]map[string]bool{},
		images: map[string][]string{}, folderAt: map[string]bool{}}
	for _, f := range folders {
		if f.ParentID == "" {
			continue // the root
		}
		parent := cleanDir(f.Path)
		lp.children[parent] = append(lp.children[parent], f.Name)
		rel := path.Join(parent, f.Name)
		lp.images[rel] = f.ImageFiles
		lp.folderAt[libpath.Fold(rel)] = true
	}
	for i := range all {
		mf := &all[i]
		lp.tracksIn[dirOf(mf.Path)] = append(lp.tracksIn[dirOf(mf.Path)], mf)
		lp.trackAt[libpath.Fold(mf.Path)] = mf
	}

	places := lp.place(scope, albumsOf(all))
	lp.flagCollisions(places)
	var files []Row
	lp.folders, files = lp.otherFiles(places)

	for _, p := range places {
		r := row(p.mf)
		switch p.status {
		case "":
			lp.inPlace++
		case StatusMove:
			r.NewPath, r.Status = p.newPath, StatusMove
			lp.moves = append(lp.moves, r)
		default:
			r.NewPath, r.Status = p.newPath, p.status
			lp.unplaced = append(lp.unplaced, r)
		}
	}
	slices.SortStableFunc(lp.moves, func(a, b Row) int { return strings.Compare(a.NewPath, b.NewPath) })
	for _, f := range files {
		if f.Status == StatusFile {
			lp.moves = append(lp.moves, f)
		} else {
			lp.unplaced = append(lp.unplaced, f)
		}
	}
	return lp, nil
}

func row(mf *model.MediaFile) Row {
	return Row{TrackID: mf.ID, LibraryID: mf.LibraryID, Path: mf.Path, Title: mf.Title, Artist: mf.Artist, Album: mf.Album}
}

// album is what the scheme needs to know of an album, from all its Tracks in the library: its
// year, and whether it has several discs.
type album struct {
	year      int
	multiDisc bool
}

func albumsOf(all model.MediaFiles) map[string]album {
	res := map[string]album{}
	original := map[string]int{}
	for _, mf := range all {
		a := res[mf.AlbumID]
		a.year = max(a.year, mf.Year)
		a.multiDisc = a.multiDisc || mf.DiscNumber > 1
		res[mf.AlbumID] = a
		original[mf.AlbumID] = max(original[mf.AlbumID], mf.OriginalYear)
	}
	// The year the album page shows: its original year, when it has one.
	for id, y := range original {
		if y > 0 {
			a := res[id]
			a.year = y
			res[id] = a
		}
	}
	return res
}

// place works out each Track's new path, or why it has none.
func (lp *libraryPlan) place(scope model.MediaFiles, albums map[string]album) []placement {
	// The plan's own new folders are named by the first Track (in path order) to need each.
	slices.SortStableFunc(scope, func(a, b model.MediaFile) int { return strings.Compare(a.Path, b.Path) })
	res := make([]placement, 0, len(scope))
	for i := range scope {
		mf := &scope[i]
		p := placement{mf: mf}
		newPath, reason := lp.newPath(mf, albums[mf.AlbumID])
		switch {
		case reason != "":
			p.status = reason
		case libpath.Fold(newPath) == libpath.Fold(mf.Path):
			// In place. A name that differs only in letter case stays: on the NAS it is the same.
		default:
			p.newPath, p.status = newPath, StatusMove
		}
		res = append(res, p)
	}
	return res
}

// newPath is the Track's path under the scheme, with its folders as the library has them (or
// as the plan makes them), or the reason it has none.
func (lp *libraryPlan) newPath(mf *model.MediaFile, al album) (string, string) {
	if !tagedit.Writable(mf.LibraryID) {
		return "", ReasonEditingOff
	}
	if reason := lp.unplaceable(mf, al); reason != "" {
		return "", reason
	}
	if lp.hasTwin(dirOf(mf.Path)) || lp.fileTwin(mf) {
		return "", ReasonCaseTwin
	}
	artist := libpath.SafeName(mf.AlbumArtist, "")
	albumDir := libpath.AlbumFolder(libpath.SafeName(mf.Album, ""), al.year)
	dir, ok := lp.folder(artist, albumDir)
	if !ok {
		return "", ReasonCaseTwin
	}
	file := libpath.TrackFile(libpath.SafeName(mf.Title, ""), mf.DiscNumber, mf.TrackNumber, al.multiDisc, path.Ext(mf.Path))
	return path.Join(dir, file), ""
}

// unplaceable is why the Track's tags can't name its place, if they can't: retag's flags tell a
// title that isn't one, or still starts with a number.
func (lp *libraryPlan) unplaceable(mf *model.MediaFile, al album) string {
	if libpath.SafeName(mf.AlbumArtist, "") == "" || retag.UnknownArtist(mf.AlbumArtist) || lp.splitAlbum(mf) {
		return ReasonNoAlbumArtist
	}
	if libpath.SafeName(mf.Album, "") == "" || retag.UnknownAlbum(mf.Album) {
		return ReasonNoAlbum
	}
	stem := strings.TrimSuffix(path.Base(mf.Path), path.Ext(mf.Path))
	c := retag.Clean(retag.Local{Title: mf.Title, Artist: mf.Artist, Album: mf.Album, AlbumArtist: mf.AlbumArtist,
		Track: mf.TrackNumber, Disc: mf.DiscNumber, Year: mf.Year, Stem: stem, Folder: path.Base(dirOf(mf.Path))})
	switch {
	case (c.Has(retag.FlagNoTitle) && !numberTitle(mf)) || libpath.SafeName(mf.Title, "") == "":
		return ReasonNoTitle
	case c.Has(retag.FlagNumberKept) || c.TookNumber():
		return ReasonTitleNumber
	case mf.TrackNumber <= 0:
		return ReasonNoTrackNumber
	case al.multiDisc && mf.DiscNumber <= 0:
		return ReasonNoDiscNumber
	}
	return ""
}

// numberTitle reports whether the Track's title is a number that isn't its track number: a real
// title ("1979", "2112"), which retag's FlagNoTitle can't tell from a lost one ("05" on track 5).
func numberTitle(mf *model.MediaFile) bool {
	n, err := strconv.Atoi(strings.TrimSpace(mf.Title))
	return err == nil && n != mf.TrackNumber
}

// splitAlbum reports whether the Track has no album artist of its own (the scanner gave it its
// artist), while another Track of the same album in its folder has another artist: placed by its
// artist, the album would be split, so its album artist is set first.
func (lp *libraryPlan) splitAlbum(mf *model.MediaFile) bool {
	if mf.HasAlbumArtist || mf.Compilation {
		return false
	}
	for _, other := range lp.tracksIn[dirOf(mf.Path)] {
		if libpath.SameName(other.Album, mf.Album) && !libpath.SameName(other.AlbumArtist, mf.AlbumArtist) {
			return true
		}
	}
	return false
}

// folder returns the path of the folders names (one inside the other, from the library's root):
// for each, the existing folder whose name differs only in letter case, or the one the plan makes
// already, or the name. ok is false when an existing folder has a twin (see ReasonCaseTwin).
func (lp *libraryPlan) folder(names ...string) (string, bool) {
	parent := ""
	for _, name := range names {
		var got []string
		for _, c := range lp.children[parent] {
			if libpath.SameName(c, name) {
				got = append(got, c)
			}
		}
		switch len(got) {
		case 0:
			key := libpath.Fold(path.Join(parent, name))
			if planned, ok := lp.planned[key]; ok {
				name = planned
			} else {
				lp.planned[key] = name
			}
		case 1:
			name = got[0]
		default:
			return "", false
		}
		parent = path.Join(parent, name)
	}
	return parent, true
}

// hasTwin reports whether the folder dir, or any above it, has a twin (see ReasonCaseTwin).
func (lp *libraryPlan) hasTwin(dir string) bool {
	for ; dir != ""; dir = dirOf(dir) {
		n := 0
		for _, c := range lp.children[dirOf(dir)] {
			if libpath.SameName(c, path.Base(dir)) {
				n++
			}
		}
		if n > 1 {
			return true
		}
	}
	return false
}

// fileTwin reports whether another Track's File in the Track's folder has a name differing from
// its own only in letter case.
func (lp *libraryPlan) fileTwin(mf *model.MediaFile) bool {
	for _, other := range lp.tracksIn[dirOf(mf.Path)] {
		if other.ID != mf.ID && libpath.SameName(other.Path, mf.Path) {
			return true
		}
	}
	return false
}

// flagCollisions leaves where they are the Tracks planned for one path (ReasonSamePath), and
// those whose new path is taken by a File already there (ReasonPathTaken).
func (lp *libraryPlan) flagCollisions(places []placement) {
	byPath := map[string][]int{}
	for i, p := range places {
		if p.status == StatusMove {
			byPath[libpath.Fold(p.newPath)] = append(byPath[libpath.Fold(p.newPath)], i)
		}
	}
	for key, idx := range byPath {
		if len(idx) > 1 {
			for _, i := range idx {
				places[i].status = ReasonSamePath
			}
			continue
		}
		p := &places[idx[0]]
		if other, ok := lp.trackAt[key]; (ok && other.ID != p.mf.ID) || (!ok && lp.taken(p.newPath)) {
			p.status = ReasonPathTaken
		}
	}
}

// taken reports whether a File is at rel already, in any letter case: a Track, an image the scan
// found, or, in a folder the library has (the only ones read from disk), any File.
func (lp *libraryPlan) taken(rel string) bool {
	if lp.trackAt[libpath.Fold(rel)] != nil {
		return true
	}
	dir := dirOf(rel)
	for _, img := range lp.images[dir] {
		if libpath.SameName(img, path.Base(rel)) {
			return true
		}
	}
	return lp.folderAt[libpath.Fold(dir)] && lp.onDisk(rel)
}

// onDisk reports whether something is at rel on disk, in any letter case. Each folder is read
// once.
func (lp *libraryPlan) onDisk(rel string) bool {
	names := lp.list(dirOf(rel))
	return names[libpath.Fold(path.Base(rel))]
}

// list reads the folder dir from disk (once): the Folds of its entries' names.
func (lp *libraryPlan) list(dir string) map[string]bool {
	if names, ok := lp.listed[dir]; ok {
		return names
	}
	var names map[string]bool
	entries, err := lp.o.fsys.ReadDir(lp.abs(dir))
	if err == nil {
		names = make(map[string]bool, len(entries))
		for _, e := range entries {
			names[libpath.Fold(e.Name())] = true
		}
	}
	lp.listed[dir] = names
	return names
}

func (lp *libraryPlan) abs(rel string) string {
	return filepath.Join(lp.lib.Path, filepath.FromSlash(rel))
}

func cleanDir(p string) string {
	p = strings.Trim(path.Clean("/"+filepath.ToSlash(p)), "/")
	return cmp.Or(p, "")
}

// dirOf is the folder of the path p, "" for the library's root.
func dirOf(p string) string {
	d := path.Dir(p)
	if d == "." || d == "/" {
		return ""
	}
	return d
}
