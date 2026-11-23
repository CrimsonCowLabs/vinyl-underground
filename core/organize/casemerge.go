package organize

import (
	"errors"
	"fmt"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/navidrome/navidrome/core/libpath"
	"github.com/navidrome/navidrome/core/quarantine"
)

// The case-merge planner (#169), ported from contrib/library-cleanup's casemerge.py, whose test
// cases it keeps. The NAS looks names up case-insensitively (SMB1) and caches the lookups, so for
// two siblings such as `ATB` and `Atb` a path into either may reach the other, and listings under
// them can't be trusted until the names are unique. Merging takes two phases:
//
//   - Phase A (planDisambiguation): every member of each outermost clash but one gets a unique
//     temporary name ("Atb [case-merge]"). A rename can't lose data, whichever real folder the
//     server applies it to. Clashes nested inside clashing folders only become visible once their
//     parents are unique, so the phase is planned again after each pass's rescan, until none is
//     left.
//   - Phase B (planCaseMerges): with every name unique, each "[case-merge]" sibling is merged into
//     its partner. The side with more Files wins; a same-name File of the same size goes to
//     Quarantine (once the job confirms it byte-identical); anything else is left for the admin.
//
// These functions are pure: they plan from a listing (CaseEntry), and never touch the disk.

// The steps of a case merge (CaseStep.Op).
const (
	// CaseRename gives Path the name NewPath, in the same folder: Phase A's temporary names, and
	// Phase B's renamed side taking back the preferred name once its partner is emptied.
	CaseRename = "rename"
	// CaseMove moves the File or folder Path to NewPath, in its partner.
	CaseMove = "move"
	// CaseRemoveDir is the folder Path, left empty by the moves before it.
	CaseRemoveDir = "rmdir"
	// CaseQuarantine sends the File Path to Quarantine (NewPath), as a Duplicate of the File Keep,
	// once the job has confirmed it byte-identical to Keep.
	CaseQuarantine = "quarantine"
	// CaseDecide is a File Path that clashes with NewPath but may differ: the admin decides.
	CaseDecide = "decide"
)

// ErrCaseClashesLeft is a Phase B plan while names still clash: Phase A runs again first.
var ErrCaseClashesLeft = errors.New("names still differ only by letter case; give them temporary names first")

// CaseEntry is a folder or File of a listing, relative to its library, slash-separated.
type CaseEntry struct {
	Path string
	Dir  bool
	Size int64
}

// CaseClash is a set of siblings in the folder Parent whose names differ only by letter case.
type CaseClash struct {
	LibraryID int      `json:"libraryId"`
	Parent    string   `json:"parent"`
	Names     []string `json:"names"`
}

// CaseStep is one step of a case merge (Op is one of the Case* codes above). TrackID is the Track
// whose File Path is, when the database has one (filled in by the Organizer, not the planner).
type CaseStep struct {
	LibraryID int    `json:"libraryId"`
	Op        string `json:"op"`
	Path      string `json:"path"`
	NewPath   string `json:"newPath,omitempty"`
	Keep      string `json:"keep,omitempty"`
	TrackID   string `json:"trackId,omitempty"`
	// dir is a step of a folder's (as the listing has it).
	dir bool
}

type caseNode struct {
	dir      bool
	size     int64
	children map[string]*caseNode
}

func (n *caseNode) fileCount() int {
	if !n.dir {
		return 1
	}
	c := 0
	for _, ch := range n.children {
		c += ch.fileCount()
	}
	return c
}

func buildCaseTree(entries []CaseEntry) *caseNode {
	root := &caseNode{dir: true, children: map[string]*caseNode{}}
	for _, e := range entries {
		node := root
		parts := strings.Split(e.Path, "/")
		for _, part := range parts[:len(parts)-1] {
			next := node.children[part]
			if next == nil {
				next = &caseNode{dir: true, children: map[string]*caseNode{}}
				node.children[part] = next
			}
			node = next
		}
		name := parts[len(parts)-1]
		switch existing := node.children[name]; {
		case existing == nil:
			node.children[name] = &caseNode{dir: e.Dir, size: e.Size, children: map[string]*caseNode{}}
		case !e.Dir:
			existing.dir, existing.size = false, e.Size
		}
	}
	return root
}

