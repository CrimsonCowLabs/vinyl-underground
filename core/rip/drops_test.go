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
	"go.senan.xyz/taglib"
)

// Files copied straight into the Inbox folder (#35) are found among the Inbox's Tracks after a
// scan, and grouped by folder into review cards once they have stopped changing.
var _ = Describe("Dropped Files", func() {
	const (
		inboxID = 2
		settle  = 30 * time.Millisecond
	)
	var (
		ctx      context.Context
		ds       *tests.MockDataStore
		mfs      *tests.MockMediaFileRepo
		tracks   model.MediaFiles
		inbox    string
		proposer *fakeProposer
		matcher  *fakeMatcher
		ripper   *Ripper
	)

	BeforeEach(func() {
		ctx = request.WithUser(GinkgoT().Context(), model.User{ID: "admin", IsAdmin: true})
		inbox = GinkgoT().TempDir()
		libs := &tests.MockLibraryRepo{}
		libs.SetData(model.Libraries{{ID: 1, Name: "Music", Path: GinkgoT().TempDir()}})
		libs.PutFn = func(l *model.Library) error {
			if l.ID == 0 {
				l.ID = inboxID
			}
			libs.Data[l.ID] = *l
			return nil
		}
		mfs = tests.CreateMockMediaFileRepo()
		tracks = nil
		ds = &tests.MockDataStore{MockedLibrary: libs, MockedMediaFile: mfs,
			MockedUser: tests.CreateMockUserRepo()}
		_, err := core.EnsureInboxLibrary(ctx, ds, inbox)
		Expect(err).ToNot(HaveOccurred())
		Expect(ds.User(ctx).Put(&model.User{ID: "admin", UserName: "admin", IsAdmin: true})).To(Succeed())
		proposer = &fakeProposer{}
		matcher = &fakeMatcher{have: map[string]model.RipMatches{}}
		ripper = New(ds, newFakeFetcher(), &scanRecorder{}, Options{
			DropSettle: settle, ScanRetryDelay: time.Millisecond, UploadScanDelay: time.Millisecond,
			Proposer: proposer, Matcher: matcher,
		})
	})

	// drop copies a fixture into the Inbox at rel, and scans it in as an Inbox Track.
	drop := func(fixture, rel string, trackNumber int) {
		data, err := os.ReadFile(filepath.Join("tests", "fixtures", fixture))
		Expect(err).ToNot(HaveOccurred())
		full := filepath.Join(inbox, filepath.FromSlash(rel))
		Expect(os.MkdirAll(filepath.Dir(full), 0o755)).To(Succeed())
		Expect(os.WriteFile(full, data, 0o644)).To(Succeed())
		tracks = append(tracks, model.MediaFile{ID: "mf-" + rel, LibraryID: inboxID, Path: rel,
			Title: "Song", Artist: "Artist", TrackNumber: trackNumber, Size: int64(len(data))})
		mfs.SetData(tracks)
	}
	reconcile := func() bool {
		pending, err := ripper.ReconcileDrops(ctx)
		Expect(err).ToNot(HaveOccurred())
		return pending
	}
	// settled reconciles twice, a settle apart, as the reconciler's loop does.
	settled := func() {
		reconcile()
		time.Sleep(settle + 10*time.Millisecond)
		reconcile()
	}
	jobs := func() model.RipJobs {
		js, err := ripper.Jobs(ctx)
		Expect(err).ToNot(HaveOccurred())
		return js
	}
	paths := func(job model.RipJob) []string {
		var res []string
		for _, it := range job.Items {
			res = append(res, it.Path)
		}
		return res
	}

	It("groups a dropped folder's Files into one card, in track order, once they have settled", func() {
		drop("test.mp3", "Album/b.mp3", 2)
		drop("test.flac", "Album/CD1/a.flac", 1)

		Expect(reconcile()).To(BeTrue(), "first seen: waiting for them to settle")
		Expect(jobs()).To(BeEmpty())
		time.Sleep(settle + 10*time.Millisecond)
		Expect(reconcile()).To(BeFalse())

		js := jobs()
		Expect(js).To(HaveLen(1))
		job := js[0]
		Expect(job.Kind).To(Equal(model.RipKindDrop))
		Expect(job.Status).To(Equal(model.RipReady))
		Expect(job.Folder).To(Equal("Album"))
		Expect(job.Title).To(Equal("Album"))
		Expect(job.URL).To(BeEmpty())
		Expect(job.UserID).To(Equal("admin"))
		Expect(paths(job)).To(Equal([]string{"Album/CD1/a.flac", "Album/b.mp3"}))
		Expect(job.Items[0].Status).To(Equal(model.RipReady))
		Expect(job.Items[0].Position).To(Equal(1))
		Expect(job.Items[1].Position).To(Equal(2))
		Expect(job.Items[1].Title).To(Equal("Song"), "named after the File's own tags")
	})

	It("gives each File dropped loose in the Inbox a card of its own", func() {
		drop("test.mp3", "one.mp3", 0)
		drop("test.ogg", "two.ogg", 0)
		settled()

		js := jobs()
		Expect(js).To(HaveLen(2))
		for _, j := range js {
			Expect(j.Kind).To(Equal(model.RipKindDrop))
			Expect(j.Folder).To(BeEmpty())
			Expect(j.Items).To(HaveLen(1))
		}
	})

	It("adopts nothing twice, however often it runs", func() {
		drop("test.mp3", "Album/a.mp3", 1)
		settled()
		settled()
		reconcile()
		Expect(jobs()).To(HaveLen(1))
		Expect(jobs()[0].Items).To(HaveLen(1))
	})

	It("adds a File dropped later into the folder to the folder's card", func() {
		drop("test.mp3", "Album/a.mp3", 1)
		settled()
		drop("test.ogg", "Album/b.ogg", 2)
		Expect(os.Chtimes(filepath.Join(inbox, "Album"), time.Now(), time.Now())).To(Succeed())
		settled()

		js := jobs()
		Expect(js).To(HaveLen(1))
		Expect(paths(js[0])).To(Equal([]string{"Album/a.mp3", "Album/b.ogg"}))
		Expect(js[0].Items[1].Position).To(Equal(2))
	})

	It("never adopts a File a Rip or an upload knows, nor a hidden one", func() {
		drop("test.mp3", "Ripped/a.mp3", 1)
		drop("test.mp3", "Upload 2026-11-21 10.00.00/a.mp3", 1)
		drop("test.mp3", ".rips/item-1/a.mp3", 1)
		drop("test.mp3", ".uploads/upload-1.mp3", 0)
		drop("test.mp3", "Album/.a.edit-123.mp3", 0)
		for _, kind := range []model.RipKind{model.RipKindRip, model.RipKindUpload} {
			job := &model.RipJob{Kind: kind, Status: model.RipReady}
			Expect(ds.RipJob(ctx).Put(job)).To(Succeed())
			p := "Ripped/a.mp3"
			if kind == model.RipKindUpload {
				p = "Upload 2026-11-21 10.00.00/a.mp3"
			}
			Expect(ds.RipItem(ctx).Put(&model.RipItem{JobID: job.ID, Position: 1, Status: model.RipReady, Path: p})).To(Succeed())
		}
		settled()

		Expect(jobs()).To(HaveLen(2), "only the Rip and the upload")
	})

	It("waits while a File is still changing, and adopts its folder only once all of it has settled", func() {
		drop("test.mp3", "Album/a.mp3", 1)
		drop("test.mp3", "Album/b.mp3", 2)
		reconcile()
		time.Sleep(settle + 10*time.Millisecond)
		// b.mp3 is still being copied.
		f, err := os.OpenFile(filepath.Join(inbox, "Album", "b.mp3"), os.O_APPEND|os.O_WRONLY, 0)
		Expect(err).ToNot(HaveOccurred())
		_, err = f.Write([]byte("more"))
		Expect(err).ToNot(HaveOccurred())
		Expect(f.Close()).To(Succeed())
		Expect(reconcile()).To(BeTrue())
		Expect(jobs()).To(BeEmpty())

		time.Sleep(settle + 10*time.Millisecond)
		Expect(reconcile()).To(BeTrue(), "its Track was scanned before the copy finished")
		Expect(jobs()).To(BeEmpty())

		tracks[1].Size += 4 // the next scan reads it whole
		mfs.SetData(tracks)
		reconcile()
		Expect(jobs()).To(HaveLen(1))
		Expect(jobs()[0].Items).To(HaveLen(2))
	})

	It("waits while Files are still being added to the folder", func() {
		drop("test.mp3", "Album/a.mp3", 1)
		reconcile()
		time.Sleep(settle + 10*time.Millisecond)
		Expect(os.Chtimes(filepath.Join(inbox, "Album"), time.Now(), time.Now())).To(Succeed())
		Expect(reconcile()).To(BeTrue())
		Expect(jobs()).To(BeEmpty())
	})

	It("skips Tracks whose File is gone or missing", func() {
		drop("test.mp3", "Album/a.mp3", 1)
		Expect(os.Remove(filepath.Join(inbox, "Album", "a.mp3"))).To(Succeed())
		drop("test.mp3", "Other/b.mp3", 1)
		tracks[1].Missing = true
		mfs.SetData(tracks)
		settled()
		Expect(jobs()).To(BeEmpty())
	})

	It("matches each File against the Library, never against its own Track, and proposes its tags from its own", func() {
		drop("test.mp3", "Album/a.mp3", 1)
		other := model.RipMatch{ID: "mf-elsewhere", Title: "Song", Artist: "Artist", Library: "Music"}
		self := model.RipMatch{ID: "mf-Album/a.mp3", Title: "Song", Artist: "Artist", Library: "Inbox"}
		matcher.have["Artist|Song"] = model.RipMatches{self, other}
		go ripper.runProposals(ctx)
		settled()

		job := jobs()[0]
		Expect(job.Items[0].AlreadyHave).To(Equal(model.RipMatches{other}))
		Eventually(proposer.asked).Should(HaveLen(1))
		in := proposer.asked()[0]
		Expect(in.Artist).To(Equal("Artist"))
		Expect(in.Title).To(Equal("Song"))
		Expect(in.Album).To(Equal("Album"))
		Expect(in.Tagged).To(BeTrue())
	})

	It("is never picked up by the ripping worker", func() {
		drop("test.mp3", "Album/a.mp3", 1)
		settled()
		_, err := ds.RipJob(ctx).NextQueued()
		Expect(err).To(MatchError(model.ErrNotFound))
	})
})

