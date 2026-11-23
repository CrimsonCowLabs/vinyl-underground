package tagedit

import (
	"context"
	"os"
	"path/filepath"

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

// twinFS is the NAS over SMB1, where a path into one of two names differing only in letter case
// may reach the other: a rename of from renames to instead.
type twinFS struct {
	safefile.OS
	from, to string
}

func (f twinFS) RenameNoReplace(oldname, newname string) error {
	if oldname == f.from {
		oldname = f.to
	}
	return f.OS.RenameNoReplace(oldname, newname)
}

// The case merge's writes (#169): renames of Files and folders with all they hold, and Duplicates
// sent to Quarantine once confirmed byte-identical.
var _ = Describe("Case merge writes", func() {
	var (
		ctx    context.Context
		ds     model.DataStore
		editor *Editor
		libDir string
		lib    model.Library
	)

	put := func(rel, content string) {
		GinkgoHelper()
		abs := filepath.Join(libDir, filepath.FromSlash(rel))
		Expect(os.MkdirAll(filepath.Dir(abs), 0o755)).To(Succeed())
		Expect(os.WriteFile(abs, []byte(content), 0o644)).To(Succeed())
	}
	scanned := func(id, rel string) {
		GinkgoHelper()
		info, err := os.Stat(filepath.Join(libDir, filepath.FromSlash(rel)))
		Expect(err).ToNot(HaveOccurred())
		mf := model.MediaFile{ID: id, LibraryID: 1, Path: rel, FolderID: model.FolderID(lib, filepath.Dir(rel)),
			Size: info.Size(), UpdatedAt: info.ModTime(), Participants: model.Participants{}}
		Expect(ds.MediaFile(ctx).Put(&mf)).To(Succeed())
	}
	trackPath := func(id string) string {
		GinkgoHelper()
		mf, err := ds.MediaFile(ctx).Get(id)
		Expect(err).ToNot(HaveOccurred())
		return mf.Path
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

		put("Music/ATB/a.mp3", "a")
		put("Music/Atb/b.mp3", "b")
		put("Music/Atb/cover.jpg", "image, no Track's")
		scanned("mf-a", "Music/ATB/a.mp3")
		scanned("mf-b", "Music/Atb/b.mp3")
	})

	rename := func(from, to string) (*Result, error) {
		return editor.RenameInJob(ctx, "job-1", Rename{LibraryID: 1, Path: from, NewPath: to})
	}

	Describe("RenameInJob", func() {
		It("renames a folder with all it holds, journaled, and points its Tracks at their new place, keeping their IDs", func() {
			res, err := rename("Music/Atb", "Music/Atb [case-merge]")
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Changed).To(BeTrue())
			Expect(files()).To(Equal(map[string]string{
				"Music/ATB/a.mp3":                  "a",
				"Music/Atb [case-merge]/b.mp3":     "b",
				"Music/Atb [case-merge]/cover.jpg": "image, no Track's",
			}))
			Expect(trackPath("mf-b")).To(Equal("Music/Atb [case-merge]/b.mp3"))
			Expect(trackPath("mf-a")).To(Equal("Music/ATB/a.mp3"), "the twin's Tracks stay")
			e := entry(res.Entry)
			Expect(e.Kind).To(Equal(model.EditRename))
			Expect(e.State).To(Equal(model.EditWritten))
			Expect(e.JobID).To(Equal("job-1"))
			Expect(e.Path).To(Equal("Music/Atb"))
			Expect(e.NewPath).To(Equal("Music/Atb [case-merge]"))
		})

		It("records the twin the server renamed instead (SMB1), and moves that one's Tracks", func() {
			editor.fsys = twinFS{from: filepath.Join(libDir, "Music", "Atb"), to: filepath.Join(libDir, "Music", "ATB")}
			res, err := rename("Music/Atb", "Music/Atb [case-merge]")
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Warning).ToNot(BeEmpty())
			Expect(files()).To(HaveKey("Music/Atb [case-merge]/a.mp3"))
			Expect(entry(res.Entry).Path).To(Equal("Music/ATB"))
			Expect(trackPath("mf-a")).To(Equal("Music/Atb [case-merge]/a.mp3"))
			Expect(trackPath("mf-b")).To(Equal("Music/Atb/b.mp3"))
		})

		It("renames a File, and its Track with it", func() {
			put("Music/Queen/c.mp3", "c")
			scanned("mf-c", "Music/Queen/c.mp3")
			res, err := rename("Music/Queen/c.mp3", "Music/Queen/c [case-merge].mp3")
			Expect(err).ToNot(HaveOccurred())
			Expect(trackPath("mf-c")).To(Equal("Music/Queen/c [case-merge].mp3"))
			Expect(entry(res.Entry).Path).To(Equal("Music/Queen/c.mp3"))
		})

		It("moves a folder into another one, made if it is gone", func() {
			_, err := rename("Music/Atb", "Bands/Atb")
			Expect(err).ToNot(HaveOccurred())
			Expect(trackPath("mf-b")).To(Equal("Bands/Atb/b.mp3"))
			Expect(files()).To(HaveKey("Bands/Atb/b.mp3"))
		})

		It("refuses a new path through a folder with a case twin", func() {
			_, err := rename("Music/Atb/b.mp3", "Music/ATB/b.mp3")
			Expect(err).To(MatchError(safefile.ErrCaseTwin))
			Expect(files()).To(HaveKey("Music/Atb/b.mp3"))
		})

		It("never renames onto a name taken, in any letter case, and journals the refusal", func() {
			_, err := rename("Music/Atb", "Music/atb")
			Expect(err).To(MatchError(safefile.ErrDestTaken))
			Expect(files()).To(HaveKey("Music/Atb/b.mp3"))
			Expect(trackPath("mf-b")).To(Equal("Music/Atb/b.mp3"))
		})

		It("refuses what isn't there", func() {
			_, err := rename("Music/Nope", "Music/Nope [case-merge]")
			Expect(err).To(MatchError(ErrChangedOnDisk))
		})

		It("refuses a read-only library", func() {
			core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Name: "NAS"}}})
			_, err := rename("Music/Atb", "Music/Atb [case-merge]")
			Expect(err).To(MatchError(ErrEditingOff))
		})
	})

	Describe("undo", func() {
		It("puts the name back, beside its twin where the filesystem holds both, and the Tracks with it", func() {
			res, err := rename("Music/Atb", "Music/Atb [case-merge]")
			Expect(err).ToNot(HaveOccurred())
			undo, err := editor.UndoInJob(ctx, "job-2", res.Entry)
			Expect(err).ToNot(HaveOccurred())
			Expect(undo.Changed).To(BeTrue())
			Expect(files()).To(HaveKey("Music/Atb/b.mp3"))
			Expect(trackPath("mf-b")).To(Equal("Music/Atb/b.mp3"))
			Expect(entry(undo.Entry).UndoOf).To(Equal(res.Entry))

			_, err = editor.UndoInJob(ctx, "job-3", res.Entry)
			Expect(err).To(MatchError(ErrAlreadyUndone))
		})

		It("is refused while a later write moved what the rename left", func() {
			res, err := rename("Music/Atb", "Music/Atb [case-merge]")
			Expect(err).ToNot(HaveOccurred())
			_, err = rename("Music/Atb [case-merge]", "Music/Atb 2")
			Expect(err).ToNot(HaveOccurred())
			_, err = editor.UndoInJob(ctx, "job-2", res.Entry)
			Expect(err).To(MatchError(ErrChangedLater))
		})

		It("is refused while a later write to something inside what it renamed stands, and allowed once that is undone", func() {
			res, err := rename("Music/Atb", "Music/Atb [case-merge]")
			Expect(err).ToNot(HaveOccurred())
			put("Music/Queen/c.mp3", "c")
			moved, err := editor.MoveInJob(ctx, "job-2", Move{LibraryID: 1, Path: "Music/Queen/c.mp3", NewPath: "Music/Atb [case-merge]/c.mp3"})
			Expect(err).ToNot(HaveOccurred())
			_, err = editor.UndoInJob(ctx, "job-3", res.Entry)
			Expect(err).To(MatchError(ErrChangedLater))
			Expect(files()).To(HaveKey("Music/Atb [case-merge]/c.mp3"))

			_, err = editor.UndoInJob(ctx, "job-4", moved.Entry)
			Expect(err).ToNot(HaveOccurred())
			_, err = editor.UndoInJob(ctx, "job-5", res.Entry)
			Expect(err).ToNot(HaveOccurred())
			Expect(files()).To(HaveKey("Music/Atb/b.mp3"))
		})

		It("is refused when what the rename left is gone", func() {
			res, err := rename("Music/Atb", "Music/Atb [case-merge]")
			Expect(err).ToNot(HaveOccurred())
			Expect(os.RemoveAll(filepath.Join(libDir, "Music", "Atb [case-merge]"))).To(Succeed())
			_, err = editor.UndoInJob(ctx, "job-2", res.Entry)
			Expect(err).To(MatchError(ErrChangedOnDisk))
		})
	})

	Describe("Settle", func() {
		It("finishes a rename a stop interrupted once the new name is there, pointing the Tracks at it", func() {
			Expect(os.Rename(filepath.Join(libDir, "Music", "Atb"), filepath.Join(libDir, "Music", "Atb [case-merge]"))).To(Succeed())
			e := &model.EditEntry{JobID: "job-1", Kind: model.EditRename, State: model.EditPending, LibraryID: 1,
				Path: "Music/Atb", NewPath: "Music/Atb [case-merge]"}
			Expect(ds.EditJournal(ctx).Put(e)).To(Succeed())
			editor.Settle(ctx)
			Expect(entry(e.ID).State).To(Equal(model.EditWritten))
			Expect(trackPath("mf-b")).To(Equal("Music/Atb [case-merge]/b.mp3"))
		})

		It("closes one that never renamed", func() {
			e := &model.EditEntry{JobID: "job-1", Kind: model.EditRename, State: model.EditPending, LibraryID: 1,
				Path: "Music/Atb", NewPath: "Music/Atb [case-merge]"}
			Expect(ds.EditJournal(ctx).Put(e)).To(Succeed())
			editor.Settle(ctx)
			Expect(entry(e.ID).State).To(Equal(model.EditFailed))
			Expect(trackPath("mf-b")).To(Equal("Music/Atb/b.mp3"))
		})
	})

	Describe("QuarantineDuplicateInJob", func() {
		BeforeEach(func() {
			put("Music/Atb [case-merge]/Song.mp3", "same bytes")
			put("Music/ATB/Song.mp3", "same bytes")
			scanned("mf-dup", "Music/Atb [case-merge]/Song.mp3")
		})
		dup := func(rel string, trackID string) (*Result, error) {
			return editor.QuarantineDuplicateInJob(ctx, "job-1", Duplicate{LibraryID: 1, TrackID: trackID, Path: rel,
				NewPath: "_Quarantine/Music/ATB/Song.mp3", Keep: "Music/ATB/Song.mp3"})
		}

		It("sends a byte-identical File to Quarantine, its Track with it, and its undo brings both back", func() {
			res, err := dup("Music/Atb [case-merge]/Song.mp3", "mf-dup")
			Expect(err).ToNot(HaveOccurred())
			Expect(files()).To(HaveKeyWithValue("_Quarantine/Music/ATB/Song.mp3", "same bytes"))
			Expect(files()).ToNot(HaveKey("Music/Atb [case-merge]/Song.mp3"))
			Expect(entry(res.Entry).Kind).To(Equal(model.EditQuarantine))
			mf, err := ds.MediaFile(ctx).Get("mf-dup")
			Expect(err).ToNot(HaveOccurred())
			Expect(mf.Quarantined).To(BeTrue())

			_, err = editor.UndoInJob(ctx, "job-2", res.Entry)
			Expect(err).ToNot(HaveOccurred())
			Expect(files()).To(HaveKeyWithValue("Music/Atb [case-merge]/Song.mp3", "same bytes"))
			mf, err = ds.MediaFile(ctx).Get("mf-dup")
			Expect(err).ToNot(HaveOccurred())
			Expect(mf.Quarantined).To(BeFalse())
		})

		It("sends a File no scan found (no Track)", func() {
			put("Music/Atb [case-merge]/Other.mp3", "same bytes")
			_, err := dup("Music/Atb [case-merge]/Other.mp3", "")
			Expect(err).ToNot(HaveOccurred())
			Expect(files()).To(HaveKey("_Quarantine/Music/ATB/Song.mp3"))
		})

		It("leaves a File whose bytes differ from the one it would duplicate", func() {
			put("Music/ATB/Song.mp3", "other bytes")
			_, err := dup("Music/Atb [case-merge]/Song.mp3", "mf-dup")
			Expect(err).To(MatchError(ErrNotDuplicate))
			Expect(files()).To(HaveKeyWithValue("Music/Atb [case-merge]/Song.mp3", "same bytes"))
			Expect(files()).ToNot(HaveKey("_Quarantine/Music/ATB/Song.mp3"))
		})
	})
})