func joinRel(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "/" + name
}

// caseTag is Phase A's temporary-name tag: " [case-merge]", then " [case-merge 2]", ...
const caseTag = "case-merge"

var caseTagRE = regexp.MustCompile(` \[` + caseTag + `(?: \d+)?\]`)

// HasCaseTag reports whether the name has Phase A's temporary tag.
func HasCaseTag(name string) bool { return caseTagRE.MatchString(name) }

func withCaseTag(name string, dir bool, n int) string {
	tag := " [" + caseTag + "]"
	if n > 1 {
		tag = fmt.Sprintf(" [%s %d]", caseTag, n)
	}
	stem, ext := name, ""
	if !dir {
		stem, ext = splitExt(name)
	}
	return stem + tag + ext
}

// stripCaseTag is the name without its Phase A tag ("Atb [case-merge]" -> "Atb").
func stripCaseTag(name string) string {
	if loc := caseTagRE.FindStringIndex(name); loc != nil {
		return name[:loc[0]] + name[loc[1]:]
	}
	return name
}

// splitExt splits a name as Python's os.path.splitext does: a name's leading dots aren't an
// extension.
func splitExt(name string) (string, string) {
	ext := path.Ext(name)
	if strings.TrimLeft(strings.TrimSuffix(name, ext), ".") == "" {
		return name, ""
	}
	return strings.TrimSuffix(name, ext), ext
}

// isUpper reports whether the name has letters, all capitals (Python's str.isupper).
func isUpper(s string) bool {
	cased := false
	for _, r := range s {
		switch {
		case unicode.IsLower(r):
			return false
		case unicode.IsUpper(r) || unicode.IsTitle(r):
			cased = true
		}
	}
	return cased
}

// preferName orders the spellings of a name to keep: not ALL CAPS first, then alphabetical.
func preferName(a, b string) int {
	if ua, ub := isUpper(a), isUpper(b); ua != ub {
		if ua {
			return 1
		}
		return -1
	}
	return strings.Compare(a, b)
}

// eachParent calls fn with every (parent, name) along the path, so folders count even when the
// listing only has them as a File's folder.
func eachParent(p string, fn func(parent, name string)) {
	parts := strings.Split(p, "/")
	for i := range parts {
		fn(strings.Join(parts[:i], "/"), parts[i])
	}
}

// findCaseClashes is every set of siblings whose names differ only by letter case, by parent and
// then names.
func findCaseClashes(entries []CaseEntry) []CaseClash {
	groups := map[string]map[string]map[string]bool{}
	for _, e := range entries {
		eachParent(e.Path, func(parent, name string) {
			if groups[parent] == nil {
				groups[parent] = map[string]map[string]bool{}
			}
			key := libpath.Fold(name)
			if groups[parent][key] == nil {
				groups[parent][key] = map[string]bool{}
			}
			groups[parent][key][name] = true
		})
	}
	var res []CaseClash
	for parent, byKey := range groups {
		for _, names := range byKey {
			if len(names) > 1 {
				res = append(res, CaseClash{Parent: parent, Names: slices.Sorted(maps.Keys(names))})
			}
		}
	}
	slices.SortFunc(res, func(a, b CaseClash) int {
		if c := strings.Compare(a.Parent, b.Parent); c != 0 {
			return c
		}
		return slices.Compare(a.Names, b.Names)
	})
	return res
}

