package editjob

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/safefile"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.senan.xyz/taglib"
)

// A tag edit through the queue, on real Files, and a restart in the middle of one.
var _ = Describe("Tag edit jobs", func() {
	var (
		ctx     context.Context
		ds      *tests.MockDataStore
		journal *tests.MockEditJournalRepo
		editor  *tagedit.Editor
		q       *Queue
		libDir  string
		files   []string // the Files, absolute
	)

	BeforeEach(func() {
		ctx = request.WithUser(context.Background(), model.User{ID: "admin-1", IsAdmin: true})
		libDir = GinkgoT().TempDir()
		ds = &tests.MockDataStore{MockedEditJob: &tests.MockEditJobRepo{}}
		journal = &tests.MockEditJournalRepo{}
		ds.MockedEditJournal = journal
		ds.MockedLibrary = &tests.MockLibraryRepo{}
		ds.MockedLibrary.(*tests.MockLibraryRepo).SetData(model.Libraries{{ID: 1, Name: "NAS", Path: libDir}})
		ds.MockedMediaFile = tests.CreateMockMediaFileRepo()
		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Name: "NAS", Writable: true}}})
		DeferCleanup(func() { core.SetLibraryEditing(core.LibraryEditing{}) })

		data, err := os.ReadFile(filepath.Join("tests", "fixtures", "test.mp3"))
		Expect(err).ToNot(HaveOccurred())
		Expect(os.MkdirAll(filepath.Join(libDir, "Artist", "Album"), 0o755)).To(Succeed())
		files = nil
		var mfs model.MediaFiles
		for i, name := range []string{"01 One.mp3", "02 Two.mp3"} {
			p := filepath.Join(libDir, "Artist", "Album", name)
			Expect(os.WriteFile(p, data, 0o644)).To(Succeed())
			info, err := os.Stat(p)
			Expect(err).ToNot(HaveOccurred())
			files = append(files, p)
			mfs = append(mfs, model.MediaFile{ID: "track-" + string(rune('1'+i)), LibraryID: 1, Path: "Artist/Album/" + name,
				Suffix: "mp3", Size: info.Size(), UpdatedAt: info.ModTime()})
		}
		ds.MockedMediaFile.(*tests.MockMediaFileRepo).SetData(mfs)
		editor = tagedit.New(ds, nil)
		q = New(ds, nil, Kinds(editor))
	})

	titleOf := func(p string) string {
		GinkgoHelper()
		tags, err := taglib.ReadTags(p)
		Expect(err).ToNot(HaveOccurred())
		return strings.Join(tags["TITLE"], "|")
	}
	hiddenFiles := func() []string {
		GinkgoHelper()
		entries, err := os.ReadDir(filepath.Dir(files[0]))
		Expect(err).ToNot(HaveOccurred())
		var res []string
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				res = append(res, e.Name())
			}
		}
		return res
	}
	enqueue := func() *model.EditJob {
		GinkgoHelper()
		var steps []model.EditJobStep
		for i, title := range []string{"New One", "New Two"} {
			s, err := TagsStep("track-"+string(rune('1'+i)), tagedit.Values{"title": {title}})
			Expect(err).ToNot(HaveOccurred())
			steps = append(steps, s)
		}
		job, err := q.Enqueue(ctx, model.EditTags, steps)
		Expect(err).ToNot(HaveOccurred())
		return job
	}
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
	// stopInFirstFile leaves the job as a stop in the middle of its first File would: the job and
	// the step running, the write pending in the journal (with the checksum of its checked copy,
	// if it got that far), and the File where the stop left it.
	stopInFirstFile := func(job *model.EditJob, copySum string) {
		GinkgoHelper()
		repo := ds.EditJob(ctx)
		job.State, job.Current = model.EditJobRunning, "Artist/Album/01 One.mp3"
		Expect(repo.Put(job)).To(Succeed())
		steps, err := repo.Steps(job.ID)
		Expect(err).ToNot(HaveOccurred())
		steps[0].State = model.EditStepRunning
		Expect(repo.PutStep(&steps[0])).To(Succeed())
		Expect(journal.Put(&model.EditEntry{JobID: job.ID, Kind: model.EditTags, State: model.EditPending, LibraryID: 1,
			MediaFileID: "track-1", Path: "Artist/Album/01 One.mp3", NewPath: "Artist/Album/01 One.mp3", CopySum: copySum})).To(Succeed())
	}

	It("writes every File of the job, through the Edit journal, with the job's ID", func() {
		job := enqueue()
		got := run(job)
		Expect(got.Skipped).To(BeZero())
		Expect(titleOf(files[0])).To(Equal("New One"))
		Expect(titleOf(files[1])).To(Equal("New Two"))
		entries := journal.All()
		Expect(entries).To(HaveLen(2))
		Expect(entries).To(HaveEach(HaveField("JobID", job.ID)))
		Expect(entries).To(HaveEach(HaveField("UserID", "admin-1")))
		Expect(got.Steps[0].EntryID).To(Equal(entries[0].ID))
	})

	It("skips a File changed on disk since the scan, and writes the rest", func() {
		job := enqueue()
		Expect(os.Chtimes(files[0], time.Now(), time.Now().Add(-time.Hour))).To(Succeed())
		got := run(job)
		Expect(got.Skipped).To(Equal(1))
		Expect(got.Steps[0].State).To(Equal(model.EditStepSkipped))
		Expect(got.Steps[0].Code).To(Equal("changedOnDisk"))
		Expect(titleOf(files[1])).To(Equal("New Two"))
	})

	It("finishes a File a restart cut off as its checked copy took its place, then carries on", func() {
		job := enqueue()
		// The checked copy, with the new title, moved into the File's place, and the File still
		// at its backup name.
		temp := safefile.TempName(files[0])
		data, err := os.ReadFile(files[0])
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(temp, data, 0o644)).To(Succeed())
		Expect(taglib.WriteTags(temp, map[string][]string{"TITLE": {"New One"}}, 0)).To(Succeed())
		copied, err := os.ReadFile(temp)
		Expect(err).ToNot(HaveOccurred())
		sum := sha256.Sum256(copied)
		Expect(os.Rename(files[0], safefile.BackupName(files[0]))).To(Succeed())
		Expect(os.Rename(temp, files[0])).To(Succeed())
		stopInFirstFile(job, hex.EncodeToString(sum[:]))

		// The restart: the startup settle, then the queue.
		editor.Settle(ctx)
		got := run(job)

		Expect(titleOf(files[0])).To(Equal("New One"))
		Expect(titleOf(files[1])).To(Equal("New Two"))
		Expect(hiddenFiles()).To(BeEmpty())
		Expect(got.Steps[0].State).To(Equal(model.EditStepWritten))
		Expect(got.Steps[1].State).To(Equal(model.EditStepWritten))
		Expect(got.Skipped).To(BeZero())
	})

	DescribeTable("rolls back a File a restart cut off before its copy took its place, then carries on",
		func(movedAside bool) {
			job := enqueue()
			before := titleOf(files[0])
			// A half-made copy beside the File; or the File moved aside, its copy not yet in place.
			temp := safefile.TempName(files[0])
			Expect(os.WriteFile(temp, []byte("half a copy"), 0o644)).To(Succeed())
			sum := ""
			if movedAside {
				s := sha256.Sum256([]byte("half a copy"))
				sum = hex.EncodeToString(s[:])
				Expect(os.Rename(files[0], safefile.BackupName(files[0]))).To(Succeed())
			}
			stopInFirstFile(job, sum)

			editor.Settle(ctx)
			got := run(job)

			Expect(titleOf(files[0])).To(Equal(before))
			Expect(titleOf(files[1])).To(Equal("New Two"))
			Expect(hiddenFiles()).To(BeEmpty())
			Expect(got.Steps[0].State).To(Equal(model.EditStepSkipped))
			Expect(got.Steps[0].Code).To(Equal(CodeInterrupted))
			Expect(got.Steps[1].State).To(Equal(model.EditStepWritten))
		},
		Entry("before the copy was checked", false),
		Entry("with the File moved aside", true),
	)

	It("refuses to queue an edit of a Track whose library is read-only", func() {
		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Name: "NAS", Writable: false}}})
		s, err := TagsStep("track-1", tagedit.Values{"title": {"x"}})
		Expect(err).ToNot(HaveOccurred())
		Expect(q.Enqueue(ctx, model.EditTags, []model.EditJobStep{s})).Error().To(MatchError(tagedit.ErrEditingOff))
	})
})
