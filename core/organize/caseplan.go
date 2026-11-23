package organize

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/libpath"
	"github.com/navidrome/navidrome/core/quarantine"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
)

// The case merge through Organize (#169): CasePlan works out the next phase of the case merge for
// every library the app can write, and RunCase queues the plan the admin reviewed as one edit job,
// journaled and undoable as a whole from the Edit history (see casemerge.go for the two phases).
//
// What can be trusted on the NAS decides where each phase reads:
//
//   - Phase A finds the clashes in the database (folders, Tracks and images as scanned: a folder
//     that isn't in a clash lists its children reliably, so both twins are there) and checks each
//     against a listing of its folder on disk, so a rename the scan hasn't seen yet isn't planned
//     twice. It plans only the outermost clashes; the job's rescan then reads the renamed folders.
//   - Every folder already given a temporary name (by a Phase A job, as the Edit journal has it, or
//     by casemerge.py, as a scan found it) is read from disk with its partner, whole: with unique
//     names those listings are reliable, and they hold the Files scans miss. A clash nested in them
//     is Phase A again; once none is left, Phase B merges them from those listings.
//
// Phase A runs until no clash is left; its plan names the pass. A Phase B File that clashes with
// its partner's and differs is listed for the admin (Decide), never resolved.

// The phases of a case merge (CasePlan.Phase).
const (
	PhaseRename = "rename"
	PhaseMerge  = "merge"
)

// ErrNoCaseMerge is a RunCase with nothing to rename or merge.
var ErrNoCaseMerge = errors.New("no names differ only by letter case, and nothing is left to merge")

// CasePlan is the next phase of the case merge: every set of case-colliding siblings found
// (Clashes), and the steps of the phase (Phase A's renames, or Phase B's moves, folder removals,
// renames and Duplicates to Quarantine), with the conflicts left for the admin (Decide). Phase is
// empty when there is nothing to do. Token names the plan, so RunCase runs only what was reviewed.
type CasePlan struct {
	Token   string      `json:"token"`
	Phase   string      `json:"phase"`
	Clashes []CaseClash `json:"clashes"`
	Steps   []CaseStep  `json:"steps"`
	Decide  []CaseStep  `json:"decide"`
}

