package tagedit

import (
	"context"
	"os"
	"path/filepath"
	"strconv"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/safefile"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/persistence"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Promote (#171) is a move from the Inbox, a library of its own, into the Library: the File
// changes library as well as path, and its Track keeps its ID.
var _ = Describe("Moves into another library (Promote)", func() {
	var (
		ctx              context.Context
		ds               model.DataStore
		editor           *Editor
		libDir, inboxDir string
		inbox            model.Library
	)

	put := func(root, rel, content string) {
		GinkgoHelper()
		abs := filepath.Join(root, filepath.FromSlash(rel))
		Expect(os.MkdirAll(filepath.Dir(abs), 0o755)).To(Succeed())
		Expect(os.WriteFile(abs, []byte(content), 0o644)).To(Succeed())
	}
	scanned := func(id string, lib model.Library, rel string) {
		GinkgoHelper()
		info, err := os.Stat(filepath.Join(lib.Path, filepath.FromSlash(rel)))
		Expect(err).ToNot(HaveOccurred())
		mf := model.MediaFile{ID: id, LibraryID: lib.ID, Path: rel, FolderID: model.FolderID(lib, filepath.Dir(rel)),
			Size: info.Size(), UpdatedAt: info.ModTime(), Participants: model.Participants{}}
		Expect(ds.MediaFile(ctx).Put(&mf)).To(Succeed())
	}
	track := func(id string) *model.MediaFile {
		GinkgoHelper()
		mf, err := ds.MediaFile(ctx).Get(id)
		Expect(err).ToNot(HaveOccurred())
		return mf
	}
	entry := func(id string) *model.EditEntry {
		GinkgoHelper()
		e, err := ds.EditJournal(ctx).Get(id)
		Expect(err).ToNot(HaveOccurred())
		return e
	}
	files := func(root string) map[string]string {
		GinkgoHelper()
		res := map[string]string{}
		Expect(filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, _ := os.ReadFile(p)
			rel, _ := filepath.Rel(root, p)
			res[filepath.ToSlash(rel)] = string(b)
			return nil
		})).To(Succeed())
		return res
	}

	BeforeEach(func() {
		ctx = request.WithUser(context.Background(), model.User{ID: "admin-1", UserName: "admin", IsAdmin: true})
		ds = persistence.New(db.Db())
		for _, table := range []string{"media_file", "folder", "album", "edit_journal"} {
			_, err := db.Db().Exec("delete from " + table)
			Expect(err).ToNot(HaveOccurred())
		}
		libDir, inboxDir = GinkgoT().TempDir(), GinkgoT().TempDir()
		_, err := db.Db().Exec("update library set path = ? where id = 1", libDir)
		Expect(err).ToNot(HaveOccurred())
		inbox = model.Library{Name: "Inbox", Path: inboxDir}
		Expect(ds.Library(ctx).Put(&inbox)).To(Succeed())
		Expect(ds.Property(ctx).Put("InboxLibraryID", strconv.Itoa(inbox.ID))).To(Succeed())
		DeferCleanup(func() {
			_, _ = db.Db().Exec("delete from media_file")
			_, _ = db.Db().Exec("delete from library where id = ?", inbox.ID)
			_ = ds.Property(context.Background()).Delete("InboxLibraryID")
		})
		DeferCleanup(configtest.SetupConfig())
		conf.Server.DataFolder = conf.NewDir(GinkgoT().TempDir())
		// The Inbox is left out of the check (core.DetectLibraryEditing).
		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Name: "NAS", Writable: true}}})
		DeferCleanup(func() { core.SetLibraryEditing(core.LibraryEditing{}) })
		editor = New(ds, tests.NewMockScanner())

		put(inboxDir, "Rip Album/01.opus", "audio")
		scanned("mf-1", inbox, "Rip Album/01.opus")
	})

	promote := func() (*Result, error) {
		return editor.MoveInJob(ctx, "job-1", Move{LibraryID: inbox.ID, NewLibraryID: 1, TrackID: "mf-1",
			Path: "Rip Album/01.opus", NewPath: "Rip Artist/Rip Album (2024)/01 Song.opus"})
	}

	It("moves the File into the Library, keeping the Track's ID, and journals both libraries", func() {
		res, err := promote()
		Expect(err).ToNot(HaveOccurred())
		Expect(files(libDir)).To(Equal(map[string]string{"Rip Artist/Rip Album (2024)/01 Song.opus": "audio"}))
		Expect(files(inboxDir)).To(BeEmpty())

		mf := track("mf-1")
		Expect(mf.LibraryID).To(Equal(1))
		Expect(mf.Path).To(Equal("Rip Artist/Rip Album (2024)/01 Song.opus"))
		Expect(mf.FolderID).To(Equal(model.FolderID(model.Library{ID: 1, Path: libDir}, "Rip Artist/Rip Album (2024)")))

		e := entry(res.Entry)
		Expect(e.State).To(Equal(model.EditWritten))
		Expect(e.LibraryID).To(Equal(inbox.ID))
		Expect(e.NewLibraryID).To(Equal(1))
		Expect(e.DestLibraryID()).To(Equal(1))
		Expect(e.Path).To(Equal("Rip Album/01.opus"))
		Expect(e.NewPath).To(Equal("Rip Artist/Rip Album (2024)/01 Song.opus"))
	})

	It("copies, checks and deletes the File when the Inbox is another filesystem, and undoes it the same way", func() {
		editor.fsys = exdevFS{safefile.OS{}}
		res, err := promote()
		Expect(err).ToNot(HaveOccurred())
		Expect(files(libDir)).To(Equal(map[string]string{"Rip Artist/Rip Album (2024)/01 Song.opus": "audio"}))
		Expect(files(inboxDir)).To(BeEmpty())
		Expect(safefile.IsRenameMark(entry(res.Entry).CopySum)).To(BeFalse(), "told by its checksum")
		Expect(track("mf-1").LibraryID).To(Equal(1))

		_, err = editor.Undo(ctx, res.Entry)
		Expect(err).ToNot(HaveOccurred())
		Expect(files(inboxDir)).To(Equal(map[string]string{"Rip Album/01.opus": "audio"}))
		Expect(track("mf-1").LibraryID).To(Equal(inbox.ID))
	})

	It("reuses a Library folder whose name differs only in letter case", func() {
		put(libDir, "rip artist/Other (2001)/01 X.opus", "x")
		res, err := promote()
		Expect(err).ToNot(HaveOccurred())
		Expect(entry(res.Entry).NewPath).To(Equal("rip artist/Rip Album (2024)/01 Song.opus"))
		Expect(track("mf-1").Path).To(Equal("rip artist/Rip Album (2024)/01 Song.opus"))
	})

	It("never moves over a File in the Library", func() {
		put(libDir, "Rip Artist/Rip Album (2024)/01 song.OPUS", "other")
		_, err := promote()
		Expect(err).To(MatchError(safefile.ErrDestTaken))
		Expect(files(inboxDir)).To(Equal(map[string]string{"Rip Album/01.opus": "audio"}))
		Expect(track("mf-1").LibraryID).To(Equal(inbox.ID))
	})

	It("is off while the Library is read-only", func() {
		core.SetLibraryEditing(core.LibraryEditing{Enabled: false})
		_, err := promote()
		Expect(err).To(MatchError(ErrEditingOff))
		Expect(editor.CheckMove(ctx, Move{LibraryID: inbox.ID, NewLibraryID: 1, TrackID: "mf-1"})).To(MatchError(ErrEditingOff))
	})

	It("moves only between the Inbox and a writable library", func() {
		other := model.Library{Name: "Other", Path: GinkgoT().TempDir()}
		Expect(ds.Library(ctx).Put(&other)).To(Succeed())
		DeferCleanup(func() { _, _ = db.Db().Exec("delete from library where id = ?", other.ID) })
		Expect(editor.CheckMove(ctx, Move{LibraryID: other.ID, NewLibraryID: 1})).To(MatchError(ErrEditingOff))
		Expect(editor.CheckMove(ctx, Move{LibraryID: inbox.ID, NewLibraryID: other.ID})).To(MatchError(ErrEditingOff))
		Expect(editor.CheckMove(ctx, Move{LibraryID: inbox.ID, NewLibraryID: 1, TrackID: "mf-1"})).To(Succeed())
	})

	It("is undone back into the Inbox, the Track with it; a redo promotes it again", func() {
		res, err := promote()
		Expect(err).ToNot(HaveOccurred())
		Expect(safefile.RemoveEmptyDirs(safefile.OS{}, inboxDir, "Rip Album")).To(HaveLen(1))

		undo, err := editor.Undo(ctx, res.Entry)
		Expect(err).ToNot(HaveOccurred())
		Expect(files(inboxDir)).To(Equal(map[string]string{"Rip Album/01.opus": "audio"}))
		Expect(files(libDir)).To(BeEmpty())
		mf := track("mf-1")
		Expect(mf.LibraryID).To(Equal(inbox.ID))
		Expect(mf.Path).To(Equal("Rip Album/01.opus"))
		u := entry(undo.Entry)
		Expect(u.LibraryID).To(Equal(1))
		Expect(u.NewLibraryID).To(Equal(inbox.ID))

		_, err = editor.Undo(ctx, res.Entry)
		Expect(err).To(MatchError(ErrAlreadyUndone))

		_, err = editor.Undo(ctx, undo.Entry)
		Expect(err).ToNot(HaveOccurred())
		Expect(track("mf-1").LibraryID).To(Equal(1))
		Expect(files(inboxDir)).To(BeEmpty())
	})

	It("refuses an undo while a later move of the File in the Library stands", func() {
		res, err := promote()
		Expect(err).ToNot(HaveOccurred())
		scanned("mf-1", model.Library{ID: 1, Path: libDir}, "Rip Artist/Rip Album (2024)/01 Song.opus")
		_, err = editor.MoveInJob(ctx, "job-2", Move{LibraryID: 1, TrackID: "mf-1",
			Path: "Rip Artist/Rip Album (2024)/01 Song.opus", NewPath: "Rip Artist/Other/01 Song.opus"})
		Expect(err).ToNot(HaveOccurred())
		_, err = editor.Undo(ctx, res.Entry)
		Expect(err).To(MatchError(ErrChangedLater))
	})

	Describe("Settle", func() {
		pending := func(sum string) *model.EditEntry {
			GinkgoHelper()
			e := &model.EditEntry{JobID: "job-1", Kind: model.EditMove, State: model.EditPending, LibraryID: inbox.ID,
				NewLibraryID: 1, MediaFileID: "mf-1", Path: "Rip Album/01.opus",
				NewPath: "Rip Artist/Rip Album (2024)/01 Song.opus", CopySum: sum}
			Expect(ds.EditJournal(ctx).Put(e)).To(Succeed())
			return e
		}

		It("finishes a Promote stopped once the copy was placed in the Library: the Inbox copy goes, the Track follows", func() {
			e := pending(sumOfBytes([]byte("audio")))
			put(libDir, "Rip Artist/Rip Album (2024)/01 Song.opus", "audio")
			editor.Settle(ctx)
			Expect(files(inboxDir)).To(BeEmpty())
			Expect(files(libDir)).To(Equal(map[string]string{"Rip Artist/Rip Album (2024)/01 Song.opus": "audio"}))
			mf := track("mf-1")
			Expect(mf.LibraryID).To(Equal(1))
			Expect(mf.Path).To(Equal("Rip Artist/Rip Album (2024)/01 Song.opus"))
			Expect(entry(e.ID).State).To(Equal(model.EditWritten))
		})

		It("undoes a Promote stopped while copying: the File stays in the Inbox", func() {
			e := pending("")
			put(libDir, "Rip Artist/Rip Album (2024)/"+filepath.Base(safefile.MoveTemp("01 Song.opus")), "aud")
			editor.Settle(ctx)
			Expect(files(inboxDir)).To(Equal(map[string]string{"Rip Album/01.opus": "audio"}))
			Expect(files(libDir)).To(BeEmpty())
			Expect(track("mf-1").LibraryID).To(Equal(inbox.ID))
			Expect(entry(e.ID).State).To(Equal(model.EditFailed))
		})
	})
})
