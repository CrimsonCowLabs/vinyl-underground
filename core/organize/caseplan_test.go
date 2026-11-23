package organize

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The case merge through Organize (#169): both phases planned, run as jobs through the real queue,
// journal and write path, on real Files, and undone.
var _ = Describe("Case merge", func() {
	var (
		l       *library
		scanner *tests.MockScanner
		editor  *tagedit.Editor
		q       *editjob.Queue
		o       *Organizer
	)

	BeforeEach(func() {
		l = newLibrary()
		scanner = tests.NewMockScanner()
		editor = tagedit.New(l.ds, scanner)
		q = editjob.New(l.ds, nil, editjob.Kinds(l.ds, editor))
		for kind, k := range JobKinds(l.ds, editor) {
			q.Handle(kind, k)
		}
		o = New(l.ds, q, nil)

		l.track("Music/OutKast/Aquemini/01 Hold On.mp3", model.MediaFile{ID: "hold-on"})
		l.track("Music/Outkast/STANKONIA/01 Intro.mp3", model.MediaFile{ID: "intro"})
		l.track("Music/Outkast/Stankonia/02 Gasoline Dreams.mp3", model.MediaFile{ID: "gasoline"})
		// A File no scan found: no Track, not even its folder's image.
		Expect(os.WriteFile(filepath.Join(l.dir, "Music/Outkast/Stankonia/notes.nfo"), []byte("missed"), 0o644)).To(Succeed())
		l.trackWith("Music/Blondie/Atomic.mp3", "atomic audio", model.MediaFile{ID: "atomic"})
		l.trackWith("Music/Blondie/atomic.mp3", "atomic audio", model.MediaFile{ID: "atomic-dup"})
		l.track("Music/Bangles/a.mp3", model.MediaFile{ID: "bangles-a"})
		l.track("Music/BANGLES/a.mp3", model.MediaFile{ID: "BANGLES-a"})
		l.trackWith("Music/Bangles/b.mp3", "short", model.MediaFile{ID: "bangles-b"})
		l.trackWith("Music/BANGLES/b.mp3", "a longer one", model.MediaFile{ID: "BANGLES-b"})
	})

	runQueue := func(job *model.EditJob) *model.EditJob {
		GinkgoHelper()
		runCtx, cancel := context.WithCancel(l.ctx)
		done := make(chan struct{})
		go func() { q.Run(runCtx); close(done) }()
		defer func() { cancel(); Eventually(done).Should(BeClosed()) }()
		got, err := q.Wait(l.ctx, job.ID, 10*time.Second)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.State).To(Equal(model.EditJobDone))
		return got
	}
	plan := func() *CasePlan {
		GinkgoHelper()
		p, err := o.CasePlan(l.ctx)
		Expect(err).ToNot(HaveOccurred())
		return p
	}
	run := func(p *CasePlan) *model.EditJob {
		GinkgoHelper()
		job, err := o.RunCase(l.ctx, p.Token)
		Expect(err).ToNot(HaveOccurred())
		return runQueue(job)
	}
	undo := func(job *model.EditJob) *model.EditJob {
		GinkgoHelper()
		steps, err := editjob.UndoJobSteps(l.ctx, l.ds, job.ID)
		Expect(err).ToNot(HaveOccurred())
		u, err := q.EnqueueWith(l.ctx, model.EditUndo, editjob.UndoJobArgs(job.ID), steps)
		Expect(err).ToNot(HaveOccurred())
		return runQueue(u)
	}
	paths := func() map[string]string {
		GinkgoHelper()
		mfs, err := l.ds.MediaFile(l.ctx).GetAll()
		Expect(err).ToNot(HaveOccurred())
		res := map[string]string{}
		for _, mf := range mfs {
			res[mf.ID] = mf.Path
		}
		return res
	}
	ops := func(steps []CaseStep) []string {
		var res []string
		for _, s := range steps {
			res = append(res, s.Op+" "+s.Path+" -> "+s.NewPath)
		}
		return res
	}
	original := map[string]string{
		"Music/OutKast/Aquemini/01 Hold On.mp3":          "audio of Music/OutKast/Aquemini/01 Hold On.mp3",
		"Music/Outkast/STANKONIA/01 Intro.mp3":           "audio of Music/Outkast/STANKONIA/01 Intro.mp3",
		"Music/Outkast/Stankonia/02 Gasoline Dreams.mp3": "audio of Music/Outkast/Stankonia/02 Gasoline Dreams.mp3",
		"Music/Outkast/Stankonia/notes.nfo":              "missed",
		"Music/Blondie/Atomic.mp3":                       "atomic audio",
		"Music/Blondie/atomic.mp3":                       "atomic audio",
		"Music/Bangles/a.mp3":                            "audio of Music/Bangles/a.mp3",
		"Music/BANGLES/a.mp3":                            "audio of Music/BANGLES/a.mp3",
		"Music/Bangles/b.mp3":                            "short",
		"Music/BANGLES/b.mp3":                            "a longer one",
	}

	It("lists every set of case-colliding siblings, nested ones once their parents are renamed, and merges them in two phases", func() {
		before := paths()

		By("Phase A, first pass: the outermost clashes get temporary names")
		p := plan()
		Expect(p.Phase).To(Equal(PhaseRename))
		Expect(p.Clashes).To(ConsistOf(
			CaseClash{LibraryID: 1, Parent: "Music", Names: []string{"BANGLES", "Bangles"}},
			CaseClash{LibraryID: 1, Parent: "Music", Names: []string{"OutKast", "Outkast"}},
			CaseClash{LibraryID: 1, Parent: "Music/Blondie", Names: []string{"Atomic.mp3", "atomic.mp3"}},
		))
		Expect(ops(p.Steps)).To(Equal([]string{
			"rename Music/BANGLES -> Music/BANGLES [case-merge]",
			"rename Music/Blondie/atomic.mp3 -> Music/Blondie/atomic [case-merge].mp3",
			"rename Music/Outkast -> Music/Outkast [case-merge]",
		}))
		job := run(p)
		Expect(job.Kind).To(Equal(model.EditCaseRename))
		Expect(job.Skipped).To(BeZero())
		Expect(l.files()).To(HaveKeyWithValue("Music/Outkast [case-merge]/Stankonia/notes.nfo", "missed"))
		Expect(paths()).To(HaveKeyWithValue("intro", "Music/Outkast [case-merge]/STANKONIA/01 Intro.mp3"))
		Expect(paths()).To(HaveKeyWithValue("atomic-dup", "Music/Blondie/atomic [case-merge].mp3"))
		Expect(scanner.GetScanFoldersCalls()).ToNot(BeEmpty(), "the job rescans what it renamed")

		By("Phase A, second pass: the clash nested in a renamed folder")
		p = plan()
		Expect(p.Phase).To(Equal(PhaseRename))
		Expect(p.Clashes).To(ConsistOf(CaseClash{LibraryID: 1, Parent: "Music/Outkast [case-merge]", Names: []string{"STANKONIA", "Stankonia"}}))
		Expect(ops(p.Steps)).To(Equal([]string{
			"rename Music/Outkast [case-merge]/STANKONIA -> Music/Outkast [case-merge]/STANKONIA [case-merge]",
		}))
		run(p)

		By("Phase B: every name is unique; each renamed sibling merges into its partner")
		p = plan()
		Expect(p.Phase).To(Equal(PhaseMerge))
		Expect(p.Clashes).To(BeEmpty())
		Expect(ops(p.Steps)).To(Equal([]string{
			"move Music/OutKast/Aquemini -> Music/Outkast [case-merge]/Aquemini",
			"rmdir Music/OutKast -> ",
			"rename Music/Outkast [case-merge] -> Music/OutKast",
			"quarantine Music/Blondie/atomic [case-merge].mp3 -> _Quarantine/Music/Blondie/Atomic.mp3",
			"move Music/OutKast/STANKONIA [case-merge]/01 Intro.mp3 -> Music/OutKast/Stankonia/01 Intro.mp3",
			"rmdir Music/OutKast/STANKONIA [case-merge] -> ",
		}))
		// Same size, but they differ: the admin's to decide, as is a File of another size.
		Expect(ops(p.Decide)).To(Equal([]string{
			"decide Music/BANGLES [case-merge]/a.mp3 -> Music/Bangles/a.mp3",
			"decide Music/BANGLES [case-merge]/b.mp3 -> Music/Bangles/b.mp3",
		}))
		job = run(p)
		Expect(job.Kind).To(Equal(model.EditCaseMerge))
		Expect(job.Skipped).To(BeZero())
		Expect(l.files()).To(Equal(map[string]string{
			"Music/OutKast/Aquemini/01 Hold On.mp3":          "audio of Music/OutKast/Aquemini/01 Hold On.mp3",
			"Music/OutKast/Stankonia/01 Intro.mp3":           "audio of Music/Outkast/STANKONIA/01 Intro.mp3",
			"Music/OutKast/Stankonia/02 Gasoline Dreams.mp3": "audio of Music/Outkast/Stankonia/02 Gasoline Dreams.mp3",
			"Music/OutKast/Stankonia/notes.nfo":              "missed",
			"Music/Blondie/Atomic.mp3":                       "atomic audio",
			"_Quarantine/Music/Blondie/Atomic.mp3":           "atomic audio",
			"_Quarantine/.ndignore":                          "",
			"Music/Bangles/a.mp3":                            "audio of Music/Bangles/a.mp3",
			"Music/BANGLES [case-merge]/a.mp3":               "audio of Music/BANGLES/a.mp3",
			"Music/Bangles/b.mp3":                            "short",
			"Music/BANGLES [case-merge]/b.mp3":               "a longer one",
		}))
		// Every Track kept its ID, at its File's place (the Duplicate's, kept for a restore).
		Expect(paths()).To(Equal(map[string]string{
			"hold-on":    "Music/OutKast/Aquemini/01 Hold On.mp3",
			"intro":      "Music/OutKast/Stankonia/01 Intro.mp3",
			"gasoline":   "Music/OutKast/Stankonia/02 Gasoline Dreams.mp3",
			"atomic":     "Music/Blondie/Atomic.mp3",
			"atomic-dup": "Music/Blondie/atomic [case-merge].mp3",
			"bangles-a":  "Music/Bangles/a.mp3",
			"BANGLES-a":  "Music/BANGLES [case-merge]/a.mp3",
			"bangles-b":  "Music/Bangles/b.mp3",
			"BANGLES-b":  "Music/BANGLES [case-merge]/b.mp3",
		}))
		Expect(paths()).To(HaveLen(len(before)))
		dup, err := l.ds.MediaFile(l.ctx).Get("atomic-dup")
		Expect(err).ToNot(HaveOccurred())
		Expect(dup.Quarantined).To(BeTrue())

		By("Then nothing is left to do, but the conflicts for the admin")
		p = plan()
		Expect(p.Phase).To(BeEmpty())
		Expect(p.Steps).To(BeEmpty())
		Expect(p.Decide).To(HaveLen(2))
		_, err = o.RunCase(l.ctx, p.Token)
		Expect(err).To(MatchError(ErrNoCaseMerge))
	})

	It("undoes both phases from the Edit history: every File and Track back where it was", func() {
		passes := []*model.EditJob{run(plan()), run(plan()), run(plan())}
		Expect(passes[2].Kind).To(Equal(model.EditCaseMerge))

		for i := len(passes) - 1; i >= 0; i-- {
			got := undo(passes[i])
			Expect(got.Skipped).To(BeZero(), "undo of pass %d: %v", i, got.Steps)
		}
		files := l.files()
		delete(files, "_Quarantine/.ndignore")
		Expect(files).To(Equal(original))
		Expect(paths()).To(Equal(map[string]string{
			"hold-on":    "Music/OutKast/Aquemini/01 Hold On.mp3",
			"intro":      "Music/Outkast/STANKONIA/01 Intro.mp3",
			"gasoline":   "Music/Outkast/Stankonia/02 Gasoline Dreams.mp3",
			"atomic":     "Music/Blondie/Atomic.mp3",
			"atomic-dup": "Music/Blondie/atomic.mp3",
			"bangles-a":  "Music/Bangles/a.mp3",
			"BANGLES-a":  "Music/BANGLES/a.mp3",
			"bangles-b":  "Music/Bangles/b.mp3",
			"BANGLES-b":  "Music/BANGLES/b.mp3",
		}))
		dup, err := l.ds.MediaFile(l.ctx).Get("atomic-dup")
		Expect(err).ToNot(HaveOccurred())
		Expect(dup.Quarantined).To(BeFalse())
	})

	It("keeps the Track of a Duplicate sent to Quarantine when the renamed side wins and takes back the path it left", func() {
		l = newLibrary()
		l.trackWith("Music/Dire Straits/Brothers/01.mp3", "so far away", model.MediaFile{ID: "so-far"})
		l.trackWith("Music/DIRE STRAITS [case-merge]/Brothers/01.mp3", "so far away", model.MediaFile{ID: "so-far-2"})
		l.track("Music/DIRE STRAITS [case-merge]/Brothers/02.mp3", model.MediaFile{ID: "money"})
		before := l.files()

		p := plan()
		Expect(ops(p.Steps)).To(Equal([]string{
			"quarantine Music/Dire Straits/Brothers/01.mp3 -> _Quarantine/Music/DIRE STRAITS [case-merge]/Brothers/01.mp3",
			"rmdir Music/Dire Straits/Brothers -> ",
			"rmdir Music/Dire Straits -> ",
			"rename Music/DIRE STRAITS [case-merge] -> Music/Dire Straits",
		}))
		job := run(p)
		Expect(job.Skipped).To(BeZero())
		Expect(paths()).To(Equal(map[string]string{
			"so-far":   "Music/Dire Straits/Brothers/01.mp3",
			"so-far-2": "Music/Dire Straits/Brothers/01.mp3",
			"money":    "Music/Dire Straits/Brothers/02.mp3",
		}))
		dup, err := l.ds.MediaFile(l.ctx).Get("so-far")
		Expect(err).ToNot(HaveOccurred())
		Expect(dup.Quarantined).To(BeTrue(), "kept for a restore")

		undo(job)
		files := l.files()
		delete(files, "_Quarantine/.ndignore")
		Expect(files).To(Equal(before))
		Expect(paths()).To(Equal(map[string]string{
			"so-far":   "Music/Dire Straits/Brothers/01.mp3",
			"so-far-2": "Music/DIRE STRAITS [case-merge]/Brothers/01.mp3",
			"money":    "Music/DIRE STRAITS [case-merge]/Brothers/02.mp3",
		}))
	})

	It("refuses a plan that changed since it was reviewed, and queues nothing", func() {
		p := plan()
		Expect(os.Rename(filepath.Join(l.dir, "Music/BANGLES"), filepath.Join(l.dir, "Music/BANGLES 2"))).To(Succeed())
		_, err := o.RunCase(l.ctx, p.Token)
		Expect(err).To(MatchError(ErrPlanChanged))
		jobs, err := l.ds.EditJob(l.ctx).GetAll(0)
		Expect(err).ToNot(HaveOccurred())
		Expect(jobs).To(BeEmpty())
	})

	It("skips a Duplicate whose bytes turn out to differ, and leaves it for the admin", func() {
		run(plan())
		run(plan())
		// The two differ only in the middle, where the plan doesn't look.
		data := make([]byte, 3*sampleBytes)
		Expect(os.WriteFile(filepath.Join(l.dir, "Music/Blondie/Atomic.mp3"), data, 0o644)).To(Succeed())
		other := slices.Clone(data)
		other[len(other)/2] = 1
		Expect(os.WriteFile(filepath.Join(l.dir, "Music/Blondie/atomic [case-merge].mp3"), other, 0o644)).To(Succeed())
		p := plan()
		Expect(ops(p.Steps)).To(ContainElement("quarantine Music/Blondie/atomic [case-merge].mp3 -> _Quarantine/Music/Blondie/Atomic.mp3"))
		job := run(p)
		var codes []string
		for _, s := range job.Steps {
			if s.State == model.EditStepSkipped {
				codes = append(codes, s.Code+" "+s.Path)
			}
		}
		Expect(codes).To(ContainElement("notDuplicate Music/Blondie/atomic [case-merge].mp3"))
		Expect(l.files()).To(HaveKey("Music/Blondie/atomic [case-merge].mp3"))

		// The next plan doesn't try again: the clash is the admin's to decide.
		p = plan()
		Expect(ops(p.Steps)).ToNot(ContainElement(ContainSubstring("atomic [case-merge].mp3")))
		Expect(ops(p.Decide)).To(ContainElement("decide Music/Blondie/atomic [case-merge].mp3 -> Music/Blondie/Atomic.mp3"))
	})
})