// planDisambiguation is Phase A: all but one member of each outermost clash get a unique temporary
// name. Clashes beneath a clashing folder are left for a later pass, as their listings can't be
// trusted yet.
func planDisambiguation(entries []CaseEntry) []CaseStep {
	dirs := map[string]bool{}
	siblings := map[string]map[string]bool{}
	for _, e := range entries {
		if e.Dir {
			dirs[e.Path] = true
		}
		parts := strings.Split(e.Path, "/")
		for i := range parts[:len(parts)-1] {
			dirs[strings.Join(parts[:i+1], "/")] = true
		}
		eachParent(e.Path, func(parent, name string) {
			if siblings[parent] == nil {
				siblings[parent] = map[string]bool{}
			}
			siblings[parent][libpath.Fold(name)] = true
		})
	}
	clashes := findCaseClashes(entries)
	clashing := map[string]bool{}
	for _, c := range clashes {
		for _, n := range c.Names {
			clashing[libpath.Fold(joinRel(c.Parent, n))] = true
		}
	}
	var plan []CaseStep
	for _, c := range clashes {
		nested := false
		if c.Parent != "" {
			parts := strings.Split(c.Parent, "/")
			for i := range parts {
				if clashing[libpath.Fold(strings.Join(parts[:i+1], "/"))] {
					nested = true
					break
				}
			}
		}
		if nested {
			continue
		}
		names := slices.Clone(c.Names)
		slices.SortFunc(names, preferName)
		tag := 1
		for _, name := range names[1:] {
			p := joinRel(c.Parent, name)
			var renamed string
			for {
				renamed = withCaseTag(name, dirs[p], tag)
				if !siblings[c.Parent][libpath.Fold(renamed)] {
					break
				}
				tag++
			}
			siblings[c.Parent][libpath.Fold(renamed)] = true
			plan = append(plan, CaseStep{Op: CaseRename, Path: p, NewPath: joinRel(c.Parent, renamed)})
			tag++
		}
	}
	slices.SortFunc(plan, func(a, b CaseStep) int {
		return cmpSteps(a, b)
	})
	return plan
}

func cmpSteps(a, b CaseStep) int {
	return slices.Compare([]string{a.Op, a.Path, a.NewPath}, []string{b.Op, b.Path, b.NewPath})
}

// planCaseMerges is Phase B: the steps, in order, that merge each "[case-merge]" sibling into its
// partner. It refuses while names still clash (ErrCaseClashesLeft).
func planCaseMerges(entries []CaseEntry) ([]CaseStep, error) {
	return planCaseMergesWith(entries, caseLook{})
}

// caseLook is what Phase B may look at on disk, beyond the listing (either may be nil).
type caseLook struct {
	// alike says whether the File other may be a Duplicate of keep (the same size): a quick look
	// at the two Files. When it says no, the clash is the admin's to decide. The job still
	// confirms a Duplicate byte-identical before it moves it.
	alike func(keep, other string) bool
	// taken says whether something is at the path in Quarantine already, in any letter case.
	taken func(rel string) bool
}

// planCaseMergesWith is planCaseMerges, looking at the disk through look.
func planCaseMergesWith(entries []CaseEntry, look caseLook) ([]CaseStep, error) {
	if clashes := findCaseClashes(entries); len(clashes) > 0 {
		return nil, fmt.Errorf("%w: %d sets (e.g. %v in %q)", ErrCaseClashesLeft, len(clashes), clashes[0].Names, clashes[0].Parent)
	}
	m := &caseMerger{look: look}
	m.planDir(buildCaseTree(entries), "")
	return m.plan, nil
}

type caseMerger struct {
	plan []CaseStep
	look caseLook
}

func (m *caseMerger) add(s CaseStep) { m.plan = append(m.plan, s) }

func sortedNames(n *caseNode) []string { return slices.Sorted(maps.Keys(n.children)) }

// quarantinePath is where a Duplicate of the File keep goes in Quarantine: named after keep,
// numbered when several copies of it go there, or one is there already.
func (m *caseMerger) quarantinePath(keep string) string {
	planned := map[string]bool{}
	for _, s := range m.plan {
		if s.Op == CaseQuarantine {
			planned[libpath.Fold(s.NewPath)] = true
		}
	}
	taken := func(p string) bool {
		return planned[libpath.Fold(p)] || (m.look.taken != nil && m.look.taken(p))
	}
	stem, ext := splitExt(quarantine.Into(keep))
	p := stem + ext
	for n := 2; taken(p); n++ {
		p = fmt.Sprintf("%s [%d]%s", stem, n, ext)
	}
	return p
}

// resolveFileClash is a File other at opath clashing with keep at kpath: a Duplicate to
// Quarantine when both are Files of the same size, else the admin's to decide.
func (m *caseMerger) resolveFileClash(keep *caseNode, kpath string, other *caseNode, opath string) bool {
	if m.duplicate(keep, kpath, other, opath) {
		m.add(CaseStep{Op: CaseQuarantine, Path: opath, NewPath: m.quarantinePath(kpath), Keep: kpath})
		return true
	}
	m.add(CaseStep{Op: CaseDecide, Path: opath, NewPath: kpath})
	return false
}

