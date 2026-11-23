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

var _ = Describe("Quarantine moves", func() {
	const songPath = "Artist/Album/01 Song.mp3"
	var (
		ctx     context.Context
		ds      model.DataStore
		scanner *tests.MockScanner
		editor  *Editor
		libDir  string
	)

	abs := func(rel string) string { return filepath.Join(libDir, filepath.FromSlash(rel)) }
	write := func(rel, content string) {
		GinkgoHelper()
		Expect(os.MkdirAll(filepath.Dir(abs(rel)), 0o755)).To(Succeed())
		Expect(os.WriteFile(abs(rel), []byte(content), 0o644)).To(Succeed())
	}
	content := func(rel string) string {
		GinkgoHelper()
		b, err := os.ReadFile(abs(rel))
		Expect(err).ToNot(HaveOccurred())
		return string(b)
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
	scannedFolders := func() []string {
		var res []string
		for _, c := range scanner.GetScanFoldersCalls() {
			for _, t := range c.Targets {
				res = append(res, t.FolderPath)
			}
		}
		return res
	}

	BeforeEach(func() {
		ctx = request.WithUser(context.Background(), model.User{ID: "admin-1", UserName: "admin", IsAdmin: true})
		ds = persistence.New(db.Db())
		for _, table := range []string{"media_file", "edit_journal"} {
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

		write(songPath, "song")
		// As the scanner records it: the move's guard checks the File's size and time.
		info, err := os.Stat(abs(songPath))
		Expect(err).ToNot(HaveOccurred())
		mf := model.MediaFile{ID: "mf-1", LibraryID: 1, Path: songPath, Title: "Song", Participants: model.Participants{},
			Size: info.Size(), UpdatedAt: info.ModTime()}
		Expect(ds.MediaFile(ctx).Put(&mf)).To(Succeed())

		scanner = tests.NewMockScanner()
		editor = New(ds, scanner)
	})

	Describe("QuarantineInJob", func() {
		It("moves the File into Quarantine at its relative path, journaled, and the Track leaves the Library", func() {
			res, err := editor.QuarantineInJob(ctx, "job-1", "mf-1")
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Changed).To(BeTrue())

			Expect(content("_Quarantine/Artist/Album/01 Song.mp3")).To(Equal("song"))
			Expect(abs(songPath)).ToNot(BeAnExistingFile())
			Expect(abs("_Quarantine/.ndignore")).To(BeARegularFile(), "the scanner skips Quarantine")

			e := entry(res.Entry)
			Expect(e.Kind).To(Equal(model.EditQuarantine))
			Expect(e.State).To(Equal(model.EditWritten))
			Expect(e.JobID).To(Equal("job-1"))
			Expect(e.UserID).To(Equal("admin-1"))
			Expect(e.MediaFileID).To(Equal("mf-1"))
			Expect(e.Path).To(Equal(songPath))
			Expect(e.NewPath).To(Equal("_Quarantine/" + songPath))

			mf := track("mf-1")
			Expect(mf.Missing).To(BeTrue())
			Expect(mf.Quarantined).To(BeTrue())
			Expect(mf.Path).To(Equal(songPath), "the row keeps its path, so the File's return finds it")
			Expect(scannedFolders()).To(ConsistOf("Artist/Album"))
		})

		It("goes into the folders already in Quarantine whose names differ only in letter case", func() {
			write("_quarantine/ARTIST/other.mp3", "other")
			res, err := editor.QuarantineInJob(ctx, "job-1", "mf-1")
			Expect(err).ToNot(HaveOccurred())
			Expect(content("_quarantine/ARTIST/Album/01 Song.mp3")).To(Equal("song"))
			Expect(entry(res.Entry).NewPath).To(Equal("_quarantine/ARTIST/Album/01 Song.mp3"))
			entries, _ := os.ReadDir(libDir)
			Expect(entries).To(HaveLen(2), "no second Quarantine folder")
		})

		It("goes to the configured Quarantine folder", func() {
			conf.Server.QuarantineFolder = "Removed"
			_, err := editor.QuarantineInJob(ctx, "job-1", "mf-1")
			Expect(err).ToNot(HaveOccurred())
			Expect(content("Removed/" + songPath)).To(Equal("song"))
			Expect(abs("Removed/.ndignore")).To(BeARegularFile())
		})

		It("flags a File already at the target and never overwrites it", func() {
			write("_Quarantine/Artist/Album/01 song.mp3", "earlier")
			_, err := editor.QuarantineInJob(ctx, "job-1", "mf-1")
			Expect(err).To(MatchError(safefile.ErrDestTaken))
			Expect(Code(err)).To(Equal("pathTaken"))
			Expect(content(songPath)).To(Equal("song"))
			Expect(content("_Quarantine/Artist/Album/01 song.mp3")).To(Equal("earlier"))
			Expect(track("mf-1").Missing).To(BeFalse())
		})

		It("refuses a read-only library, a missing File and a File in Quarantine already", func() {
			core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Writable: false}}})
			_, err := editor.QuarantineInJob(ctx, "job-1", "mf-1")
			Expect(err).To(MatchError(ErrEditingOff))
			core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Writable: true}}})

			Expect(os.Remove(abs(songPath))).To(Succeed())
			_, err = editor.QuarantineInJob(ctx, "job-1", "mf-1")
			Expect(err).To(MatchError(ErrChangedOnDisk))

			inQ := model.MediaFile{ID: "mf-q", LibraryID: 1, Path: "_Quarantine/x.mp3", Participants: model.Participants{}}
			Expect(ds.MediaFile(ctx).Put(&inQ)).To(Succeed())
			write("_Quarantine/x.mp3", "x")
			_, err = editor.QuarantineInJob(ctx, "job-1", "mf-q")
			Expect(err).To(MatchError(ErrInQuarantine))
		})

		It("marks the Track as in Quarantine before its File goes, so no scan in between purges it", func() {
			var atRename *model.MediaFile
			editor.fsys = renameSpyFS{FS: safefile.OS{}, onRename: func() { atRename = track("mf-1") }}
			_, err := editor.QuarantineInJob(ctx, "job-1", "mf-1")
			Expect(err).ToNot(HaveOccurred())
			Expect(atRename).ToNot(BeNil())
			Expect(atRename.Quarantined).To(BeTrue())
		})

		It("leaves the File where it was when the move fails, and closes the entry as failed", func() {
			editor.fsys = faultyFS{FS: safefile.OS{}, match: "01 Song.mp3"}
			_, err := editor.QuarantineInJob(ctx, "job-1", "mf-1")
			Expect(err).To(HaveOccurred())
			Expect(content(songPath)).To(Equal("song"))
			entries, err := ds.EditJournal(ctx).ForJob("job-1")
			Expect(err).ToNot(HaveOccurred())
			Expect(entries).To(HaveLen(1))
			Expect(entries[0].State).To(Equal(model.EditFailed))
			Expect(track("mf-1").Missing).To(BeFalse())
			Expect(track("mf-1").Quarantined).To(BeFalse())
		})
	})

	Describe("Settle", func() {
		// pending is a move into Quarantine a stop cut off after its rename was recorded (the
		// File's RenameMark) and its Track marked.
		pending := func() *model.EditEntry {
			GinkgoHelper()
			info, err := os.Stat(abs(songPath))
			Expect(err).ToNot(HaveOccurred())
			e := &model.EditEntry{JobID: "job-1", Kind: model.EditQuarantine, State: model.EditPending, LibraryID: 1,
				MediaFileID: "mf-1", Path: songPath, NewPath: "_Quarantine/" + songPath, CopySum: safefile.RenameMark(info)}
			Expect(ds.EditJournal(ctx).Put(e)).To(Succeed())
			Expect(ds.MediaFile(ctx).MarkQuarantined(true, "mf-1")).To(Succeed())
			return e
		}

		It("finishes a move the stop cut off after the File was moved", func() {
			e := pending()
			Expect(os.MkdirAll(filepath.Dir(abs("_Quarantine/"+songPath)), 0o755)).To(Succeed())
			Expect(os.Rename(abs(songPath), abs("_Quarantine/"+songPath))).To(Succeed())
			editor.Settle(ctx)
			Expect(entry(e.ID).State).To(Equal(model.EditWritten))
			Expect(track("mf-1").Quarantined).To(BeTrue())
			Expect(track("mf-1").Missing).To(BeTrue())
		})

		It("closes as failed a move the stop cut off before the File was moved", func() {
			e := pending()
			editor.Settle(ctx)
			Expect(entry(e.ID).State).To(Equal(model.EditFailed))
			Expect(track("mf-1").Missing).To(BeFalse())
			Expect(track("mf-1").Quarantined).To(BeFalse())
			Expect(content(songPath)).To(Equal("song"))
		})

		It("leaves pending a move whose File is in neither place", func() {
			e := pending()
			Expect(os.Remove(abs(songPath))).To(Succeed())
			editor.Settle(ctx)
			Expect(entry(e.ID).State).To(Equal(model.EditPending))
		})
	})

	Describe("Undo", func() {
		var moved string
		BeforeEach(func() {
			res, err := editor.QuarantineInJob(ctx, "job-1", "mf-1")
			Expect(err).ToNot(HaveOccurred())
			moved = res.Entry
			scanner.Reset()
		})

		It("puts the File back at its original path, journaled, and the Track out of Quarantine; undoing that quarantines it again", func() {
			res, err := editor.Undo(ctx, moved)
			Expect(err).ToNot(HaveOccurred())
			Expect(content(songPath)).To(Equal("song"))
			Expect(abs("_Quarantine/" + songPath)).ToNot(BeAnExistingFile())
			u := entry(res.Entry)
			Expect(u.Kind).To(Equal(model.EditQuarantine))
			Expect(u.UndoOf).To(Equal(moved))
			Expect(u.Path).To(Equal("_Quarantine/" + songPath))
			Expect(u.NewPath).To(Equal(songPath))
			Expect(track("mf-1").Quarantined).To(BeFalse())
			Expect(scannedFolders()).To(ConsistOf("Artist/Album"), "the scan finds the File, on the same row")

			_, err = editor.Undo(ctx, moved)
			Expect(err).To(MatchError(ErrAlreadyUndone))

			_, err = editor.Undo(ctx, res.Entry)
			Expect(err).ToNot(HaveOccurred())
			Expect(content("_Quarantine/" + songPath)).To(Equal("song"))
			Expect(track("mf-1").Quarantined).To(BeTrue())
		})

		It("recreates the original folders if they are gone", func() {
			Expect(os.RemoveAll(abs("Artist"))).To(Succeed())
			_, err := editor.Undo(ctx, moved)
			Expect(err).ToNot(HaveOccurred())
			Expect(content(songPath)).To(Equal("song"))
		})

		It("never overwrites a File that took the original path since", func() {
			write("Artist/Album/01 SONG.mp3", "new")
			_, err := editor.Undo(ctx, moved)
			Expect(err).To(MatchError(safefile.ErrDestTaken))
			Expect(content("_Quarantine/" + songPath)).To(Equal("song"))
			Expect(content("Artist/Album/01 SONG.mp3")).To(Equal("new"))
		})

		It("refuses when the File isn't in Quarantine any more", func() {
			Expect(os.Remove(abs("_Quarantine/" + songPath))).To(Succeed())
			_, err := editor.Undo(ctx, moved)
			Expect(err).To(MatchError(ErrChangedOnDisk))
		})
	})

	Describe("RestoreInJob", func() {
		It("moves a File the Edit journal has no move of to the path beneath Quarantine it has, and its Track out of Quarantine", func() {
			write("_Quarantine/"+songPath, "song")
			Expect(os.Remove(abs(songPath))).To(Succeed())
			Expect(ds.MediaFile(ctx).MarkQuarantined(true, "mf-1")).To(Succeed())
			Expect(ds.MediaFile(ctx).MarkMissing(true, &model.MediaFile{ID: "mf-1"})).To(Succeed())

			res, err := editor.RestoreInJob(ctx, "job-2", 1, "_Quarantine/"+songPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(content(songPath)).To(Equal("song"))
			e := entry(res.Entry)
			Expect(e.Kind).To(Equal(model.EditQuarantine))
			Expect(e.MediaFileID).To(BeEmpty(), "the Track's row stays at the File's old path, for it to come back to")
			Expect(e.Path).To(Equal("_Quarantine/" + songPath))
			Expect(e.NewPath).To(Equal(songPath))
			Expect(track("mf-1").Quarantined).To(BeFalse())
		})

		It("refuses a File outside Quarantine, and a .ndignore", func() {
			_, err := editor.RestoreInJob(ctx, "job-2", 1, songPath)
			Expect(err).To(MatchError(ErrNotInQuarantine))
			write("_Quarantine/Artist/.ndignore", "")
			_, err = editor.RestoreInJob(ctx, "job-2", 1, "_Quarantine/Artist/.ndignore")
			Expect(err).To(MatchError(ErrNotInQuarantine))
			_, err = editor.RestoreInJob(ctx, "job-2", 1, "_Quarantine/.ndignore")
			Expect(err).To(MatchError(ErrNotInQuarantine))
		})
	})

	Describe("EmptyInJob", func() {
		var files []QuarantinedFile
		BeforeEach(func() {
			_, err := editor.QuarantineInJob(ctx, "job-1", "mf-1")
			Expect(err).ToNot(HaveOccurred())
			write("_Quarantine/Other/Dupe.mp3", "dupe!")
			files = []QuarantinedFile{
				{LibraryID: 1, Path: "_Quarantine/" + songPath, Size: 4},
				{LibraryID: 1, Path: "_Quarantine/Other/Dupe.mp3", Size: 5},
			}
		})

		It("deletes the confirmed Files for good, journaled, and their Tracks with them", func() {
			res, err := editor.EmptyInJob(ctx, "job-3", files)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Changed).To(BeTrue())
			Expect(abs("_Quarantine/" + songPath)).ToNot(BeAnExistingFile())
			Expect(abs("_Quarantine/Other/Dupe.mp3")).ToNot(BeAnExistingFile())
			Expect(abs("_Quarantine/.ndignore")).To(BeARegularFile(), "the scanner still skips Quarantine")
			Expect(abs("_Quarantine/Other")).ToNot(BeADirectory(), "emptied folders go")

			entries, err := ds.EditJournal(ctx).ForJob("job-3")
			Expect(err).ToNot(HaveOccurred())
			Expect(entries).To(HaveLen(2))
			for _, e := range entries {
				Expect(e.Kind).To(Equal(model.EditDelete))
				Expect(e.State).To(Equal(model.EditWritten))
				Expect(e.NewPath).To(BeEmpty())
			}
			Expect(entries[0].MediaFileID).To(Equal("mf-1"))

			_, err = ds.MediaFile(ctx).Get("mf-1")
			Expect(err).To(MatchError(model.ErrNotFound), "a Track whose File is gone for good goes")

			_, err = editor.Undo(ctx, entries[0].ID)
			Expect(err).To(MatchError(ErrNotUndoable))
		})

		It("leaves a File that changed since the confirmation, and one outside Quarantine", func() {
			write("_Quarantine/Other/Dupe.mp3", "changed!")
			files = append(files, QuarantinedFile{LibraryID: 1, Path: "Artist/Keep.mp3", Size: 4})
			write("Artist/Keep.mp3", "keep")
			res, err := editor.EmptyInJob(ctx, "job-3", files)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Warning).ToNot(BeEmpty())
			Expect(content("_Quarantine/Other/Dupe.mp3")).To(Equal("changed!"))
			Expect(content("Artist/Keep.mp3")).To(Equal("keep"))
			Expect(abs("_Quarantine/" + songPath)).ToNot(BeAnExistingFile())
		})

		It("can run again after a stop: the Files gone already are passed over, and their Tracks still go", func() {
			Expect(os.Remove(abs("_Quarantine/" + songPath))).To(Succeed())
			res, err := editor.EmptyInJob(ctx, "job-3", files)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Warning).To(BeEmpty())
			Expect(abs("_Quarantine/Other/Dupe.mp3")).ToNot(BeAnExistingFile())
			_, err = ds.MediaFile(ctx).Get("mf-1")
			Expect(err).To(MatchError(model.ErrNotFound))
		})

		It("never deletes a .ndignore, which keeps the scanner out", func() {
			write("_Quarantine/Other/.ndignore", "")
			files = append(files, QuarantinedFile{LibraryID: 1, Path: "_Quarantine/Other/.ndignore", Size: 0})
			res, err := editor.EmptyInJob(ctx, "job-3", files)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Warning).ToNot(BeEmpty())
			Expect(abs("_Quarantine/Other/.ndignore")).To(BeAnExistingFile())
		})

		It("refuses the undo of the move of a File it deleted", func() {
			moved, err := ds.EditJournal(ctx).ForJob("job-1")
			Expect(err).ToNot(HaveOccurred())
			_, err = editor.EmptyInJob(ctx, "job-3", files)
			Expect(err).ToNot(HaveOccurred())
			_, err = editor.Undo(ctx, moved[0].ID)
			Expect(err).To(MatchError(ErrDeletedForGood))
		})

		It("refuses a read-only library", func() {
			core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Writable: false}}})
			_, err := editor.EmptyInJob(ctx, "job-3", files)
			Expect(err).To(MatchError(ErrEditingOff))
			Expect(content("_Quarantine/Other/Dupe.mp3")).To(Equal("dupe!"))
		})
	})
})

// renameSpyFS calls onRename just before each rename.
type renameSpyFS struct {
	safefile.FS
	onRename func()
}

func (f renameSpyFS) Rename(oldname, newname string) error {
	f.onRename()
	return f.FS.Rename(oldname, newname)
}
