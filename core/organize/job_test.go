package organize

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"time"

	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/librarymanager"
	"github.com/navidrome/navidrome/core/safefile"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The Organize job through the real queue, journal and write path, on real Files.
var _ = Describe("Run", func() {
	var (
		l       *library
		scanner *tests.MockScanner
		editor  *tagedit.Editor
		q       *editjob.Queue
		o       *Organizer
	)
	everything := Scope{Selection: librarymanager.Selection{Filter: map[string]any{}}}
	song := func(title string, track int) model.MediaFile {
		return model.MediaFile{Title: title, Artist: "Band", AlbumArtist: "Band", Album: "Record", Year: 1999, TrackNumber: track}
	}

	BeforeEach(func() {
		l = newLibrary()
		scanner = tests.NewMockScanner()
		editor = tagedit.New(l.ds, scanner)
		q = editjob.New(l.ds, nil, editjob.Kinds(l.ds, editor))
		for kind, k := range JobKinds(l.ds, editor) {
			q.Handle(kind, k)
		}
		o = New(l.ds, q, nil)

		l.track("iTunes_Library/Band/Record/a.mp3", song("One", 1))
		l.track("iTunes_Library/Band/Record/b.mp3", song("Two", 2))
		l.file("iTunes_Library/Band/Record/cover.jpg", "image")
		l.file("iTunes_Library/Band/Record/notes.nfo", "notes")
		l.file("iTunes_Library/keep.txt", "not the album's")
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
	organize := func() *model.EditJob {
		GinkgoHelper()
		p, err := o.Plan(l.ctx, everything)
		Expect(err).ToNot(HaveOccurred())
		job, err := o.Run(l.ctx, everything, p.Token)
		Expect(err).ToNot(HaveOccurred())
		return job
	}
	organized := map[string]string{
		"Band/Record (1999)/01 One.mp3": "audio of iTunes_Library/Band/Record/a.mp3",
		"Band/Record (1999)/02 Two.mp3": "audio of iTunes_Library/Band/Record/b.mp3",
		"Band/Record (1999)/cover.jpg":  "image",
		"Band/Record (1999)/notes.nfo":  "notes",
		"iTunes_Library/keep.txt":       "not the album's",
	}
	original := map[string]string{
		"iTunes_Library/Band/Record/a.mp3":     "audio of iTunes_Library/Band/Record/a.mp3",
		"iTunes_Library/Band/Record/b.mp3":     "audio of iTunes_Library/Band/Record/b.mp3",
		"iTunes_Library/Band/Record/cover.jpg": "image",
		"iTunes_Library/Band/Record/notes.nfo": "notes",
		"iTunes_Library/keep.txt":              "not the album's",
	}
	trackPath := func(id string) string {
		GinkgoHelper()
		mf, err := l.ds.MediaFile(l.ctx).Get(id)
		Expect(err).ToNot(HaveOccurred())
		return mf.Path
	}
	ids := func() []string {
		GinkgoHelper()
		mfs, err := l.ds.MediaFile(l.ctx).GetAll()
		Expect(err).ToNot(HaveOccurred())
		var res []string
		for _, mf := range mfs {
			res = append(res, mf.ID)
		}
		return res
	}

	It("moves every File of the plan as one job, keeps the Tracks' IDs, removes the emptied folders and rescans", func() {
		before := ids()
		job := organize()
		Expect(job.Kind).To(Equal(model.EditOrganize))
		Expect(job.Total).To(Equal(2), "one step per Track; the folder's other Files go with it")

		got := runQueue(job)
		Expect(got.Skipped).To(BeZero())
		Expect(l.files()).To(Equal(organized))
		Expect(l.dirs()).To(ConsistOf("Band", "Band/Record (1999)", "iTunes_Library"))
		Expect(ids()).To(ConsistOf(before))
		for _, s := range got.Steps {
			if s.MediaFileID != "" {
				Expect(trackPath(s.MediaFileID)).To(HavePrefix("Band/Record (1999)/"))
			}
		}

		// Each move is journaled, old path to new path.
		entries, err := l.ds.EditJournal(l.ctx).ForJob(job.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(entries).To(HaveLen(4))
		for _, e := range entries {
			Expect(e.Kind).To(Equal(model.EditMove))
			Expect(original).To(HaveKey(e.Path))
			Expect(organized).To(HaveKey(e.NewPath))
		}

		calls := scanner.GetScanFoldersCalls()
		Expect(calls).To(HaveLen(1))
		Expect(calls[0].Targets).To(ConsistOf(
			model.ScanTarget{LibraryID: 1, FolderPath: "Band/Record (1999)"},
			model.ScanTarget{LibraryID: 1, FolderPath: "iTunes_Library"},
		))
	})

	It("undoes the whole job: every File back at its original path, the Tracks too, the new folders gone", func() {
		job := organize()
		runQueue(job)

		steps, err := editjob.UndoJobSteps(l.ctx, l.ds, job.ID)
		Expect(err).ToNot(HaveOccurred())
		undo, err := q.EnqueueWith(l.ctx, model.EditUndo, editjob.UndoJobArgs(job.ID), steps)
		Expect(err).ToNot(HaveOccurred())
		got := runQueue(undo)
		Expect(got.Skipped).To(BeZero())
		Expect(l.files()).To(Equal(original))
		Expect(l.dirs()).To(ConsistOf("iTunes_Library", "iTunes_Library/Band", "iTunes_Library/Band/Record"))
		for _, s := range got.Steps {
			if s.MediaFileID != "" {
				Expect(trackPath(s.MediaFileID)).To(HavePrefix("iTunes_Library/Band/Record/"))
			}
		}
	})

	It("refuses a plan that changed since it was reviewed, and queues nothing", func() {
		p, err := o.Plan(l.ctx, everything)
		Expect(err).ToNot(HaveOccurred())
		l.track("iTunes_Library/Band/Record/c.mp3", song("Three", 3))
		_, err = o.Run(l.ctx, everything, p.Token)
		Expect(err).To(MatchError(ErrPlanChanged))
		Expect(Code(err)).To(Equal("planChanged"))
		jobs, err := l.ds.EditJob(l.ctx).GetAll(0)
		Expect(err).ToNot(HaveOccurred())
		Expect(jobs).To(BeEmpty())
	})

	It("refuses a plan with nothing to move", func() {
		_, err := o.Run(l.ctx, Scope{Selection: librarymanager.Selection{IDs: []string{"nope"}}}, token(nil))
		Expect(err).To(MatchError(ErrNothingToMove))
	})

	It("skips a File changed on disk since it was planned, and moves the rest", func() {
		job := organize()
		Expect(os.WriteFile(filepath.Join(l.dir, "iTunes_Library/Band/Record/b.mp3"), []byte("changed"), 0o644)).To(Succeed())
		got := runQueue(job)
		Expect(got.Skipped).To(Equal(1))
		Expect(l.files()).To(HaveKeyWithValue("iTunes_Library/Band/Record/b.mp3", "changed"))
		Expect(l.files()).To(HaveKey("Band/Record (1999)/01 One.mp3"))
	})

	It("never sweeps a Track's old copy that its move couldn't remove into the new folder", func() {
		job := &model.EditJob{ID: "job-x", Kind: model.EditOrganize}
		// The Track moved, but its old copy is still there, and its entry pending (Settle removes it).
		mf, err := l.ds.MediaFile(l.ctx).GetAll()
		Expect(err).ToNot(HaveOccurred())
		for _, m := range mf {
			Expect(l.ds.MediaFile(l.ctx).Relocate(m.ID, 1, "Band/Record (1999)/"+filepath.Base(m.Path), "f")).To(Succeed())
			Expect(l.ds.EditJournal(l.ctx).Put(&model.EditEntry{JobID: job.ID, Kind: model.EditMove, State: model.EditPending,
				LibraryID: 1, MediaFileID: m.ID, Path: m.Path, NewPath: "Band/Record (1999)/" + filepath.Base(m.Path)})).To(Succeed())
		}
		k := jobKind{ds: l.ds, editor: editor}
		Expect(k.sweep(l.ctx, job, FolderMove{LibraryID: 1, Path: "iTunes_Library/Band/Record", NewPath: "Band/Record (1999)"})).To(Succeed())
		Expect(l.files()).To(HaveKey("iTunes_Library/Band/Record/a.mp3"))
		Expect(l.files()).To(HaveKey("iTunes_Library/Band/Record/b.mp3"))
		Expect(l.files()).To(HaveKey("Band/Record (1999)/cover.jpg"), "the album's other Files still go")
		Expect(l.files()).ToNot(HaveKey("Band/Record (1999)/a.mp3"))
	})

	It("carries on after a restart in the middle of a move: the move is finished, then the rest", func() {
		job := organize()
		steps, err := l.ds.EditJob(l.ctx).Steps(job.ID)
		Expect(err).ToNot(HaveOccurred())
		first := steps[0]
		m, err := editjob.ReadMove(&first)
		Expect(err).ToNot(HaveOccurred())

		// The stop came once the first File's checked copy was placed, before its old copy went.
		Expect(os.MkdirAll(filepath.Join(l.dir, "Band/Record (1999)"), 0o755)).To(Succeed())
		data, err := os.ReadFile(filepath.Join(l.dir, m.Path))
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(l.dir, m.NewPath), data, 0o644)).To(Succeed())
		sum, _, err := safefile.Hash(safefile.OS{}, filepath.Join(l.dir, m.Path))
		Expect(err).ToNot(HaveOccurred())
		Expect(l.ds.EditJournal(l.ctx).Put(&model.EditEntry{JobID: job.ID, Kind: model.EditMove, State: model.EditPending,
			LibraryID: 1, MediaFileID: m.TrackID, Path: m.Path, NewPath: m.NewPath, CopySum: hex.EncodeToString(sum)})).To(Succeed())
		job.State = model.EditJobRunning
		Expect(l.ds.EditJob(l.ctx).Put(job)).To(Succeed())
		first.State = model.EditStepRunning
		Expect(l.ds.EditJob(l.ctx).PutStep(&first)).To(Succeed())

		// The start: the journal is settled, then the queue carries on.
		editor.Settle(l.ctx)
		got := runQueue(job)
		Expect(got.Skipped).To(BeZero())
		Expect(got.Steps[0].State).To(Equal(model.EditStepWritten))
		Expect(l.files()).To(Equal(organized))
		Expect(trackPath(m.TrackID)).To(Equal(m.NewPath))
	})
})