// duplicate reports whether other may be a Duplicate of keep: both Files, of the same size, alike
// at a look (see planCaseMergesWith).
func (m *caseMerger) duplicate(keep *caseNode, kpath string, other *caseNode, opath string) bool {
	return !keep.dir && !other.dir && keep.size == other.size && (m.look.alike == nil || m.look.alike(kpath, opath))
}

func (m *caseMerger) mergeInto(winner *caseNode, wpath string, loser *caseNode, lpath string) {
	for _, name := range sortedNames(loser) {
		child := loser.children[name]
		match := ""
		for n := range winner.children {
			if libpath.SameName(n, name) {
				match = n
				break
			}
		}
		switch {
		case match != "" && child.dir && winner.children[match].dir:
			m.mergeInto(winner.children[match], joinRel(wpath, match), child, joinRel(lpath, name))
			if len(child.children) == 0 {
				delete(loser.children, name)
			}
		case match != "":
			if m.resolveFileClash(winner.children[match], joinRel(wpath, match), child, joinRel(lpath, name)) {
				delete(loser.children, name)
			}
		default:
			m.add(CaseStep{Op: CaseMove, Path: joinRel(lpath, name), NewPath: joinRel(wpath, name)})
			winner.children[name] = child
			delete(loser.children, name)
		}
	}
	if len(loser.children) == 0 {
		m.add(CaseStep{Op: CaseRemoveDir, Path: lpath})
	}
}

// mergeRank orders the two sides of a merge, the winner first: more Files, then a name that
// isn't ALL CAPS, then the side not renamed.
func mergeRank(n *caseNode, finalName string, renamed bool) []int {
	b := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	return []int{-n.fileCount(), b(isUpper(finalName)), b(renamed)}
}

func (m *caseMerger) planDir(node *caseNode, p string) {
	groups := map[string][]string{}
	for _, name := range sortedNames(node) {
		key := libpath.Fold(stripCaseTag(name))
		groups[key] = append(groups[key], name)
	}
	ordered := slices.Collect(maps.Values(groups))
	slices.SortFunc(ordered, slices.Compare)
	for _, names := range ordered {
		var tagged []string
		partnerName := ""
		for _, n := range names {
			switch {
			case HasCaseTag(n):
				tagged = append(tagged, n)
			case partnerName == "":
				partnerName = n
			}
		}
		if len(tagged) == 0 {
			continue
		}
		if partnerName == "" {
			// The partner is gone (a Phase B run removed it, then stopped before the renamed side
			// took back its name): the first renamed name takes it back now, and the others merge
			// into it.
			first := tagged[0]
			partnerName = stripCaseTag(first)
			m.add(CaseStep{Op: CaseRename, Path: joinRel(p, first), NewPath: joinRel(p, partnerName)})
			node.children[partnerName] = node.children[first]
			delete(node.children, first)
			tagged = tagged[1:]
		}
		for _, taggedName := range tagged {
			partner, tag := node.children[partnerName], node.children[taggedName]
			partnerPath, taggedPath := joinRel(p, partnerName), joinRel(p, taggedName)
			if !partner.dir || !tag.dir {
				if m.resolveFileClash(partner, partnerPath, tag, taggedPath) {
					delete(node.children, taggedName)
				}
				continue
			}
			if slices.Compare(mergeRank(partner, partnerName, false), mergeRank(tag, stripCaseTag(taggedName), true)) <= 0 {
				m.mergeInto(partner, partnerPath, tag, taggedPath)
				if len(tag.children) == 0 {
					delete(node.children, taggedName)
				}
				continue
			}
			m.mergeInto(tag, taggedPath, partner, partnerPath)
			if len(partner.children) > 0 {
				continue
			}
			delete(node.children, partnerName)
			final := partnerName
			if preferName(stripCaseTag(taggedName), partnerName) < 0 {
				final = stripCaseTag(taggedName)
			}
			m.add(CaseStep{Op: CaseRename, Path: taggedPath, NewPath: joinRel(p, final)})
			node.children[final] = tag
			delete(node.children, taggedName)
			partnerName = final
		}
	}
	for _, name := range sortedNames(node) {
		if child := node.children[name]; child.dir {
			m.planDir(child, joinRel(p, name))
		}
	}
}
