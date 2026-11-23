package organize

import (
	"context"
	"strconv"
	"time"

	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/librarymanager"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// newInbox is the Inbox, a library of its own on a temp folder, beside l.
func newInbox(l *library) *library {
	GinkgoHelper()
	in := &library{ctx: l.ctx, ds: l.ds, dir: GinkgoT().TempDir()}
	in.lib = model.Library{Name: "Inbox", Path: in.dir}
	Expect(l.ds.Library(l.ctx).Put(&in.lib)).To(Succeed())
	Expect(l.ds.Property(l.ctx).Put("InboxLibraryID", strconv.Itoa(in.lib.ID))).To(Succeed())
	DeferCleanup(func() {
		_, _ = db.Db().Exec("delete from media_file")
		_, _ = db.Db().Exec("delete from folder where library_id = ?", in.lib.ID)
		_, _ = db.Db().Exec("delete from library where id = ?", in.lib.ID)
		_ = l.ds.Property(context.Background()).Delete("InboxLibraryID")
	})
	Expect(l.ds.Folder(l.ctx).Put(model.NewFolder(in.lib, "."))).To(Succeed())
	return in
}

// Promote (#171) is Organize from the Inbox: the Inbox's Tracks are planned to their scheme path
// in the Library, with the same rules, and moved there by the same job.
var _ = Describe("Promote", func() {
	var (
		l, inbox *library
		o        *Organizer
	)
	rip := func(title string, track int) model.MediaFile {
		return model.MediaFile{Title: title, Artist: "Rip Artist", AlbumArtist: "Rip Artist", Album: "Rip Album", Year: 2024, TrackNumber: track}
	}

	BeforeEach(func() {
		l = newLibrary()
		inbox = newInbox(l)
		o = New(l.ds, nil, nil)
	})

	plan := func(ids ...string) *Plan {
		GinkgoHelper()
		target, err := PromoteLibrary(l.ctx, l.ds)
		Expect(err).ToNot(HaveOccurred())
		Expect(target).ToNot(BeNil())
		p, err := o.Plan(l.ctx, Scope{Selection: librarymanager.Selection{IDs: ids}, Into: target.ID})
		Expect(err).ToNot(HaveOccurred())
		return p
	}

	Describe("PromoteLibrary", func() {
		It("is the writable library that isn't the Inbox", func() {
			got, err := PromoteLibrary(l.ctx, l.ds)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.ID).To(Equal(1))
		})

		It("is none while editing is off: Promote is off", func() {
			core.SetLibraryEditing(core.LibraryEditing{})
			got, err := PromoteLibrary(l.ctx, l.ds)
			Expect(err).ToNot(HaveOccurred())
			Expect(got).To(BeNil())
		})
	})

	It("plans each Inbox File to its scheme path in the Library, with its album's other Files", func() {
		a := inbox.track("Rip Album/01.opus", rip("Song", 1))
		b := inbox.track("Rip Album/02.opus", rip("Other", 2))
		inbox.file("Rip Album/cover.jpg", "image")
		p := plan(a.ID, b.ID)
		Expect(p.Unplaced).To(BeEmpty())
		Expect(p.Moves).To(ConsistOf(
			Row{TrackID: a.ID, LibraryID: inbox.lib.ID, NewLibraryID: 1, Path: "Rip Album/01.opus",
				NewPath: "Rip Artist/Rip Album (2024)/01 Song.opus", Title: "Song", Artist: "Rip Artist", Album: "Rip Album", Status: StatusMove},
			Row{TrackID: b.ID, LibraryID: inbox.lib.ID, NewLibraryID: 1, Path: "Rip Album/02.opus",
				NewPath: "Rip Artist/Rip Album (2024)/02 Other.opus", Title: "Other", Artist: "Rip Artist", Album: "Rip Album", Status: StatusMove},
			Row{LibraryID: inbox.lib.ID, NewLibraryID: 1, Path: "Rip Album/cover.jpg",
				NewPath: "Rip Artist/Rip Album (2024)/cover.jpg", Status: StatusFile},
		))
		Expect(p.Folders).To(Equal([]FolderMove{{LibraryID: inbox.lib.ID, NewLibraryID: 1,
			Path: "Rip Album", NewPath: "Rip Artist/Rip Album (2024)"}}))
	})

	It("moves a File even when its Inbox path is its scheme path: it still changes library", func() {
		a := inbox.track("Rip Artist/Rip Album (2024)/01 Song.opus", rip("Song", 1))
		p := plan(a.ID)
		Expect(p.InPlace).To(BeZero())
		Expect(p.Moves).To(HaveLen(1))
		Expect(p.Moves[0].NewPath).To(Equal("Rip Artist/Rip Album (2024)/01 Song.opus"))
	})

	It("reuses a Library folder whose name differs only in letter case", func() {
		l.track("rip artist/RIP ALBUM (2024)/09 Old.mp3", rip("Old", 9))
		a := inbox.track("Rip Album/01.opus", rip("Song", 1))
		Expect(plan(a.ID).Moves[0].NewPath).To(Equal("rip artist/RIP ALBUM (2024)/01 Song.opus"))
	})

	It("leaves a File whose place in the Library is taken, in any letter case", func() {
		l.track("Rip Artist/Rip Album (2024)/01 song.opus", rip("Song", 1))
		a := inbox.track("Rip Album/01.opus", rip("Song", 1))
		p := plan(a.ID)
		Expect(p.Moves).To(BeEmpty())
		Expect(p.Unplaced).To(HaveLen(1))
		Expect(p.Unplaced[0].Status).To(Equal(ReasonPathTaken))
	})

	It("leaves a File whose place goes through a Library folder with a case twin", func() {
		l.track("Rip Artist/Other (2001)/01 X.mp3", rip("X", 1))
		l.track("RIP ARTIST/Else (2002)/01 Y.mp3", rip("Y", 1))
		a := inbox.track("Rip Album/01.opus", rip("Song", 1))
		Expect(plan(a.ID).Unplaced[0].Status).To(Equal(ReasonCaseTwin))
	})

	It("leaves the Files it can't place, with why", func() {
		noArtist := rip("A", 1)
		noArtist.AlbumArtist = ""
		noNumber := rip("B", 0)
		noNumber.Album = "Other"
		a := inbox.track("x/a.opus", noArtist)
		b := inbox.track("y/b.opus", noNumber)
		p := plan(a.ID, b.ID)
		Expect(p.Moves).To(BeEmpty())
		reasons := map[string]string{}
		for _, u := range p.Unplaced {
			reasons[u.Path] = u.Status
		}
		Expect(reasons).To(Equal(map[string]string{"x/a.opus": ReasonNoAlbumArtist, "y/b.opus": ReasonNoTrackNumber}))
	})

	It("leaves every File while the Library is read-only", func() {
		a := inbox.track("Rip Album/01.opus", rip("Song", 1))
		target := model.Library{ID: 1}
		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Writable: false}}})
		p, err := o.Plan(l.ctx, Scope{Selection: librarymanager.Selection{IDs: []string{a.ID}}, Into: target.ID})
		Expect(err).ToNot(HaveOccurred())
		Expect(p.Unplaced[0].Status).To(Equal(ReasonEditingOff))
	})

	It("treats the Files the old Promote put in Vinyl Underground/ as ordinary Library Tracks, which Organize moves into the tree", func() {
		old := l.track("Vinyl Underground/Rip Artist/Rip Album (2024)/01 Song.opus", rip("Song", 1))
		p, err := o.Plan(l.ctx, Scope{Selection: librarymanager.Selection{IDs: []string{old.ID}}})
		Expect(err).ToNot(HaveOccurred())
		Expect(p.Moves).To(ConsistOf(And(HaveField("Path", old.Path), HaveField("NewLibraryID", 0),
			HaveField("NewPath", "Rip Artist/Rip Album (2024)/01 Song.opus"))))
	})

	Describe("the job", func() {
		var q *editjob.Queue
		BeforeEach(func() {
			editor := tagedit.New(l.ds, tests.NewMockScanner())
			q = editjob.New(l.ds, nil, editjob.Kinds(l.ds, editor))
			for kind, k := range JobKinds(l.ds, editor) {
				q.Handle(kind, k)
			}
			o = New(l.ds, q, nil)
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

		It("moves the Files into the Library as one Promote job, keeping each Track, and its undo puts them back in the Inbox", func() {
			a := inbox.track("Rip Album/01.opus", rip("Song", 1))
			inbox.file("Rip Album/cover.jpg", "image")
			scope := Scope{Selection: librarymanager.Selection{IDs: []string{a.ID}}, Into: 1}
			p, err := o.Plan(l.ctx, scope)
			Expect(err).ToNot(HaveOccurred())
			job, err := o.RunAs(l.ctx, model.EditPromote, scope, p.Token)
			Expect(err).ToNot(HaveOccurred())
			Expect(job.Kind).To(Equal(model.EditPromote))
			got := runQueue(job)
			Expect(got.Skipped).To(BeZero())

			Expect(l.files()).To(Equal(map[string]string{
				"Rip Artist/Rip Album (2024)/01 Song.opus": "audio of Rip Album/01.opus",
				"Rip Artist/Rip Album (2024)/cover.jpg":    "image",
			}))
			Expect(inbox.files()).To(BeEmpty())
			Expect(inbox.dirs()).To(BeEmpty(), "the emptied Inbox folder is removed")
			mf, err := l.ds.MediaFile(l.ctx).Get(a.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(mf.LibraryID).To(Equal(1))
			Expect(mf.Path).To(Equal("Rip Artist/Rip Album (2024)/01 Song.opus"))

			steps, err := editjob.UndoJobSteps(l.ctx, l.ds, job.ID)
			Expect(err).ToNot(HaveOccurred())
			undo, err := q.EnqueueWith(l.ctx, model.EditUndo, editjob.UndoJobArgs(job.ID), steps)
			Expect(err).ToNot(HaveOccurred())
			Expect(runQueue(undo).Skipped).To(BeZero())
			Expect(inbox.files()).To(Equal(map[string]string{
				"Rip Album/01.opus":   "audio of Rip Album/01.opus",
				"Rip Album/cover.jpg": "image",
			}))
			Expect(l.files()).To(BeEmpty())
			Expect(l.dirs()).To(BeEmpty(), "the Library folders it made are removed")
			mf, err = l.ds.MediaFile(l.ctx).Get(a.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(mf.LibraryID).To(Equal(inbox.lib.ID))
			Expect(mf.Path).To(Equal("Rip Album/01.opus"))
		})

		It("leaves in the Inbox the card's Files it doesn't place (not scanned yet) and hidden ones", func() {
			a := inbox.track("Rip Album/01.opus", rip("Song", 1))
			inbox.file("Rip Album/cover.jpg", "image")
			inbox.file("Rip Album/02.opus", "landed after the last scan")
			inbox.file("Rip Album/.03.opus.part", "still copying")
			scope := Scope{Selection: librarymanager.Selection{IDs: []string{a.ID}}, Into: 1,
				Keep: []string{"Rip Album/01.opus", "Rip Album/02.opus"}}
			p, err := o.Plan(l.ctx, scope)
			Expect(err).ToNot(HaveOccurred())
			job, err := o.RunAs(l.ctx, model.EditPromote, scope, p.Token)
			Expect(err).ToNot(HaveOccurred())
			Expect(runQueue(job).Skipped).To(BeZero())
			Expect(l.files()).To(Equal(map[string]string{
				"Rip Artist/Rip Album (2024)/01 Song.opus": "audio of Rip Album/01.opus",
				"Rip Artist/Rip Album (2024)/cover.jpg":    "image",
			}))
			Expect(inbox.files()).To(Equal(map[string]string{
				"Rip Album/02.opus":       "landed after the last scan",
				"Rip Album/.03.opus.part": "still copying",
			}))
		})
	})
})
