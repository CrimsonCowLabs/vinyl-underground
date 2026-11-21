package rip

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Promote", func() {
	var (
		ctx            context.Context
		ds             *tests.MockDataStore
		mfs            *tests.MockMediaFileRepo
		music, inbox   string
		promote        string
		scanner        *scanRecorder
		ripper         *Ripper
		inboxID        int
		destOpus       string
		destOpusRel    string
		otherJobsMatch *model.RipItem
	)

	const musicID = 1

	BeforeEach(func() {
		ctx = request.WithUser(GinkgoT().Context(), model.User{ID: "admin", IsAdmin: true})
		music, inbox = GinkgoT().TempDir(), GinkgoT().TempDir()
		promote = filepath.Join(music, "Vinyl Underground")
		Expect(os.Mkdir(promote, 0o755)).To(Succeed())
		libs := &tests.MockLibraryRepo{}
		libs.SetData(model.Libraries{{ID: musicID, Name: "Music", Path: music}})
		libs.PutFn = func(l *model.Library) error {
			if l.ID == 0 {
				l.ID = 2
			}
			libs.Data[l.ID] = *l
			return nil
		}
		mfs = tests.CreateMockMediaFileRepo()
		ds = &tests.MockDataStore{MockedLibrary: libs, MockedMediaFile: mfs}
		_, err := core.EnsureInboxLibrary(ctx, ds, inbox)
		Expect(err).ToNot(HaveOccurred())
		lib, err := core.FindInboxLibrary(ctx, ds)
		Expect(err).ToNot(HaveOccurred())
		inboxID = lib.ID
		scanner = &scanRecorder{}
		ripper = New(ds, newFakeFetcher(), scanner, Options{ScanRetryDelay: time.Millisecond, PromoteRoot: promote})
		destOpusRel = "Vinyl Underground/Rip Artist/Rip Album/Ripped Opus.opus"
		destOpus = filepath.Join(music, filepath.FromSlash(destOpusRel))

		// Another job that warned "already have" about the Track in a.opus.
		other := &model.RipJob{URL: "https://www.youtube.com/watch?v=y", Status: model.RipReady}
		Expect(ds.RipJob(ctx).Put(other)).To(Succeed())
		otherJobsMatch = &model.RipItem{JobID: other.ID, Position: 1, Status: model.RipQueued,
			AlreadyHave: model.RipMatches{{ID: "mf-a", Title: "Ripped Opus", Library: "Inbox"}}}
		Expect(ds.RipItem(ctx).Put(otherJobsMatch)).To(Succeed())
	})

	landed := func(fixture, rel string) []byte {
		data, err := os.ReadFile(filepath.Join("tests", "fixtures", "rips", fixture))
		Expect(err).ToNot(HaveOccurred())
		full := filepath.Join(inbox, filepath.FromSlash(rel))
		Expect(os.MkdirAll(filepath.Dir(full), 0o755)).To(Succeed())
		Expect(os.WriteFile(full, data, 0o644)).To(Succeed())
		return data
	}
	scanned := func(id, rel string) {
		mfs.Data[id] = &model.MediaFile{ID: id, LibraryID: inboxID, Path: rel, FolderID: "inbox-folder"}
	}
	newJob := func(status model.RipStatus, folder string, paths ...string) *model.RipJob {
		job := &model.RipJob{URL: "https://music.youtube.com/playlist?list=x", Status: status, Folder: folder}
		Expect(ds.RipJob(ctx).Put(job)).To(Succeed())
		for i, p := range paths {
			it := model.RipItem{JobID: job.ID, Position: i + 1, Status: model.RipReady, Path: p}
			if p == "" {
				it.Status, it.Error = model.RipFailed, "ERROR: Video unavailable"
			}
			if status == model.RipDownloading || status == model.RipQueued {
				it.Status = status
			}
			Expect(ds.RipItem(ctx).Put(&it)).To(Succeed())
			job.Items = append(job.Items, it)
		}
		return job
	}
	itemsOf := func(job *model.RipJob) model.RipItems {
		items, err := ds.RipItem(ctx).ForJobs(job.ID)
		Expect(err).ToNot(HaveOccurred())
		return items
	}
	jobGone := func(job *model.RipJob) {
		_, err := ds.RipJob(ctx).Get(job.ID)
		Expect(err).To(MatchError(model.ErrNotFound))
		Expect(itemsOf(job)).To(BeEmpty())
	}
	inInbox := func(rel string) bool {
		_, err := os.Lstat(filepath.Join(inbox, filepath.FromSlash(rel)))
		return err == nil
	}

	It("moves every File into the promote folder, keeps each Track, and the job goes", func() {
		opus := landed("ripped.opus", "Album/a.opus")
		landed("ripped.m4a", "Album/b.m4a")
		scanned("mf-a", "Album/a.opus")
		scanned("mf-b", "Album/b.m4a")
		job := newJob(model.RipReady, "Album", "Album/a.opus", "", "Album/b.m4a")

		res, err := ripper.Promote(ctx, job.ID)
		Expect(err).ToNot(HaveOccurred())

		Expect(res.Failed).To(BeEmpty())
		Expect(res.Promoted).To(ConsistOf(
			PromotedFile{ItemID: job.Items[0].ID, Path: destOpusRel},
			PromotedFile{ItemID: job.Items[2].ID, Path: "Vinyl Underground/Rip Artist/Rip Album/Ripped M4A.m4a"},
		))
		Expect(res.Review).To(BeNil())
		Expect(os.ReadFile(destOpus)).To(Equal(opus))
		Expect(filepath.Join(inbox, "Album")).ToNot(BeADirectory())
		jobGone(job)

		a := mfs.Data["mf-a"]
		Expect(a.LibraryID).To(Equal(musicID))
		Expect(a.Path).To(Equal(destOpusRel))
		Expect(a.FolderID).To(Equal(model.FolderID(model.Library{ID: musicID, Path: music}, "Vinyl Underground/Rip Artist/Rip Album")))
		Expect(mfs.Data["mf-b"].LibraryID).To(Equal(musicID))

		Expect(itemsOf(&model.RipJob{ID: otherJobsMatch.JobID})[0].AlreadyHave[0].Library).To(Equal("Music"))
		Eventually(scanner.scans).Should(ConsistOf(ConsistOf(
			model.ScanTarget{LibraryID: inboxID},
			model.ScanTarget{LibraryID: musicID, FolderPath: "Vinyl Underground"},
		)))
	})

	It("promotes a single video's File, which has no subfolder, and leaves the Inbox itself", func() {
		landed("ripped.opus", "a.opus")
		job := newJob(model.RipReady, "", "a.opus")
		res, err := ripper.Promote(ctx, job.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(res.Promoted).To(HaveLen(1))
		Expect(destOpus).To(BeAnExistingFile())
		Expect(inbox).To(BeADirectory())
		jobGone(job)
	})

	It("promotes File by File: one that fails stays in the Inbox with its error, the others go", func() {
		landed("ripped.opus", "Album/a.opus")
		m4a := landed("ripped.m4a", "Album/b.m4a")
		scanned("mf-a", "Album/a.opus")
		scanned("mf-b", "Album/b.m4a")
		job := newJob(model.RipReady, "Album", "Album/a.opus", "Album/b.m4a")
		ripper.promoteFS = &faultyFS{promoteFS: osFS{}, op: "write", match: "Ripped M4A", err: errInjected}

		res, err := ripper.Promote(ctx, job.ID)
		Expect(err).ToNot(HaveOccurred())

		Expect(res.Promoted).To(ConsistOf(PromotedFile{ItemID: job.Items[0].ID, Path: destOpusRel}))
		Expect(res.Failed).To(HaveLen(1))
		Expect(res.Failed[0].ItemID).To(Equal(job.Items[1].ID))
		Expect(res.Failed[0].Error).To(ContainSubstring("injected failure"))
		Expect(res.Review).ToNot(BeNil())
		Expect(res.Review.Tracks).To(HaveLen(1))
		Expect(res.Review.Tracks[0].ItemID).To(Equal(job.Items[1].ID))

		Expect(os.ReadFile(filepath.Join(inbox, "Album", "b.m4a"))).To(Equal(m4a))
		entries, _ := os.ReadDir(filepath.Join(promote, "Rip Artist", "Rip Album"))
		Expect(entries).To(HaveLen(1)) // the Opus File only: no temp, no half-copied M4A
		Expect(mfs.Data["mf-b"].LibraryID).To(Equal(inboxID))
		items := itemsOf(job)
		Expect(items).To(HaveLen(1))
		Expect(items[0].Error).To(ContainSubstring("Promote failed"))
		Expect(items[0].PromoteDest).To(BeEmpty())

		// Fixed, it goes on a second try, and then the job goes too.
		ripper.promoteFS = nil
		res, err = ripper.Promote(ctx, job.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(res.Promoted).To(HaveLen(1))
		jobGone(job)
	})

	It("leaves the File in the Inbox, and nothing in the promote folder, when the Track can't be moved", func() {
		opus := landed("ripped.opus", "Album/a.opus")
		scanned("mf-a", "Album/a.opus")
		job := newJob(model.RipReady, "Album", "Album/a.opus")
		mfs.Err = true

		res, err := ripper.Promote(ctx, job.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(res.Failed).To(HaveLen(1))
		Expect(os.ReadFile(filepath.Join(inbox, "Album", "a.opus"))).To(Equal(opus))
		Expect(destOpus).ToNot(BeAnExistingFile())
		Expect(itemsOf(job)[0].PromoteDest).To(BeEmpty())
	})

	It("says so when the Inbox copy can't be deleted, still rescans, and deletes it on the next try", func() {
		landed("ripped.opus", "Album/a.opus")
		scanned("mf-a", "Album/a.opus")
		job := newJob(model.RipReady, "Album", "Album/a.opus")
		ripper.promoteFS = &faultyFS{promoteFS: osFS{}, op: "remove", match: filepath.Join(inbox, "Album", "a.opus"), err: errInjected}

		res, err := ripper.Promote(ctx, job.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(res.Failed).To(HaveLen(1))
		Expect(res.Failed[0].Error).To(ContainSubstring("Inbox copy could not be deleted"))
		Expect(mfs.Data["mf-a"].Path).To(Equal(destOpusRel))
		Expect(os.ReadFile(destOpus)).ToNot(BeEmpty())
		Expect(itemsOf(job)[0].PromoteDest).To(Equal(destOpus))
		Eventually(scanner.scans).Should(HaveLen(1))

		res, err = ripper.Promote(ctx, job.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(res.Promoted).To(HaveLen(1))
		Expect(inInbox("Album/a.opus")).To(BeFalse())
		Expect(destOpus).To(BeAnExistingFile())
		jobGone(job)
	})

	It("reuses folders in the promote folder whose names differ only by letter case", func() {
		Expect(os.MkdirAll(filepath.Join(promote, "RIP ARTIST", "rip album"), 0o755)).To(Succeed())
		landed("ripped.opus", "a.opus")
		job := newJob(model.RipReady, "", "a.opus")
		_, err := ripper.Promote(ctx, job.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(filepath.Join(promote, "RIP ARTIST", "rip album", "Ripped Opus.opus")).To(BeAnExistingFile())
		entries, _ := os.ReadDir(promote)
		Expect(entries).To(HaveLen(1))
	})

	It("refuses a job still running, a job with no Files, and is off with no promote folder", func() {
		running := newJob(model.RipDownloading, "Album", "Album/a.opus")
		_, err := ripper.Promote(ctx, running.ID)
		Expect(err).To(MatchError(ErrNotReady))

		failed := newJob(model.RipFailed, "", "")
		_, err = ripper.Promote(ctx, failed.ID)
		Expect(err).To(MatchError(ErrNothingToPromote))

		_, err = ripper.Promote(ctx, "nope")
		Expect(err).To(MatchError(model.ErrNotFound))

		off := New(ds, newFakeFetcher(), scanner, Options{})
		Expect(off.PromoteEnabled()).To(BeFalse())
		_, err = off.Promote(ctx, running.ID)
		Expect(err).To(MatchError(ErrPromoteOff))
	})

	Describe("after a restart in the middle of a Promote", func() {
		var (
			job  *model.RipJob
			opus []byte
		)

		BeforeEach(func() {
			opus = landed("ripped.opus", "Album/a.opus")
			scanned("mf-a", "Album/a.opus")
			job = newJob(model.RipReady, "Album", "Album/a.opus")
			job.Items[0].PromoteDest = destOpus
			Expect(ds.RipItem(ctx).Put(&job.Items[0])).To(Succeed())
			Expect(os.MkdirAll(filepath.Dir(destOpus), 0o755)).To(Succeed())
		})

		It("undoes a move stopped while copying: the temp goes, the File stays in the Inbox", func() {
			Expect(os.WriteFile(promoteTemp(destOpus), opus[:10], 0o644)).To(Succeed())
			ripper.ResumePromotes(ctx)
			Expect(promoteTemp(destOpus)).ToNot(BeAnExistingFile())
			Expect(destOpus).ToNot(BeAnExistingFile())
			Expect(inInbox("Album/a.opus")).To(BeTrue())
			Expect(itemsOf(job)[0].PromoteDest).To(BeEmpty())
			Expect(mfs.Data["mf-a"].LibraryID).To(Equal(inboxID))
		})

		It("finishes a move stopped once the copy was in place: the Track moves, the Inbox copy goes", func() {
			Expect(os.WriteFile(destOpus, opus, 0o644)).To(Succeed())
			ripper.ResumePromotes(ctx)
			Expect(inInbox("Album/a.opus")).To(BeFalse())
			Expect(mfs.Data["mf-a"].Path).To(Equal(destOpusRel))
			jobGone(job)
			Eventually(scanner.scans).Should(HaveLen(1))
		})

		It("finishes a move stopped after the Track moved: only the Inbox copy was left", func() {
			Expect(os.WriteFile(destOpus, opus, 0o644)).To(Succeed())
			Expect(mfs.Relocate("mf-a", musicID, destOpusRel, "f")).To(Succeed())
			ripper.ResumePromotes(ctx)
			Expect(inInbox("Album/a.opus")).To(BeFalse())
			Expect(os.ReadFile(destOpus)).To(Equal(opus))
			jobGone(job)
		})

		It("leaves alone a File at the destination that isn't the one promoted", func() {
			Expect(os.WriteFile(destOpus, []byte("someone else's"), 0o644)).To(Succeed())
			ripper.ResumePromotes(ctx)
			Expect(os.ReadFile(destOpus)).To(Equal([]byte("someone else's")))
			Expect(inInbox("Album/a.opus")).To(BeTrue())
			Expect(itemsOf(job)[0].PromoteDest).To(BeEmpty())
			Expect(mfs.Data["mf-a"].LibraryID).To(Equal(inboxID))
		})

		It("keeps a move whose Track moved but whose File can't be seen (the mount is gone) for later", func() {
			Expect(mfs.Relocate("mf-a", musicID, destOpusRel, "f")).To(Succeed())
			ripper.ResumePromotes(ctx)
			Expect(itemsOf(job)[0].PromoteDest).To(Equal(destOpus))
			Expect(inInbox("Album/a.opus")).To(BeTrue())
			Expect(mfs.Data["mf-a"].Path).To(Equal(destOpusRel))

			_, err := ripper.Promote(ctx, job.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(itemsOf(job)[0].PromoteDest).To(Equal(destOpus), "still not undone")
			Expect(destOpus).ToNot(BeAnExistingFile())
		})

		It("merges a Track scanned from the Inbox copy left behind into the moved Track, which stays", func() {
			Expect(os.WriteFile(destOpus, opus, 0o644)).To(Succeed())
			mfs.Data["mf-a"].CreatedAt = time.Now().Add(-time.Hour)
			Expect(mfs.Relocate("mf-a", musicID, destOpusRel, "f")).To(Succeed())
			scanned("mf-new", "Album/a.opus") // after the restart, from the copy still in the Inbox
			mfs.Data["mf-new"].CreatedAt = time.Now()

			ripper.ResumePromotes(ctx)

			Expect(inInbox("Album/a.opus")).To(BeFalse())
			Expect(mfs.Data).ToNot(HaveKey("mf-new"))
			Expect(mfs.ReassignReferencesCalls).To(HaveKeyWithValue("mf-new", "mf-a"))
			Expect(mfs.Data["mf-a"].Path).To(Equal(destOpusRel))
			jobGone(job)
		})

		It("leaves interrupted moves alone while Promote is off (its folder may not be mounted)", func() {
			Expect(os.WriteFile(promoteTemp(destOpus), opus[:10], 0o644)).To(Succeed())
			off := New(ds, newFakeFetcher(), scanner, Options{})
			off.ResumePromotes(ctx)
			Expect(promoteTemp(destOpus)).To(BeAnExistingFile())
			Expect(itemsOf(job)[0].PromoteDest).To(Equal(destOpus))
		})

		It("does the same when the admin promotes the job again before anything else", func() {
			Expect(os.WriteFile(destOpus, opus, 0o644)).To(Succeed())
			res, err := ripper.Promote(ctx, job.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Promoted).To(ConsistOf(PromotedFile{ItemID: job.Items[0].ID, Path: destOpusRel}))
			entries, _ := os.ReadDir(filepath.Dir(destOpus))
			Expect(entries).To(HaveLen(1)) // no "(2)" copy
			jobGone(job)
		})
	})
})
