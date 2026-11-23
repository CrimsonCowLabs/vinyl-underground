package tagedit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"

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

// exdevFS can't rename a File into another folder, as across two mounts (EXDEV); a move's temp
// renames still work.
type exdevFS struct{ safefile.FS }

func (f exdevFS) Rename(oldname, newname string) error {
	if !strings.Contains(oldname, "vu-move") {
		return &os.LinkError{Op: "rename", Old: oldname, New: newname, Err: syscall.EXDEV}
	}
	return f.FS.Rename(oldname, newname)
}

var _ = Describe("Moves", func() {
	var (
		ctx    context.Context
		ds     model.DataStore
		editor *Editor
		libDir string
		lib    model.Library
	)

	// put writes a File into the library at rel.
	put := func(rel, content string) string {
		GinkgoHelper()
		abs := filepath.Join(libDir, filepath.FromSlash(rel))
		Expect(os.MkdirAll(filepath.Dir(abs), 0o755)).To(Succeed())
		Expect(os.WriteFile(abs, []byte(content), 0o644)).To(Succeed())
		return abs
	}
	// scanned records the File at rel as the Track id, as a scan would.
	scanned := func(id, rel string) {
		GinkgoHelper()
		info, err := os.Stat(filepath.Join(libDir, filepath.FromSlash(rel)))
		Expect(err).ToNot(HaveOccurred())
		mf := model.MediaFile{ID: id, LibraryID: 1, Path: rel, FolderID: model.FolderID(lib, filepath.Dir(rel)),
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
	files := func() map[string]string {
		GinkgoHelper()
		res := map[string]string{}
		Expect(filepath.WalkDir(libDir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, _ := os.ReadFile(p)
			rel, _ := filepath.Rel(libDir, p)
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
		libDir = GinkgoT().TempDir()
		_, err := db.Db().Exec("update library set path = ? where id = 1", libDir)
		Expect(err).ToNot(HaveOccurred())
		lib = model.Library{ID: 1, Path: libDir}
		DeferCleanup(configtest.SetupConfig())
		conf.Server.DataFolder = conf.NewDir(GinkgoT().TempDir())
		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Name: "NAS", Writable: true}}})
		DeferCleanup(func() { core.SetLibraryEditing(core.LibraryEditing{}) })
		editor = New(ds, tests.NewMockScanner())

		put("iTunes/Band/Record/a.mp3", "audio")
		scanned("mf-1", "iTunes/Band/Record/a.mp3")
	})

	move := func(trackID, from, to string) (*Result, error) {
		return editor.MoveInJob(ctx, "job-1", Move{LibraryID: 1, TrackID: trackID, Path: from, NewPath: to})
	}

	Describe("MoveInJob", func() {
		It("moves a Track's File, keeping the Track's ID, pointed at its new path and folder, and journals the move", func() {
			res, err := move("mf-1", "iTunes/Band/Record/a.mp3", "Band/Record (1999)/01 A.mp3")
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Changed).To(BeTrue())
			Expect(files()).To(Equal(map[string]string{"Band/Record (1999)/01 A.mp3": "audio"}))

			mf := track("mf-1")
			Expect(mf.Path).To(Equal("Band/Record (1999)/01 A.mp3"))
			Expect(mf.FolderID).To(Equal(model.FolderID(lib, "Band/Record (1999)")))

			e := entry(res.Entry)
			Expect(e.Kind).To(Equal(model.EditMove))
			Expect(e.State).To(Equal(model.EditWritten))
			Expect(e.JobID).To(Equal("job-1"))
			Expect(e.UserID).To(Equal("admin-1"))
			Expect(e.MediaFileID).To(Equal("mf-1"))
			Expect(e.Path).To(Equal("iTunes/Band/Record/a.mp3"))
			Expect(e.NewPath).To(Equal("Band/Record (1999)/01 A.mp3"))
			// On one share the File is renamed, and told by its size and time.
			Expect(safefile.IsRenameMark(e.CopySum)).To(BeTrue())
			Expect(e.CopySum).To(HavePrefix("moved:5:"))
		})

		It("copies, checks and deletes the File across filesystems, told by its checksum; its undo works too", func() {
			editor.fsys = exdevFS{safefile.OS{}}
			res, err := move("mf-1", "iTunes/Band/Record/a.mp3", "Band/Record (1999)/01 A.mp3")
			Expect(err).ToNot(HaveOccurred())
			Expect(files()).To(Equal(map[string]string{"Band/Record (1999)/01 A.mp3": "audio"}))
			Expect(entry(res.Entry).CopySum).To(Equal(sumOfBytes([]byte("audio"))))
			Expect(track("mf-1").Path).To(Equal("Band/Record (1999)/01 A.mp3"))

			_, err = editor.Undo(ctx, res.Entry)
			Expect(err).ToNot(HaveOccurred())
			Expect(files()).To(Equal(map[string]string{"iTunes/Band/Record/a.mp3": "audio"}))
		})

		It("reuses a folder whose name differs only in letter case, and journals the path as it is on disk", func() {
			Expect(os.MkdirAll(filepath.Join(libDir, "BAND"), 0o755)).To(Succeed())
			res, err := move("mf-1", "iTunes/Band/Record/a.mp3", "Band/Record (1999)/01 A.mp3")
			Expect(err).ToNot(HaveOccurred())
			Expect(entry(res.Entry).NewPath).To(Equal("BAND/Record (1999)/01 A.mp3"))
			Expect(track("mf-1").Path).To(Equal("BAND/Record (1999)/01 A.mp3"))
		})

		It("moves a File that is no Track's", func() {
			put("iTunes/Band/Record/cover.jpg", "image")
			res, err := move("", "iTunes/Band/Record/cover.jpg", "Band/Record (1999)/cover.jpg")
			Expect(err).ToNot(HaveOccurred())
			Expect(files()).To(HaveKeyWithValue("Band/Record (1999)/cover.jpg", "image"))
			Expect(entry(res.Entry).MediaFileID).To(BeEmpty())
		})

		It("refuses a File changed on disk since the last scan, leaving it as it was", func() {
			put("iTunes/Band/Record/a.mp3", "changed audio")
			_, err := move("mf-1", "iTunes/Band/Record/a.mp3", "Band/Record (1999)/01 A.mp3")
			Expect(err).To(MatchError(ErrChangedOnDisk))
			Expect(files()).To(Equal(map[string]string{"iTunes/Band/Record/a.mp3": "changed audio"}))
		})

		It("never moves a File over another, in any letter case", func() {
			put("Band/Record (1999)/01 a.MP3", "other")
			_, err := move("mf-1", "iTunes/Band/Record/a.mp3", "Band/Record (1999)/01 A.mp3")
			Expect(err).To(MatchError(safefile.ErrDestTaken))
			Expect(Code(err)).To(Equal("pathTaken"))
			Expect(files()).To(Equal(map[string]string{"iTunes/Band/Record/a.mp3": "audio", "Band/Record (1999)/01 a.MP3": "other"}))
			Expect(track("mf-1").Path).To(Equal("iTunes/Band/Record/a.mp3"))
		})

		It("refuses a path through a folder with a twin differing only in letter case", func() {
			Expect(os.MkdirAll(filepath.Join(libDir, "Band"), 0o755)).To(Succeed())
			Expect(os.MkdirAll(filepath.Join(libDir, "BAND"), 0o755)).To(Succeed())
			_, err := move("mf-1", "iTunes/Band/Record/a.mp3", "Band/Record (1999)/01 A.mp3")
			Expect(err).To(MatchError(safefile.ErrCaseTwin))
			Expect(Code(err)).To(Equal("caseTwin"))
		})

		It("is off while the library is read-only", func() {
			core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Writable: false}}})
			_, err := move("mf-1", "iTunes/Band/Record/a.mp3", "Band/Record (1999)/01 A.mp3")
			Expect(err).To(MatchError(ErrEditingOff))
		})

		It("refuses a Track that isn't at the path the move is from", func() {
			_, err := move("mf-1", "iTunes/Band/Record/a.mp3", "Band/Record (1999)/01 A.mp3")
			Expect(err).ToNot(HaveOccurred())
			_, err = move("mf-1", "iTunes/Band/Record/a.mp3", "Elsewhere/a.mp3")
			Expect(err).To(MatchError(ErrChangedOnDisk))
		})
	})

	Describe("Undo", func() {
		It("moves the File back, folders and all, and points its Track back; a redo moves it again", func() {
			res, err := move("mf-1", "iTunes/Band/Record/a.mp3", "Band/Record (1999)/01 A.mp3")
			Expect(err).ToNot(HaveOccurred())
			// Organize removes the folders the move left empty.
			Expect(safefile.RemoveEmptyDirs(safefile.OS{}, libDir, "iTunes/Band/Record")).To(HaveLen(3))

			undo, err := editor.Undo(ctx, res.Entry)
			Expect(err).ToNot(HaveOccurred())
			Expect(files()).To(Equal(map[string]string{"iTunes/Band/Record/a.mp3": "audio"}))
			Expect(track("mf-1").Path).To(Equal("iTunes/Band/Record/a.mp3"))
			u := entry(undo.Entry)
			Expect(u.UndoOf).To(Equal(res.Entry))
			Expect(u.Kind).To(Equal(model.EditMove))
			Expect(u.Path).To(Equal("Band/Record (1999)/01 A.mp3"))
			Expect(u.NewPath).To(Equal("iTunes/Band/Record/a.mp3"))

			_, err = editor.Undo(ctx, res.Entry)
			Expect(err).To(MatchError(ErrAlreadyUndone))

			_, err = editor.Undo(ctx, undo.Entry)
			Expect(err).ToNot(HaveOccurred())
			Expect(track("mf-1").Path).To(Equal("Band/Record (1999)/01 A.mp3"))
		})

		It("refuses when the File at its new path isn't the one moved", func() {
			res, err := move("mf-1", "iTunes/Band/Record/a.mp3", "Band/Record (1999)/01 A.mp3")
			Expect(err).ToNot(HaveOccurred())
			put("Band/Record (1999)/01 A.mp3", "changed")
			_, err = editor.Undo(ctx, res.Entry)
			Expect(err).To(MatchError(ErrChangedOnDisk))
			Expect(files()).To(Equal(map[string]string{"Band/Record (1999)/01 A.mp3": "changed"}))
		})

		It("refuses when its old path is taken", func() {
			res, err := move("mf-1", "iTunes/Band/Record/a.mp3", "Band/Record (1999)/01 A.mp3")
			Expect(err).ToNot(HaveOccurred())
			put("iTunes/Band/Record/a.mp3", "new File")
			_, err = editor.Undo(ctx, res.Entry)
			Expect(err).To(MatchError(safefile.ErrDestTaken))
			Expect(track("mf-1").Path).To(Equal("Band/Record (1999)/01 A.mp3"))
		})

		It("refuses while a later move of the File stands", func() {
			res, err := move("mf-1", "iTunes/Band/Record/a.mp3", "Band/Record (1999)/01 A.mp3")
			Expect(err).ToNot(HaveOccurred())
			// Until its new folder is rescanned, a moved Track's File is guarded as changed.
			_, err = move("mf-1", "Band/Record (1999)/01 A.mp3", "Band/Other/01 A.mp3")
			Expect(err).To(MatchError(ErrChangedOnDisk))
			scanned("mf-1", "Band/Record (1999)/01 A.mp3")
			_, err = move("mf-1", "Band/Record (1999)/01 A.mp3", "Band/Other/01 A.mp3")
			Expect(err).ToNot(HaveOccurred())
			_, err = editor.Undo(ctx, res.Entry)
			Expect(err).To(MatchError(ErrChangedLater))
		})
	})

	Describe("Settle of a rename", func() {
		pending := func() *model.EditEntry {
			GinkgoHelper()
			info, err := os.Stat(filepath.Join(libDir, "iTunes/Band/Record/a.mp3"))
			Expect(err).ToNot(HaveOccurred())
			e := &model.EditEntry{JobID: "job-1", Kind: model.EditMove, State: model.EditPending, LibraryID: 1,
				MediaFileID: "mf-1", Path: "iTunes/Band/Record/a.mp3", NewPath: "Band/Record (1999)/01 A.mp3",
				CopySum: safefile.RenameMark(info)}
			Expect(ds.EditJournal(ctx).Put(e)).To(Succeed())
			return e
		}
		renamed := func() {
			GinkgoHelper()
			Expect(os.MkdirAll(filepath.Join(libDir, "Band/Record (1999)"), 0o755)).To(Succeed())
			Expect(os.Rename(filepath.Join(libDir, "iTunes/Band/Record/a.mp3"), filepath.Join(libDir, "Band/Record (1999)/01 A.mp3"))).To(Succeed())
		}

		It("closes a rename that never happened as not written: the File is at its old path only", func() {
			e := pending()
			editor.Settle(ctx)
			Expect(files()).To(Equal(map[string]string{"iTunes/Band/Record/a.mp3": "audio"}))
			Expect(track("mf-1").Path).To(Equal("iTunes/Band/Record/a.mp3"))
			Expect(entry(e.ID).State).To(Equal(model.EditFailed))
		})

		It("finishes a rename that happened before the Track followed: the File is at its new path only", func() {
			e := pending()
			renamed()
			editor.Settle(ctx)
			Expect(track("mf-1").Path).To(Equal("Band/Record (1999)/01 A.mp3"))
			Expect(entry(e.ID).State).To(Equal(model.EditWritten))
		})

		It("closes a rename whose Track had followed already", func() {
			e := pending()
			renamed()
			Expect(editor.relocate(ctx, "mf-1", &model.Library{ID: 1, Path: libDir}, "Band/Record (1999)/01 A.mp3")).To(Succeed())
			editor.Settle(ctx)
			Expect(track("mf-1").Path).To(Equal("Band/Record (1999)/01 A.mp3"))
			Expect(entry(e.ID).State).To(Equal(model.EditWritten))
		})

		It("leaves pending, and reports, a rename whose File is at neither path", func() {
			e := pending()
			Expect(os.Remove(filepath.Join(libDir, "iTunes/Band/Record/a.mp3"))).To(Succeed())
			editor.Settle(ctx)
			Expect(entry(e.ID).State).To(Equal(model.EditPending))
			Expect(track("mf-1").Path).To(Equal("iTunes/Band/Record/a.mp3"))
		})
	})

	Describe("Settle", func() {
		pending := func(sum string) *model.EditEntry {
			GinkgoHelper()
			e := &model.EditEntry{JobID: "job-1", Kind: model.EditMove, State: model.EditPending, LibraryID: 1,
				MediaFileID: "mf-1", Path: "iTunes/Band/Record/a.mp3", NewPath: "Band/Record (1999)/01 A.mp3", CopySum: sum}
			Expect(ds.EditJournal(ctx).Put(e)).To(Succeed())
			return e
		}

		It("finishes a move stopped once the copy was placed: the old copy goes, the Track follows", func() {
			e := pending(sumOfBytes([]byte("audio")))
			put("Band/Record (1999)/01 A.mp3", "audio")
			editor.Settle(ctx)
			Expect(files()).To(Equal(map[string]string{"Band/Record (1999)/01 A.mp3": "audio"}))
			Expect(track("mf-1").Path).To(Equal("Band/Record (1999)/01 A.mp3"))
			Expect(entry(e.ID).State).To(Equal(model.EditWritten))
		})

		It("finishes a move stopped once the old copy was removed, before the Track followed", func() {
			e := pending(sumOfBytes([]byte("audio")))
			put("Band/Record (1999)/01 A.mp3", "audio")
			Expect(os.Remove(filepath.Join(libDir, "iTunes/Band/Record/a.mp3"))).To(Succeed())
			editor.Settle(ctx)
			Expect(track("mf-1").Path).To(Equal("Band/Record (1999)/01 A.mp3"))
			Expect(entry(e.ID).State).To(Equal(model.EditWritten))
		})

		It("undoes a move stopped while copying", func() {
			e := pending("")
			put("Band/Record (1999)/"+filepath.Base(safefile.MoveTemp("01 A.mp3")), "aud")
			editor.Settle(ctx)
			Expect(files()).To(Equal(map[string]string{"iTunes/Band/Record/a.mp3": "audio"}))
			Expect(track("mf-1").Path).To(Equal("iTunes/Band/Record/a.mp3"))
			Expect(entry(e.ID).State).To(Equal(model.EditFailed))
		})
	})
})