var _ = Describe("fileInput", func() {
	It("reads a File without a title tag from its name", func() {
		dir := GinkgoT().TempDir()
		data, err := os.ReadFile(filepath.Join("tests", "fixtures", "rips", "untagged.opus"))
		Expect(err).ToNot(HaveOccurred())
		path := filepath.Join(dir, "03 - Daft Punk - One More Time.opus")
		Expect(os.WriteFile(path, data, 0o644)).To(Succeed())
		Expect(taglib.WriteTags(path, map[string][]string{}, taglib.Clear)).To(Succeed())

		in, err := fileInput(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(in.Tagged).To(BeFalse())
		Expect(in.Title).To(Equal("Daft Punk - One More Time"))
		Expect(newFileItem("job", 1, "x.opus", in, time.Now())).To(SatisfyAll(
			HaveField("Artist", "Daft Punk"), HaveField("Title", "One More Time")))
	})

	DescribeTable("titleFromName",
		func(name, want string) { Expect(titleFromName(name)).To(Equal(want)) },
		Entry("Artist - Title", "Adele - Hello.mp3", "Adele - Hello"),
		Entry("a track number", "01 Hello.mp3", "Hello"),
		Entry("a dotted track number", "1. Hello.flac", "Hello"),
		Entry("a dashed track number", "07 - Adele - Hello.m4a", "Adele - Hello"),
		Entry("underscores", "Adele_-_Hello.mp3", "Adele - Hello"),
		Entry("a number that is the title", "1979.mp3", "1979"),
	)
})
