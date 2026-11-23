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
	"go.senan.xyz/taglib"
)

// The album cover jobs (#164) through the queue, on a real database and real Files.
var _ = Describe("Cover jobs", func() {
	var (
		ctx    context.Context
		ds     model.DataStore
		editor *tagedit.Editor
		q      *Queue
		dir    string
		files  []string
		cover  []byte
	)

	BeforeEach(func() {
		ctx = request.WithUser(context.Background(), model.User{ID: "admin-1", UserName: "admin", IsAdmin: true})
		ds = persistence.New(db.Db())
		for _, table := range []string{"correction", "media_file", "folder", "album", "edit_journal", "edit_job_step", "edit_job"} {
			_, err := db.Db().Exec("delete from " + table)
			Expect(err).ToNot(HaveOccurred())
		}
		libDir := GinkgoT().TempDir()
		_, err := db.Db().Exec("update library set path = ? where id = 1", libDir)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(configtest.SetupConfig())
		conf.Server.DataFolder = conf.NewDir(GinkgoT().TempDir())
		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Name: "NAS", Writable: true}}})
		DeferCleanup(func() { core.SetLibraryEditing(core.LibraryEditing{}) })

		folder := model.NewFolder(model.Library{ID: 1}, "Band/Record")
		Expect(ds.Folder(ctx).Put(folder)).To(Succeed())
		al := model.Album{ID: "al-1", Name: "Record", AlbumArtist: "Band", LibraryID: 1, FolderIDs: []string{folder.ID}}
		Expect(ds.Album(ctx).Put(&al)).To(Succeed())
		dir = filepath.Join(libDir, "Band", "Record")
		Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
		data, err := os.ReadFile(filepath.Join("tests", "fixtures", "test.mp3"))
		Expect(err).ToNot(HaveOccurred())
		files = nil
		for i, name := range []string{"01 One.mp3", "02 Two.mp3"} {
			p := filepath.Join(dir, name)
			Expect(os.WriteFile(p, data, 0o644)).To(Succeed())
			files = append(files, p)
			info, err := os.Stat(p)
			Expect(err).ToNot(HaveOccurred())
			mf := model.MediaFile{ID: "track-" + string(rune('1'+i)), LibraryID: 1, FolderID: folder.ID, AlbumID: al.ID,
				Path: "Band/Record/" + name, Suffix: "mp3", Size: info.Size(), UpdatedAt: info.ModTime(), Participants: model.Participants{}}
			Expect(ds.MediaFile(ctx).Put(&mf)).To(Succeed())
		}
		cover, err = os.ReadFile(filepath.Join("tests", "fixtures", "artist", "an-album", "cover.jpg"))
		Expect(err).ToNot(HaveOccurred())

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
	firstPicture := func(p string) []byte {
		GinkgoHelper()
		img, err := taglib.ReadImage(p)
		Expect(err).ToNot(HaveOccurred())
		return img
	}

	It("writes the album's cover.jpg as a job of one step, which a whole-job undo removes", func() {
		step, err := CoverStep(editor, "al-1", cover)
		Expect(err).ToNot(HaveOccurred())
		job, err := q.Enqueue(ctx, model.EditCover, []model.EditJobStep{step})
		Expect(err).ToNot(HaveOccurred())

		got := run(job)
		Expect(got.Steps).To(ConsistOf(And(HaveField("State", model.EditStepWritten), HaveField("Path", "Band/Record/cover.jpg"))))
		Expect(filepath.Join(dir, "cover.jpg")).To(BeAnExistingFile())

		steps, err := UndoJobSteps(ctx, ds, job.ID)
		Expect(err).ToNot(HaveOccurred())
		undo, err := q.Enqueue(ctx, model.EditUndo, steps)
		Expect(err).ToNot(HaveOccurred())
		Expect(run(undo).Skipped).To(BeZero())
		Expect(filepath.Join(dir, "cover.jpg")).ToNot(BeAnExistingFile())
	})

	It("refuses the undo of a cover write up front while its library is read-only", func() {
		step, err := CoverStep(editor, "al-1", cover)
		Expect(err).ToNot(HaveOccurred())
		job, err := q.Enqueue(ctx, model.EditCover, []model.EditJobStep{step})
		Expect(err).ToNot(HaveOccurred())
		run(job)

		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Writable: false}}})
		steps, err := UndoJobSteps(ctx, ds, job.ID)
		Expect(err).ToNot(HaveOccurred())
		_, err = q.Enqueue(ctx, model.EditUndo, steps)
		Expect(err).To(MatchError(tagedit.ErrEditingOff))
	})

	It("refuses a cover job for an image the app can't read, queueing nothing", func() {
		_, err := CoverStep(editor, "al-1", []byte("nope"))
		Expect(err).To(MatchError(tagedit.ErrInvalidImage))
	})

	It("embeds the folder cover into every File as one job, which a whole-job undo puts back", func() {
		_, err := EmbedSteps(ctx, ds, editor, "al-1")
		Expect(err).To(MatchError(tagedit.ErrNoFolderCover), "there is no cover.jpg yet")

		Expect(os.WriteFile(filepath.Join(dir, "cover.jpg"), cover, 0o644)).To(Succeed())
		before := firstPicture(files[0])
		steps, err := EmbedSteps(ctx, ds, editor, "al-1")
		Expect(err).ToNot(HaveOccurred())
		Expect(steps).To(HaveLen(2))
		job, err := q.Enqueue(ctx, model.EditEmbed, steps)
		Expect(err).ToNot(HaveOccurred())

		got := run(job)
		Expect(got.Skipped).To(BeZero())
		for _, f := range files {
			Expect(firstPicture(f)).To(Equal(cover))
		}
		entries, err := ds.EditJournal(ctx).ForJob(job.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(entries).To(HaveLen(2))
		Expect(entries).To(HaveEach(HaveField("Kind", model.EditEmbed)))

		// The scan after the embed would record the Files' new sizes and times.
		for i, f := range files {
			info, err := os.Stat(f)
			Expect(err).ToNot(HaveOccurred())
			_, err = db.Db().Exec("update media_file set size = ?, updated_at = ? where id = ?",
				info.Size(), info.ModTime(), "track-"+string(rune('1'+i)))
			Expect(err).ToNot(HaveOccurred())
		}
		undoSteps, err := UndoJobSteps(ctx, ds, job.ID)
		Expect(err).ToNot(HaveOccurred())
		undo, err := q.Enqueue(ctx, model.EditUndo, undoSteps)
		Expect(err).ToNot(HaveOccurred())
		Expect(run(undo).Skipped).To(BeZero())
		for _, f := range files {
			Expect(firstPicture(f)).To(Equal(before))
		}
	})
})