// CasePlan works out the next phase of the case merge (see CasePlan). Nothing is written.
func (o *Organizer) CasePlan(ctx context.Context) (*CasePlan, error) {
	libs, err := o.ds.Library(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	res := &CasePlan{Clashes: []CaseClash{}, Steps: []CaseStep{}, Decide: []CaseStep{}}
	type merge struct {
		cl   *caseLibrary
		walk []CaseEntry
	}
	var merges []merge
	for i := range libs {
		lib := &libs[i]
		if !tagedit.Writable(lib.ID) {
			continue
		}
		cl, err := o.caseLibrary(ctx, lib)
		if err != nil {
			return nil, err
		}
		clashes, renames := cl.disambiguation()
		var walk []CaseEntry
		if len(renames) == 0 {
			// The tagged names and their partners, read whole: the clashes nested in them.
			walk = cl.walkTagged()
			clashes = findCaseClashes(walk)
			renames = planDisambiguation(walk)
		}
		res.Clashes = append(res.Clashes, cl.withLibrary(clashes)...)
		if len(renames) > 0 {
			res.Phase = PhaseRename
			res.Steps = append(res.Steps, cl.renameSteps(renames)...)
			continue
		}
		merges = append(merges, merge{cl: cl, walk: walk})
	}
	if res.Phase == "" {
		for _, m := range merges {
			steps, err := planCaseMergesWith(m.walk, caseLook{alike: m.cl.alike, taken: m.cl.taken})
			if err != nil {
				return nil, err
			}
			steps = m.cl.withTracks(steps, m.walk)
			for _, s := range steps {
				if s.Op == CaseDecide {
					res.Decide = append(res.Decide, s)
				} else {
					res.Steps = append(res.Steps, s)
				}
			}
		}
		if len(res.Steps) > 0 {
			res.Phase = PhaseMerge
		}
	}
	res.Token = caseToken(res.Phase, res.Steps)
	return res, nil
}

// RunCase queues the plan's phase as one edit job (model.EditCaseRename for Phase A,
// model.EditCaseMerge for Phase B), if it is still the plan the admin reviewed (token, else
// ErrPlanChanged). The job's kind rescans the folders its steps touched, so the next plan reads
// them as they are.
func (o *Organizer) RunCase(ctx context.Context, token string) (*model.EditJob, error) {
	p, err := o.CasePlan(ctx)
	if err != nil {
		return nil, err
	}
	if p.Token != token {
		return nil, ErrPlanChanged
	}
	if p.Phase == "" {
		return nil, ErrNoCaseMerge
	}
	steps := make([]model.EditJobStep, 0, len(p.Steps))
	for _, s := range p.Steps {
		step, err := caseJobStep(s)
		if err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}
	kind := model.EditCaseRename
	if p.Phase == PhaseMerge {
		kind = model.EditCaseMerge
	}
	return o.queue.EnqueueWith(ctx, kind, "", steps)
}

// caseJobStep is the job step of a case merge step: a File's move is a move (MoveInJob, keeping
// its Track); a folder's, or a rename, a rename of the folder with all it holds.
func caseJobStep(s CaseStep) (model.EditJobStep, error) {
	switch s.Op {
	case CaseMove:
		if s.dir {
			return editjob.RenameStep(tagedit.Rename{LibraryID: s.LibraryID, Path: s.Path, NewPath: s.NewPath})
		}
		return editjob.MoveStep(tagedit.Move{LibraryID: s.LibraryID, TrackID: s.TrackID, Path: s.Path, NewPath: s.NewPath})
	case CaseRename:
		return editjob.RenameStep(tagedit.Rename{LibraryID: s.LibraryID, Path: s.Path, NewPath: s.NewPath})
	case CaseRemoveDir:
		return editjob.RemoveDirStep(s.LibraryID, s.Path), nil
	case CaseQuarantine:
		return editjob.DuplicateStep(tagedit.Duplicate{LibraryID: s.LibraryID, TrackID: s.TrackID, Path: s.Path, NewPath: s.NewPath, Keep: s.Keep})
	}
	return model.EditJobStep{}, fmt.Errorf("a case merge has no %q step", s.Op)
}

func caseToken(phase string, steps []CaseStep) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s\n", phase)
	for _, s := range steps {
		_, _ = fmt.Fprintf(h, "%d\x00%s\x00%s\x00%s\x00%s\x00%s\n", s.LibraryID, s.Op, s.Path, s.NewPath, s.Keep, s.TrackID)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// caseLibrary is what the case merge reads of one library.
type caseLibrary struct {
	o   *Organizer
	lib *model.Library
	// entries are the library's folders, Tracks and images, as scanned.
	entries []CaseEntry
	// trackAt are the library's Tracks, by path (exact case).
	trackAt map[string]string
	// tagged are the paths with a Phase A tag: the folders and Files the database has, and the
	// names the Edit journal's renames gave.
	tagged []string
	// notDuplicate are the Files an earlier case merge job found weren't byte-identical to the
	// File they would duplicate (skipped, ErrNotDuplicate): the admin's to decide now.
	notDuplicate map[string]bool
}

func (o *Organizer) caseLibrary(ctx context.Context, lib *model.Library) (*caseLibrary, error) {
	cl := &caseLibrary{o: o, lib: lib, trackAt: map[string]string{}, notDuplicate: map[string]bool{}}
	if err := cl.readNotDuplicates(ctx); err != nil {
		return nil, err
	}
	folders, err := o.ds.Folder(ctx).GetAll(model.QueryOptions{
		Filters: squirrel.And{squirrel.Eq{"folder.library_id": lib.ID}, squirrel.Eq{"folder.missing": false}}})
	if err != nil {
		return nil, err
	}
	for _, f := range folders {
		if f.ParentID == "" {
			continue
		}
		rel := path.Join(cleanDir(f.Path), f.Name)
		cl.entries = append(cl.entries, CaseEntry{Path: rel, Dir: true})
		for _, img := range f.ImageFiles {
			cl.entries = append(cl.entries, CaseEntry{Path: path.Join(rel, img)})
		}
	}
	tracks, err := o.ds.MediaFile(ctx).GetAll(model.QueryOptions{
		Filters: squirrel.And{squirrel.Eq{"media_file.library_id": lib.ID}, squirrel.Eq{"media_file.missing": false}}})
	if err != nil {
		return nil, err
	}
	for _, mf := range tracks {
		cl.entries = append(cl.entries, CaseEntry{Path: mf.Path, Size: mf.Size})
		cl.trackAt[mf.Path] = mf.ID
	}
	renames, err := o.ds.EditJournal(ctx).GetAll(model.QueryOptions{Filters: squirrel.Eq{
		"library_id": lib.ID, "kind": model.EditRename, "state": model.EditWritten}})
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	addTagged := func(p string) {
		for ; p != "" && p != "."; p = dirOf(p) {
			if HasCaseTag(path.Base(p)) && !seen[p] {
				seen[p] = true
				cl.tagged = append(cl.tagged, p)
			}
		}
	}
	for _, e := range cl.entries {
		addTagged(e.Path)
	}
	for _, r := range renames {
		addTagged(r.NewPath)
	}
	return cl, nil
}

// disambiguation is Phase A from the database (but Quarantine): the outermost clashes it shows
// that are still on disk, as a listing of their folders has them, and their renames.
func (cl *caseLibrary) disambiguation() ([]CaseClash, []CaseStep) {
	var entries []CaseEntry
	for _, e := range cl.entries {
		if !quarantine.Within(e.Path) {
			entries = append(entries, e)
		}
	}
	parents := map[string]bool{}
	for _, s := range planDisambiguation(entries) {
		parents[dirOf(s.Path)] = true
	}
	var listed []CaseEntry
	for _, parent := range slices.Sorted(maps.Keys(parents)) {
		listed = append(listed, cl.listDir(parent)...)
	}
	return findCaseClashes(listed), planDisambiguation(listed)
}

// listDir is the folder rel's entries on disk (none if it can't be read).
func (cl *caseLibrary) listDir(rel string) []CaseEntry {
	entries, err := cl.o.fsys.ReadDir(cl.abs(rel))
	if err != nil {
		return nil
	}
	res := make([]CaseEntry, 0, len(entries))
	for _, e := range entries {
		ce := CaseEntry{Path: joinRel(rel, e.Name()), Dir: e.IsDir()}
		if !ce.Dir {
			if info, err := e.Info(); err == nil {
				ce.Size = info.Size()
			}
		}
		res = append(res, ce)
	}
	return res
}

// walkTagged reads from disk, whole, every name with a Phase A tag and its partners (the names in
// its folder that are the same without the tag, in any letter case): what Phase B merges.
func (cl *caseLibrary) walkTagged() []CaseEntry {
	byParent := map[string][]string{}
	for _, t := range cl.tagged {
		if !quarantine.Within(t) {
			byParent[dirOf(t)] = append(byParent[dirOf(t)], t)
		}
	}
	// Shallowest first: a tagged name inside one read already is read with it.
	depth := func(p string) int {
		if p == "" {
			return 0
		}
		return strings.Count(p, "/") + 1
	}
	parents := slices.Collect(maps.Keys(byParent))
	slices.SortFunc(parents, func(a, b string) int {
		if c := depth(a) - depth(b); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})
	var res, roots []CaseEntry
	read := func(p string) bool {
		for _, r := range roots {
			if p == r.Path || strings.HasPrefix(p, r.Path+"/") {
				return true
			}
		}
		return false
	}
	for _, parent := range parents {
		if parent != "" && read(parent) {
			continue
		}
		keys := map[string]bool{}
		for _, t := range byParent[parent] {
			keys[libpath.Fold(stripCaseTag(path.Base(t)))] = true
		}
		for _, e := range cl.listDir(parent) {
			if !keys[libpath.Fold(stripCaseTag(path.Base(e.Path)))] || read(e.Path) {
				continue
			}
			roots = append(roots, e)
			res = append(res, e)
			if e.Dir {
				res = append(res, cl.walk(e.Path)...)
			}
		}
	}
	return res
}

// walk is everything under the folder rel on disk.
func (cl *caseLibrary) walk(rel string) []CaseEntry {
	var res []CaseEntry
	dirs := []string{rel}
	for i := 0; i < len(dirs); i++ {
		for _, e := range cl.listDir(dirs[i]) {
			res = append(res, e)
			if e.Dir {
				dirs = append(dirs, e.Path)
			}
		}
	}
	return res
}

// sampleBytes is how much of each end of two same-size Files the plan compares (see alike).
const sampleBytes = 128 << 10

// alike reports whether the Files keep and other (the same size) start and end with the same
// bytes: a quick look at where tags are (ID3v2 at the start; ID3v1 and APE at the end), so a File
// whose tags differ is left for the admin rather than planned for Quarantine. The job confirms the
// whole File byte-identical before it moves it. A File that can't be read isn't alike.
func (cl *caseLibrary) alike(keep, other string) bool {
	if cl.notDuplicate[other] {
		return false
	}
	ends := func(rel string) ([]byte, error) {
		f, err := cl.o.fsys.Open(cl.abs(rel))
		if err != nil {
			return nil, err
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, sampleBytes))
		if err != nil {
			return nil, err
		}
		info, err := f.Stat()
		if err != nil {
			return nil, err
		}
		if s, ok := f.(io.Seeker); ok && info.Size() > 2*sampleBytes {
			if _, err := s.Seek(-sampleBytes, io.SeekEnd); err != nil {
				return nil, err
			}
		}
		tail, err := io.ReadAll(f)
		if err != nil {
			return nil, err
		}
		return append(b, tail...), nil
	}
	a, err := ends(keep)
	if err != nil {
		return false
	}
	b, err := ends(other)
	return err == nil && bytes.Equal(a, b)
}

// readNotDuplicates reads, from the case merge jobs so far, the Files of the library that a
// Duplicate step skipped as not byte-identical (see notDuplicate).
func (cl *caseLibrary) readNotDuplicates(ctx context.Context) error {
	repo := cl.o.ds.EditJob(ctx)
	jobs, err := repo.GetAll(0)
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if j.Kind != model.EditCaseMerge || j.Skipped == 0 {
			continue
		}
		steps, err := repo.Steps(j.ID, model.EditStepSkipped)
		if err != nil {
			return err
		}
		for _, s := range steps {
			if s.Kind != model.EditQuarantineDuplicate || s.Code != tagedit.Code(tagedit.ErrNotDuplicate) {
				continue
			}
			if d, err := editjob.ReadDuplicate(&s); err == nil && d.LibraryID == cl.lib.ID {
				cl.notDuplicate[d.Path] = true
			}
		}
	}
	return nil
}

