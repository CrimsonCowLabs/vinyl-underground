package organize

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The case-merge planner, ported with the test cases of contrib/library-cleanup's casemerge.py.
var _ = Describe("Case merge planner", func() {
	d := func(p string) CaseEntry { return CaseEntry{Path: p, Dir: true} }
	f := func(p string, size ...int64) CaseEntry {
		e := CaseEntry{Path: p, Size: 100}
		if len(size) > 0 {
			e.Size = size[0]
		}
		return e
	}
	step := func(op, p, newPath string, keep ...string) CaseStep {
		s := CaseStep{Op: op, Path: p, NewPath: newPath}
		if len(keep) > 0 {
			s.Keep = keep[0]
		}
		return s
	}

	Describe("findCaseClashes", func() {
		It("finds sibling folders differing only by case", func() {
			scan := []CaseEntry{d("Music"), d("Music/BLONDIE"), d("Music/Blondie"), d("Music/Queen")}
			Expect(findCaseClashes(scan)).To(Equal([]CaseClash{{Parent: "Music", Names: []string{"BLONDIE", "Blondie"}}}))
		})

		It("finds Files, and folders the listing only has as a File's folder", func() {
			scan := []CaseEntry{f("Music/OutKast/a.mp3"), f("Music/Outkast/b.mp3"), f("Music/Blondie/Atomic.mp3"), f("Music/Blondie/atomic.mp3")}
			Expect(findCaseClashes(scan)).To(Equal([]CaseClash{
				{Parent: "Music", Names: []string{"OutKast", "Outkast"}},
				{Parent: "Music/Blondie", Names: []string{"Atomic.mp3", "atomic.mp3"}},
			}))
		})

		It("finds none once the names are unique", func() {
			scan := []CaseEntry{f("Music/Outkast/a.mp3"), f("Music/Outkast [case-merge]/b.mp3")}
			Expect(findCaseClashes(scan)).To(BeEmpty())
		})
	})

	Describe("planDisambiguation (Phase A)", func() {
		It("renames one side of each outermost clash and leaves nested clashes for later", func() {
			scan := []CaseEntry{
				f("Music/OutKast/Aquemini/01 Hold On.mp3"),
				f("Music/Outkast/STANKONIA/01 Intro.mp3"),
				f("Music/Outkast/Stankonia/02 Gasoline Dreams.mp3"),
				f("Music/Blondie/Atomic.mp3"),
				f("Music/Blondie/atomic.mp3"),
			}
			Expect(planDisambiguation(scan)).To(Equal([]CaseStep{
				step(CaseRename, "Music/Blondie/atomic.mp3", "Music/Blondie/atomic [case-merge].mp3"),
				step(CaseRename, "Music/Outkast", "Music/Outkast [case-merge]"),
			}))
		})

		It("renames the nested clash on the next pass, once its parents are unique", func() {
			scan := []CaseEntry{
				f("Music/OutKast/Aquemini/01 Hold On.mp3"),
				f("Music/Outkast [case-merge]/STANKONIA/01 Intro.mp3"),
				f("Music/Outkast [case-merge]/Stankonia/02 Gasoline Dreams.mp3"),
			}
			Expect(planDisambiguation(scan)).To(Equal([]CaseStep{
				step(CaseRename, "Music/Outkast [case-merge]/STANKONIA", "Music/Outkast [case-merge]/STANKONIA [case-merge]"),
			}))
		})

		It("keeps the name that isn't ALL CAPS, and numbers the tags of three or more", func() {
			scan := []CaseEntry{d("ATB"), d("Atb"), d("atb"), d("atb [case-merge]")}
			Expect(planDisambiguation(scan)).To(Equal([]CaseStep{
				step(CaseRename, "ATB", "ATB [case-merge 3]"),
				step(CaseRename, "atb", "atb [case-merge 2]"),
			}))
		})

		It("plans nothing once no names clash", func() {
			Expect(planDisambiguation([]CaseEntry{f("Music/Outkast/a.mp3")})).To(BeEmpty())
		})
	})

	Describe("planCaseMerges (Phase B)", func() {
		It("moves the smaller side's contents into the side with more Files", func() {
			scan := []CaseEntry{
				d("Music"),
				d("Music/BLONDIE [case-merge]"),
				f("Music/BLONDIE [case-merge]/Call Me.mp3"),
				d("Music/Blondie"),
				f("Music/Blondie/Atomic.mp3"),
				f("Music/Blondie/Maria.mp3"),
			}
			Expect(planCaseMerges(scan)).To(Equal([]CaseStep{
				step(CaseMove, "Music/BLONDIE [case-merge]/Call Me.mp3", "Music/Blondie/Call Me.mp3"),
				step(CaseRemoveDir, "Music/BLONDIE [case-merge]", ""),
			}))
		})

		It("when the renamed side wins, it takes back the preferred name", func() {
			scan := []CaseEntry{
				f("Music/BANGLES/Eternal Flame.mp3"),
				f("Music/Bangles [case-merge]/Manic Monday.mp3"),
				f("Music/Bangles [case-merge]/Walk Like an Egyptian.mp3"),
			}
			Expect(planCaseMerges(scan)).To(Equal([]CaseStep{
				step(CaseMove, "Music/BANGLES/Eternal Flame.mp3", "Music/Bangles [case-merge]/Eternal Flame.mp3"),
				step(CaseRemoveDir, "Music/BANGLES", ""),
				step(CaseRename, "Music/Bangles [case-merge]", "Music/Bangles"),
			}))
		})

		It("on a tie of Files, the side whose name isn't ALL CAPS wins (and takes back the preferred name)", func() {
			scan := []CaseEntry{
				f("Music/BANGLES/Eternal Flame.mp3"),
				f("Music/Bangles [case-merge]/Manic Monday.mp3"),
			}
			Expect(planCaseMerges(scan)).To(Equal([]CaseStep{
				step(CaseMove, "Music/BANGLES/Eternal Flame.mp3", "Music/Bangles [case-merge]/Eternal Flame.mp3"),
				step(CaseRemoveDir, "Music/BANGLES", ""),
				step(CaseRename, "Music/Bangles [case-merge]", "Music/Bangles"),
			}))
		})

		It("moves a whole folder the other side doesn't have", func() {
			scan := []CaseEntry{
				f("Music/Outkast/Stankonia/01 Intro.mp3"),
				f("Music/Outkast/Stankonia/02 Gasoline Dreams.mp3"),
				f("Music/OutKast [case-merge]/Aquemini/01 Hold On.mp3"),
			}
			Expect(planCaseMerges(scan)).To(Equal([]CaseStep{
				step(CaseMove, "Music/OutKast [case-merge]/Aquemini", "Music/Outkast/Aquemini"),
				step(CaseRemoveDir, "Music/OutKast [case-merge]", ""),
			}))
		})

		It("merges nested folders that clash once their parents are merged", func() {
			scan := []CaseEntry{
				f("Music/Outkast/Stankonia/01 Intro.mp3"),
				f("Music/Outkast/Stankonia/02 Gasoline Dreams.mp3"),
				f("Music/OutKast [case-merge]/STANKONIA/03 So Fresh, So Clean.mp3"),
			}
			Expect(planCaseMerges(scan)).To(Equal([]CaseStep{
				step(CaseMove, "Music/OutKast [case-merge]/STANKONIA/03 So Fresh, So Clean.mp3", "Music/Outkast/Stankonia/03 So Fresh, So Clean.mp3"),
				step(CaseRemoveDir, "Music/OutKast [case-merge]/STANKONIA", ""),
				step(CaseRemoveDir, "Music/OutKast [case-merge]", ""),
			}))
		})

		It("sends a same-named File of the same size to Quarantine, named after the File it duplicates", func() {
			scan := []CaseEntry{
				f("Music/Blondie/Atomic.mp3", 500),
				f("Music/Blondie/Maria.mp3", 400),
				f("Music/BLONDIE [case-merge]/atomic.mp3", 500),
			}
			Expect(planCaseMerges(scan)).To(Equal([]CaseStep{
				step(CaseQuarantine, "Music/BLONDIE [case-merge]/atomic.mp3", "_Quarantine/Music/Blondie/Atomic.mp3", "Music/Blondie/Atomic.mp3"),
				step(CaseRemoveDir, "Music/BLONDIE [case-merge]", ""),
			}))
		})

		It("leaves a same-named File of a different size for the admin to decide", func() {
			scan := []CaseEntry{
				f("Music/Blondie/Atomic.mp3", 500),
				f("Music/Blondie/Maria.mp3", 400),
				f("Music/BLONDIE [case-merge]/Atomic.mp3", 900),
				f("Music/BLONDIE [case-merge]/Call Me.mp3", 300),
			}
			Expect(planCaseMerges(scan)).To(Equal([]CaseStep{
				step(CaseDecide, "Music/BLONDIE [case-merge]/Atomic.mp3", "Music/Blondie/Atomic.mp3"),
				step(CaseMove, "Music/BLONDIE [case-merge]/Call Me.mp3", "Music/Blondie/Call Me.mp3"),
			}))
		})

		It("leaves a same-named File of the same size for the admin when a look at the two tells them apart", func() {
			scan := []CaseEntry{
				f("Music/Blondie/Atomic.mp3", 500),
				f("Music/Blondie/Maria.mp3", 400),
				f("Music/BLONDIE [case-merge]/atomic.mp3", 500),
			}
			differ := func(keep, other string) bool {
				Expect(keep).To(Equal("Music/Blondie/Atomic.mp3"))
				Expect(other).To(Equal("Music/BLONDIE [case-merge]/atomic.mp3"))
				return false
			}
			Expect(planCaseMergesWith(scan, differ)).To(Equal([]CaseStep{
				step(CaseDecide, "Music/BLONDIE [case-merge]/atomic.mp3", "Music/Blondie/Atomic.mp3"),
			}))
		})

		It("quarantines the renamed one of a File pair of the same size", func() {
			scan := []CaseEntry{f("Music/Blondie/Atomic.mp3", 500), f("Music/Blondie/atomic [case-merge].mp3", 500)}
			Expect(planCaseMerges(scan)).To(Equal([]CaseStep{
				step(CaseQuarantine, "Music/Blondie/atomic [case-merge].mp3", "_Quarantine/Music/Blondie/Atomic.mp3", "Music/Blondie/Atomic.mp3"),
			}))
		})

		It("gives several copies of one File distinct Quarantine paths", func() {
			scan := []CaseEntry{
				f("Music/Blondie/Atomic.mp3", 500),
				f("Music/Blondie/atomic [case-merge].mp3", 500),
				f("Music/Blondie/ATOMIC [case-merge 2].mp3", 500),
			}
			steps, err := planCaseMerges(scan)
			Expect(err).ToNot(HaveOccurred())
			var dests []string
			for _, s := range steps {
				dests = append(dests, s.NewPath)
			}
			Expect(dests).To(Equal([]string{"_Quarantine/Music/Blondie/Atomic.mp3", "_Quarantine/Music/Blondie/Atomic [2].mp3"}))
		})

		It("refuses to plan while names still clash", func() {
			scan := []CaseEntry{f("Music/ATB/a.mp3"), f("Music/Atb/b.mp3"), f("Music/atb [case-merge]/c.mp3")}
			_, err := planCaseMerges(scan)
			Expect(err).To(MatchError(ErrCaseClashesLeft))
		})
	})
})
