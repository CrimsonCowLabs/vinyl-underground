package editjob

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/persistence"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Quarantine jobs (#168) through the queue, on a real database and real Files.
var _ = Describe("Quarantine jobs", func() {
	var (
		ctx    context.Context
		ds     model.DataStore
		editor *tagedit.Editor
		q      *Queue
		libDir string
	)
	paths := []string{"Band/Record/01 One.mp3", "Band/Record/02 Two.mp3"}
	abs := func(rel string) string { return filepath.Join(libDir, filepath.FromSlash(rel)) }
	write := func(rel, content string) {
		GinkgoHelper()
		Expect(os.MkdirAll(filepath.Dir(abs(rel)), 0o755)).To(Succeed())
		Expect(os.WriteFile(abs(rel), []byte(content), 0o644)).To(Succeed())
	}

	BeforeEach(func() {
		ctx = request.WithUser(context.Background(), model.User{ID: "admin-1", UserName: "admin", IsAdmin: true})
		ds = persistence.New(db.Db())
		for _, table := range []string{"media_file", "edit_journal", "edit_job_step", "edit_job"} {
			_, err := db.Db().Exec("delete from " + table)
			Expect(err).ToNot(HaveOccurred())
		}
		libDir = GinkgoT().TempDir()
		_, err := db.Db().Exec("update library set path = ? where id = 1", libDir)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(configtest.SetupConfig())
		conf.Server.DataFolder = conf.NewDir(GinkgoT().TempDir())
		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Name: "NAS", Writable: true}}})
		DeferCleanup(func() { core.SetLibraryEditing(core.LibraryEditing{}) })
		for i, p := range paths {
			write(p, "song")
			mf := model.MediaFile{ID: "track-" + string(rune('1'+i)), LibraryID: 1, Path: p, Suffix: "mp3", Participants: model.Participants{}}
			Expect(ds.MediaFile(ctx).Put(&mf)).To(Succeed())
		}
		editor = tagedit.New(ds, nil)
		q = New(ds, nil, Kinds(ds, editor))
	})

	run := func(job *model.EditJob) *model.EditJob {
		GinkgoHelper()
		runCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() { q.Run(runCtx); close(done) }()
		defer func() { cancel(); Eventually(done).Should(BeClosed()) }()
		got, err := q.Wait(ctx, job.ID, 10*time.Second)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.State).To(Equal(model.EditJobDone))
		return got
	}
	quarantineJob := func() *model.EditJob {
		GinkgoHelper()
		job, err := q.Enqueue(ctx, model.EditQuarantine, []model.EditJobStep{QuarantineStep("track-1"), QuarantineStep("track-2")})
		Expect(err).ToNot(HaveOccurred())
		return run(job)
	}

	It("sends the Tracks to Quarantine as one job, and its undo puts every File back", func() {
		job := quarantineJob()
		Expect(job.Skipped).To(Equal(0))
		for _, p := range paths {
			Expect(abs("_Quarantine/" + p)).To(BeARegularFile())
			Expect(abs(p)).ToNot(BeAnExistingFile())
		}
		for _, s := range job.Steps {
			Expect(s.Path).ToNot(BeEmpty())
		}

		steps, err := UndoJobSteps(ctx, ds, job.ID)
		Expect(err).ToNot(HaveOccurred())
		undo, err := q.EnqueueWith(ctx, model.EditUndo, UndoJobArgs(job.ID), steps)
		Expect(err).ToNot(HaveOccurred())
		undo = run(undo)
		Expect(undo.Skipped).To(Equal(0))
		for i, p := range paths {
			Expect(abs(p)).To(BeARegularFile())
			mf, err := ds.MediaFile(ctx).Get("track-" + string(rune('1'+i)))
			Expect(err).ToNot(HaveOccurred())
			Expect(mf.Quarantined).To(BeFalse())
		}
	})

	It("skips a File whose place in Quarantine is taken, and moves the others", func() {
		write("_Quarantine/Band/Record/01 one.mp3", "earlier")
		job := quarantineJob()
		Expect(job.Skipped).To(Equal(1))
		Expect(job.Steps[0].Code).To(Equal("taken"))
		Expect(abs(paths[0])).To(BeARegularFile())
		Expect(abs("_Quarantine/" + paths[1])).To(BeARegularFile())
	})

	It("refuses up front a Track already in Quarantine, or in a read-only library", func() {
		quarantineJob()
		_, err := q.Enqueue(ctx, model.EditQuarantine, []model.EditJobStep{QuarantineStep("track-1")})
		Expect(err).To(MatchError(tagedit.ErrInQuarantine))

		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Writable: false}}})
		_, err = q.Enqueue(ctx, model.EditRestore, []model.EditJobStep{RestoreStep(1, "_Quarantine/x.mp3")})
		Expect(err).To(MatchError(tagedit.ErrEditingOff))
	})

	Describe("RestoreFileStep", func() {
		It("undoes the move that put a File in Quarantine", func() {
			quarantineJob()
			step, err := RestoreFileStep(ctx, ds, 1, "_Quarantine/"+paths[0])
			Expect(err).ToNot(HaveOccurred())
			Expect(step.Kind).To(Equal(model.EditUndo))
			job, err := q.Enqueue(ctx, model.EditUndo, []model.EditJobStep{step})
			Expect(err).ToNot(HaveOccurred())
			Expect(run(job).Skipped).To(Equal(0))
			Expect(abs(paths[0])).To(BeARegularFile())
			Expect(abs("_Quarantine/"+paths[1])).To(BeARegularFile(), "only that File")
		})

		It("restores a File no journaled move put there to its path beneath Quarantine", func() {
			write("_Quarantine/Loose/Dupe.mp3", "dupe")
			step, err := RestoreFileStep(ctx, ds, 1, "_Quarantine/Loose/Dupe.mp3")
			Expect(err).ToNot(HaveOccurred())
			Expect(step.Kind).To(Equal(model.EditRestore))
			job, err := q.Enqueue(ctx, step.Kind, []model.EditJobStep{step})
			Expect(err).ToNot(HaveOccurred())
			Expect(run(job).Skipped).To(Equal(0))
			Expect(abs("Loose/Dupe.mp3")).To(BeARegularFile())
		})
	})

	Describe("Empty Quarantine", func() {
		It("deletes the confirmed Files as one job, which can't be undone", func() {
			quarantineJob()
			files := []tagedit.QuarantinedFile{{LibraryID: 1, Path: "_Quarantine/" + paths[0], Size: 4}, {LibraryID: 1, Path: "_Quarantine/" + paths[1], Size: 4}}
			step, err := EmptyStep(files)
			Expect(err).ToNot(HaveOccurred())
			job, err := q.Enqueue(ctx, model.EditEmptyQuarantine, []model.EditJobStep{step})
			Expect(err).ToNot(HaveOccurred())
			job = run(job)
			Expect(job.Steps[0].State).To(Equal(model.EditStepWritten))
			for _, p := range paths {
				Expect(abs("_Quarantine/" + p)).ToNot(BeAnExistingFile())
			}

			steps, err := UndoJobSteps(ctx, ds, job.ID)
			Expect(err).ToNot(HaveOccurred())
			_, err = q.EnqueueWith(ctx, model.EditUndo, UndoJobArgs(job.ID), steps)
			Expect(err).To(MatchError(tagedit.ErrNotUndoable))
		})

		It("runs its step again after a stop cut it off, deleting the Files still there", func() {
			quarantineJob()
			files := []tagedit.QuarantinedFile{{LibraryID: 1, Path: "_Quarantine/" + paths[0], Size: 4}, {LibraryID: 1, Path: "_Quarantine/" + paths[1], Size: 4}}
			step, err := EmptyStep(files)
			Expect(err).ToNot(HaveOccurred())
			job, err := q.Enqueue(ctx, model.EditEmptyQuarantine, []model.EditJobStep{step})
			Expect(err).ToNot(HaveOccurred())
			// The stop came after the first delete was written.
			_, err = editor.EmptyInJob(ctx, job.ID, files[:1])
			Expect(err).ToNot(HaveOccurred())
			_, err = db.Db().Exec("update edit_job set state = 'running' where id = ?", job.ID)
			Expect(err).ToNot(HaveOccurred())
			_, err = db.Db().Exec("update edit_job_step set state = 'running' where job_id = ?", job.ID)
			Expect(err).ToNot(HaveOccurred())

			job = run(job)
			Expect(job.Steps[0].State).To(Equal(model.EditStepWritten))
			Expect(abs("_Quarantine/" + paths[1])).ToNot(BeAnExistingFile())
		})

		It("is made for the Files in Quarantine now only while they are as many, and as large, as confirmed", func() {
			quarantineJob()
			write("_Quarantine/Loose/Dupe.mp3", "dupe!")
			_, err := ConfirmedEmptyStep(ctx, ds, 2, 8)
			Expect(err).To(MatchError(ErrQuarantineChanged))
			_, err = ConfirmedEmptyStep(ctx, ds, 3, 12)
			Expect(err).To(MatchError(ErrQuarantineChanged))
			step, err := ConfirmedEmptyStep(ctx, ds, 3, 13)
			Expect(err).ToNot(HaveOccurred())
			Expect(step.Args).To(ContainSubstring("_Quarantine/Loose/Dupe.mp3"))
			Expect(step.Args).To(ContainSubstring("_Quarantine/Band/Record/01 One.mp3"))
		})

		It("refuses an empty Quarantine", func() {
			_, err := ConfirmedEmptyStep(ctx, ds, 0, 0)
			Expect(err).To(MatchError(ErrNothingInQuarantine))
		})

		It("refuses an empty confirmation", func() {
			_, err := EmptyStep(nil)
			Expect(err).To(MatchError(ErrNothingInQuarantine))
		})
	})
})
