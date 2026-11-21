package rip

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// scanRecorder records the Inbox scans a review sets off in the background.
type scanRecorder struct {
	mu      sync.Mutex
	targets [][]model.ScanTarget
}

func (s *scanRecorder) ScanFolders(_ context.Context, _ bool, targets []model.ScanTarget) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.targets = append(s.targets, targets)
	return nil, nil
}

func (s *scanRecorder) scans() [][]model.ScanTarget {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]model.ScanTarget(nil), s.targets...)
}

var _ = Describe("Review and Discard", func() {
	var (
		ctx     context.Context
		ds      *tests.MockDataStore
		inbox   string
		scanner *scanRecorder
		ripper  *Ripper
	)

	const inboxID = 2

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
		ds = &tests.MockDataStore{MockedLibrary: libs}
		_, err := core.EnsureInboxLibrary(ctx, ds, inbox)
		Expect(err).ToNot(HaveOccurred())
		scanner = &scanRecorder{}
		ripper = New(ds, newFakeFetcher(), scanner, Options{ScanRetryDelay: time.Millisecond})
	})

	// landed puts a copy of a Rip fixture into the Inbox at rel, as a ripped item would be.
	landed := func(fixture, rel string) {
		data, err := os.ReadFile(filepath.Join("tests", "fixtures", "rips", fixture))
		Expect(err).ToNot(HaveOccurred())
		full := filepath.Join(inbox, filepath.FromSlash(rel))
		Expect(os.MkdirAll(filepath.Dir(full), 0o755)).To(Succeed())
		Expect(os.WriteFile(full, data, 0o644)).To(Succeed())
	}

	// newJob saves a job with an item per path ("" for a failed item).
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

	exists := func(rel string) bool {
		_, err := os.Lstat(filepath.Join(inbox, filepath.FromSlash(rel)))
		return err == nil
	}

	Describe("Review", func() {
		It("shows the job's Tracks with the tags in their Files, and the album from the first", func() {
			landed("ripped.opus", "Album/a.opus")
			landed("ripped.m4a", "Album/b.m4a")
			job := newJob(model.RipReady, "Album", "Album/a.opus", "", "Album/b.m4a")

			rev, err := ripper.Review(ctx, job.ID)
			Expect(err).ToNot(HaveOccurred())

			Expect(rev.Album).To(Equal("Rip Album"))
			Expect(rev.Tracks).To(HaveLen(3))
			Expect(rev.Tracks[0].Title).To(Equal("Ripped Opus"))
			Expect(rev.Tracks[0].Artist).To(Equal("Rip Artist"))
			Expect(rev.Tracks[1].Status).To(Equal(model.RipFailed))
			Expect(rev.Tracks[1].Error).To(Equal("ERROR: Video unavailable"))
			Expect(rev.Tracks[1].Title).To(BeEmpty())
			Expect(rev.Tracks[2].Title).To(Equal("Ripped M4A"))
		})

		It("says which Tracks the Library already seemed to have when they were queued (#32)", func() {
			landed("ripped.opus", "Album/a.opus")
			landed("ripped.m4a", "Album/b.m4a")
			job := newJob(model.RipReady, "Album", "Album/a.opus", "Album/b.m4a")
			have := model.RipMatches{{ID: "mf-1", Title: "Ripped M4A", Artist: "Rip Artist", Library: "Music"}}
			job.Items[1].AlreadyHave = have
			Expect(ds.RipItem(ctx).Put(&job.Items[1])).To(Succeed())

			rev, err := ripper.Review(ctx, job.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(rev.Tracks[0].AlreadyHave).To(BeEmpty())
			Expect(rev.Tracks[1].AlreadyHave).To(Equal(have))
		})

		Describe("tag proposals (#34)", func() {
			propose := func(it model.RipItem, p model.RipProposal) {
				Expect(ds.RipItem(ctx).SetProposal(it.ID, p)).To(Succeed())
			}
			ready := func(album, albumArtist, year string) model.RipProposal {
				return model.RipProposal{Status: model.RipProposalReady, Title: "T", Artist: "A",
					Album: album, AlbumArtist: albumArtist, Year: year, AlbumFrom: "musicbrainz"}
			}

			It("shows each Track's proposal next to its tags, and the album's when the Tracks agree", func() {
				landed("ripped.opus", "Album/a.opus")
				landed("ripped.m4a", "Album/b.m4a")
				job := newJob(model.RipReady, "Album", "Album/a.opus", "Album/b.m4a")
				propose(job.Items[0], ready("Record", "Band", "1999"))
				propose(job.Items[1], ready("Record", "Band", "1999"))

				rev, err := ripper.Review(ctx, job.ID)
				Expect(err).ToNot(HaveOccurred())
				Expect(rev.Tracks[0].Proposal).To(HaveField("Album", "Record"))
				Expect(rev.Tracks[0].Title).To(Equal("Ripped Opus"), "the File's own tag is still shown")
				Expect(rev.Proposal).To(Equal(&AlbumProposal{Album: "Record", AlbumArtist: "Band", Year: "1999"}))
				Expect(rev.ProposalsPending).To(BeFalse())
			})

			It("proposes no album-level value the Tracks disagree on", func() {
				landed("ripped.opus", "Album/a.opus")
				landed("ripped.m4a", "Album/b.m4a")
				job := newJob(model.RipReady, "Album", "Album/a.opus", "Album/b.m4a")
				propose(job.Items[0], ready("Record", "Band", "1999"))
				propose(job.Items[1], ready("Other Record", "Band", "1999"))

				rev, err := ripper.Review(ctx, job.ID)
				Expect(err).ToNot(HaveOccurred())
				Expect(rev.Proposal).To(Equal(&AlbumProposal{AlbumArtist: "Band", Year: "1999"}))
			})

			It("says when proposals are still being worked out", func() {
				landed("ripped.opus", "Album/a.opus")
				job := newJob(model.RipReady, "Album", "Album/a.opus")
				propose(job.Items[0], model.RipProposal{Status: model.RipProposalPending})

				rev, err := ripper.Review(ctx, job.ID)
				Expect(err).ToNot(HaveOccurred())
				Expect(rev.ProposalsPending).To(BeTrue())
				Expect(rev.Proposal).To(BeNil())
			})

			It("has no proposals for a job that has none", func() {
				landed("ripped.opus", "Album/a.opus")
				job := newJob(model.RipReady, "Album", "Album/a.opus")
				rev, err := ripper.Review(ctx, job.ID)
				Expect(err).ToNot(HaveOccurred())
				Expect(rev.Tracks[0].Proposal).To(BeNil())
				Expect(rev.Proposal).To(BeNil())
			})
		})

		It("still shows the card when a File is gone, with that Track's error, and saves the others", func() {
			landed("ripped.opus", "Album/a.opus")
			job := newJob(model.RipReady, "Album", "Album/a.opus", "Album/gone.opus")

			rev, err := ripper.Review(ctx, job.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(rev.Tracks[1].Error).To(Equal(errFileUnreadable))
			Expect(rev.Tracks[1].Path).To(BeEmpty())

			_, err = ripper.SaveTags(ctx, job.ID, ReviewEdits{Album: str("Still Saved")})
			Expect(err).ToNot(HaveOccurred())
			Expect(readTags(filepath.Join(inbox, "Album", "a.opus"))).To(HaveKeyWithValue("ALBUM", []string{"Still Saved"}))
		})

		It("isn't there for a job still downloading", func() {
			job := newJob(model.RipDownloading, "Album", "Album/a.opus")
			_, err := ripper.Review(ctx, job.ID)
			Expect(err).To(MatchError(ErrNotReady))
		})
	})

	Describe("SaveTags", func() {
		It("writes the album-level tags into every File and each Track's own into its File, then rescans", func() {
			landed("ripped.opus", "Album/a.opus")
			landed("ripped.mp3", "Album/b.mp3")
			job := newJob(model.RipReady, "Album", "Album/a.opus", "Album/b.mp3")

			rev, err := ripper.SaveTags(ctx, job.ID, ReviewEdits{
				Album: str("Real Album"), AlbumArtist: str("Real Band"), Year: str("1997"),
				Tracks: []TrackEdit{
					{ItemID: job.Items[0].ID, Title: str("One"), TrackNumber: str("1")},
					{ItemID: job.Items[1].ID, Title: str("Two"), Artist: str("Guest"), TrackNumber: str("2")},
				},
			})
			Expect(err).ToNot(HaveOccurred())

			Expect(rev.Album).To(Equal("Real Album"))
			Expect(rev.Tracks[1].FileTags).To(Equal(FileTags{Title: "Two", Artist: "Guest", Album: "Real Album",
				AlbumArtist: "Real Band", Year: "1997", TrackNumber: "2"}))
			a := readTags(filepath.Join(inbox, "Album", "a.opus"))
			Expect(a).To(HaveKeyWithValue("ALBUM", []string{"Real Album"}))
			Expect(a).To(HaveKeyWithValue("TITLE", []string{"One"}))
			Expect(a).To(HaveKeyWithValue("ARTIST", []string{"Rip Artist"}))
			Expect(a).To(HaveKeyWithValue("SOURCE", []string{"YouTube"}))
			Eventually(scanner.scans).Should(ContainElement([]model.ScanTarget{{LibraryID: inboxID}}))
		})

		It("refuses edits while the job is still downloading, and writes nothing", func() {
			landed("ripped.opus", "Album/a.opus")
			job := newJob(model.RipDownloading, "Album", "Album/a.opus")

			_, err := ripper.SaveTags(ctx, job.ID, ReviewEdits{Album: str("Nope")})
			Expect(err).To(MatchError(ErrNotReady))
			Expect(readTags(filepath.Join(inbox, "Album", "a.opus"))).To(HaveKeyWithValue("ALBUM", []string{"Rip Album"}))
		})

		It("checks every edit before writing any File", func() {
			landed("ripped.opus", "Album/a.opus")
			landed("ripped.opus", "Album/b.opus")
			job := newJob(model.RipReady, "Album", "Album/a.opus", "Album/b.opus")

			_, err := ripper.SaveTags(ctx, job.ID, ReviewEdits{Tracks: []TrackEdit{
				{ItemID: job.Items[0].ID, Title: str("Fine")},
				{ItemID: job.Items[1].ID, TrackNumber: str("two")},
			}})
			Expect(err).To(MatchError(ErrInvalidTags))
			Expect(readTags(filepath.Join(inbox, "Album", "a.opus"))).To(HaveKeyWithValue("TITLE", []string{"Ripped Opus"}))

			_, err = ripper.SaveTags(ctx, job.ID, ReviewEdits{Tracks: []TrackEdit{{ItemID: "someone-elses", Title: str("X")}}})
			Expect(err).To(MatchError(model.ErrNotFound))
		})

		It("never writes a File outside the Inbox", func() {
			outside := GinkgoT().TempDir()
			data, _ := os.ReadFile(filepath.Join("tests", "fixtures", "rips", "ripped.opus"))
			Expect(os.WriteFile(filepath.Join(outside, "x.opus"), data, 0o644)).To(Succeed())
			Expect(os.Symlink(outside, filepath.Join(inbox, "Elsewhere"))).To(Succeed())
			job := newJob(model.RipReady, "", "Elsewhere/x.opus")

			_, err := ripper.SaveTags(ctx, job.ID, ReviewEdits{Album: str("Hijacked")})
			Expect(err).To(MatchError(ErrOutsideInbox))
			Expect(readTags(filepath.Join(outside, "x.opus"))).To(HaveKeyWithValue("ALBUM", []string{"Rip Album"}))
		})

		It("keeps the Files it saved, and leaves the one it couldn't as it was", func() {
			landed("ripped.opus", "Album/a.opus")
			landed("ripped.opus", "Album/b.opus")
			job := newJob(model.RipReady, "Album", "Album/a.opus", "Album/b.opus")
			original, _ := os.ReadFile(filepath.Join(inbox, "Album", "b.opus"))
			calls := 0
			ripper.writeTags = func(path string, tags map[string][]string) error {
				calls++
				if calls == 2 {
					return errors.New("disk full")
				}
				return writeTagsKeepingOthers(path, tags)
			}

			_, err := ripper.SaveTags(ctx, job.ID, ReviewEdits{Album: str("Half")})
			Expect(err).To(MatchError(ContainSubstring("disk full")))

			Expect(readTags(filepath.Join(inbox, "Album", "a.opus"))).To(HaveKeyWithValue("ALBUM", []string{"Half"}))
			Expect(os.ReadFile(filepath.Join(inbox, "Album", "b.opus"))).To(Equal(original))
			Expect(visibleFiles(filepath.Join(inbox, "Album"))).To(ConsistOf("a.opus", "b.opus"))
			entries, _ := os.ReadDir(filepath.Join(inbox, "Album"))
			Expect(entries).To(HaveLen(2), "no temp File left behind")
		})
	})

	Describe("Discard", func() {
		It("deletes all the job's Files, its empty folder and the job, then rescans", func() {
			landed("ripped.opus", "Album/a.opus")
			landed("ripped.m4a", "Album/b.m4a")
			landed("untagged.opus", "keep.opus")
			job := newJob(model.RipReady, "Album", "Album/a.opus", "", "Album/b.m4a")

			Expect(ripper.Discard(ctx, job.ID)).To(Succeed())

			Expect(exists("Album")).To(BeFalse())
			Expect(exists("keep.opus")).To(BeTrue())
			_, err := ds.RipJob(ctx).Get(job.ID)
			Expect(err).To(MatchError(model.ErrNotFound))
			Expect(ds.RipItem(ctx).ForJobs(job.ID)).To(BeEmpty())
			Eventually(scanner.scans).Should(HaveLen(1))
		})

		It("keeps the folder when something else is in it", func() {
			landed("ripped.opus", "Album/a.opus")
			landed("untagged.opus", "Album/dropped by hand.opus")
			job := newJob(model.RipReady, "Album", "Album/a.opus")

			Expect(ripper.Discard(ctx, job.ID)).To(Succeed())

			Expect(exists("Album/a.opus")).To(BeFalse())
			Expect(exists("Album/dropped by hand.opus")).To(BeTrue())
		})

		It("deletes nothing, and keeps the job, when the Inbox folder can't be reached", func() {
			landed("ripped.opus", "Album/a.opus")
			job := newJob(model.RipReady, "Album", "Album/a.opus")
			Expect(os.Rename(inbox, inbox+"-unmounted")).To(Succeed())
			DeferCleanup(os.Rename, inbox+"-unmounted", inbox)

			Expect(ripper.Discard(ctx, job.ID)).To(MatchError(ErrNoInbox))
			Expect(ripper.DiscardItem(ctx, job.ID, job.Items[0].ID)).To(MatchError(ErrNoInbox))

			Expect(ds.RipJob(ctx).Get(job.ID)).ToNot(BeNil())
			Expect(ds.RipItem(ctx).ForJobs(job.ID)).To(HaveLen(1))
			Expect(filepath.Join(inbox+"-unmounted", "Album", "a.opus")).To(BeAnExistingFile())
		})

		It("deletes a single video's File, and never the Inbox itself", func() {
			landed("ripped.opus", "Song.opus")
			job := newJob(model.RipReady, "", "Song.opus")

			Expect(ripper.Discard(ctx, job.ID)).To(Succeed())

			Expect(exists("Song.opus")).To(BeFalse())
			Expect(inbox).To(BeADirectory())
		})

		It("refuses a job still queued or downloading, and deletes nothing", func() {
			landed("ripped.opus", "Album/a.opus")
			job := newJob(model.RipDownloading, "Album", "Album/a.opus")

			Expect(ripper.Discard(ctx, job.ID)).To(MatchError(ErrNotReady))
			Expect(exists("Album/a.opus")).To(BeTrue())
			Expect(ds.RipJob(ctx).Get(job.ID)).ToNot(BeNil())
		})

		It("deletes nothing at all if any File is outside the Inbox", func() {
			outside := GinkgoT().TempDir()
			Expect(os.WriteFile(filepath.Join(outside, "x.opus"), []byte("x"), 0o644)).To(Succeed())
			Expect(os.Symlink(outside, filepath.Join(inbox, "Elsewhere"))).To(Succeed())
			landed("ripped.opus", "a.opus")
			for _, bad := range []string{"Elsewhere/x.opus", "../" + filepath.Base(outside) + "/x.opus"} {
				job := newJob(model.RipReady, "", "a.opus", bad)

				Expect(ripper.Discard(ctx, job.ID)).To(MatchError(ErrOutsideInbox))

				Expect(exists("a.opus")).To(BeTrue())
				Expect(filepath.Join(outside, "x.opus")).To(BeAnExistingFile())
			}
		})

		It("doesn't remove a job folder that leads out of the Inbox", func() {
			outside := GinkgoT().TempDir()
			empty := filepath.Join(outside, "empty")
			Expect(os.Mkdir(empty, 0o755)).To(Succeed())
			job := newJob(model.RipFailed, "../"+filepath.Base(outside)+"/empty")

			Expect(ripper.Discard(ctx, job.ID)).To(Succeed())
			Expect(empty).To(BeADirectory())
		})
	})

	Describe("DiscardItem", func() {
		It("deletes one Track's File and item, and keeps the rest of the job", func() {
			landed("ripped.opus", "Album/a.opus")
			landed("ripped.m4a", "Album/b.m4a")
			job := newJob(model.RipReady, "Album", "Album/a.opus", "Album/b.m4a")

			Expect(ripper.DiscardItem(ctx, job.ID, job.Items[0].ID)).To(Succeed())

			Expect(exists("Album/a.opus")).To(BeFalse())
			Expect(exists("Album/b.m4a")).To(BeTrue())
			items, _ := ds.RipItem(ctx).ForJobs(job.ID)
			Expect(items).To(HaveLen(1))
			Expect(items[0].ID).To(Equal(job.Items[1].ID))
			Expect(ds.RipJob(ctx).Get(job.ID)).ToNot(BeNil())
			Eventually(scanner.scans).Should(HaveLen(1))
		})

		It("discards the whole job once its last File is gone", func() {
			landed("ripped.opus", "Album/a.opus")
			job := newJob(model.RipReady, "Album", "Album/a.opus", "")

			Expect(ripper.DiscardItem(ctx, job.ID, job.Items[0].ID)).To(Succeed())

			Expect(exists("Album")).To(BeFalse())
			_, err := ds.RipJob(ctx).Get(job.ID)
			Expect(err).To(MatchError(model.ErrNotFound))
			Expect(ds.RipItem(ctx).ForJobs(job.ID)).To(BeEmpty())
		})

		It("updates the job's note when a failed Track is discarded", func() {
			landed("ripped.opus", "a.opus")
			job := newJob(model.RipReady, "", "a.opus", "", "")
			job.Error = "2 of 3 Tracks failed"
			Expect(ds.RipJob(ctx).Put(job)).To(Succeed())

			Expect(ripper.DiscardItem(ctx, job.ID, job.Items[1].ID)).To(Succeed())
			j, _ := ds.RipJob(ctx).Get(job.ID)
			Expect(j.Error).To(Equal("1 of 2 Tracks failed"))

			Expect(ripper.DiscardItem(ctx, job.ID, job.Items[2].ID)).To(Succeed())
			j, _ = ds.RipJob(ctx).Get(job.ID)
			Expect(j.Error).To(BeEmpty())
			Expect(exists("a.opus")).To(BeTrue())
		})

		It("only discards an item of that job", func() {
			landed("ripped.opus", "a.opus")
			other := newJob(model.RipReady, "", "a.opus")
			job := newJob(model.RipReady, "", "")

			Expect(ripper.DiscardItem(ctx, job.ID, other.Items[0].ID)).To(MatchError(model.ErrNotFound))
			Expect(exists("a.opus")).To(BeTrue())
		})

		It("refuses while the job is still downloading", func() {
			landed("ripped.opus", "a.opus")
			job := newJob(model.RipDownloading, "", "a.opus")

			Expect(ripper.DiscardItem(ctx, job.ID, job.Items[0].ID)).To(MatchError(ErrNotReady))
			Expect(exists("a.opus")).To(BeTrue())
		})
	})
})