// taken reports whether something is at rel already (in Quarantine), in any letter case.
func (cl *caseLibrary) taken(rel string) bool {
	if _, err := cl.o.fsys.Lstat(cl.abs(rel)); err == nil {
		return true
	}
	for _, e := range cl.listDir(dirOf(rel)) {
		if libpath.SameName(path.Base(e.Path), path.Base(rel)) {
			return true
		}
	}
	return false
}

func (cl *caseLibrary) abs(rel string) string { return (&libraryPlan{lib: cl.lib}).abs(rel) }

func (cl *caseLibrary) withLibrary(clashes []CaseClash) []CaseClash {
	for i := range clashes {
		clashes[i].LibraryID = cl.lib.ID
	}
	return clashes
}

// renameSteps gives Phase A's renames their library, and tells a folder's from a File's.
func (cl *caseLibrary) renameSteps(steps []CaseStep) []CaseStep {
	for i := range steps {
		steps[i].LibraryID = cl.lib.ID
		steps[i].dir = cl.isDir(steps[i].Path)
	}
	return steps
}

func (cl *caseLibrary) isDir(rel string) bool {
	info, err := cl.o.fsys.Lstat(cl.abs(rel))
	return err == nil && info.IsDir()
}

// withTracks gives Phase B's steps their library, whether each is a folder's (from the listing),
// and each File's Track: the one at the path the File has now, before the steps before it rename
// its folders.
func (cl *caseLibrary) withTracks(steps []CaseStep, walk []CaseEntry) []CaseStep {
	dirs := map[string]bool{}
	for _, e := range walk {
		if e.Dir {
			dirs[e.Path] = true
		}
	}
	type rename struct{ from, to string }
	var renames []rename
	// original is where the path p (as it is after the renames so far) is now.
	original := func(p string) string {
		for _, r := range slices.Backward(renames) {
			if p == r.to || strings.HasPrefix(p, r.to+"/") {
				p = r.from + strings.TrimPrefix(p, r.to)
			}
		}
		return p
	}
	for i := range steps {
		s := &steps[i]
		s.LibraryID = cl.lib.ID
		now := original(s.Path)
		s.dir = dirs[now]
		if !s.dir && s.Op != CaseRemoveDir {
			s.TrackID = cl.trackAt[now]
		}
		if s.Op == CaseRename || (s.Op == CaseMove && s.dir) {
			renames = append(renames, rename{from: s.Path, to: s.NewPath})
		}
	}
	return steps
}
