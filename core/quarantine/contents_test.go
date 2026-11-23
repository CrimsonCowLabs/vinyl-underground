package quarantine

import (
	"context"
	"os"
	"path/filepath"

	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/safefile"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/persistence"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Contents", func() {
	var (
		ctx    context.Context
		ds     model.DataStore
		libDir string
	)
	write := func(rel, content string) {
		GinkgoHelper()
		p := filepath.Join(libDir, filepath.FromSlash(rel))
		Expect(os.MkdirAll(filepath.Dir(p), 0o755)).To(Succeed())
		Expect(os.WriteFile(p, []byte(content), 0o644)).To(Succeed())
	}

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		ctx = request.WithUser(context.Background(), model.User{ID: "admin-1", IsAdmin: true})
		ds = persistence.New(db.Db())
		for _, table := range []string{"media_file", "edit_journal"} {
			_, err := db.Db().Exec("delete from " + table)
			Expect(err).ToNot(HaveOccurred())
		}
		libDir = GinkgoT().TempDir()
		_, err := db.Db().Exec("update library set path = ? where id = 1", libDir)
		Expect(err).ToNot(HaveOccurred())
	})

	It("lists every File in each library's Quarantine, where it came from and its Track, with their count and size", func() {
		write("_Quarantine/.ndignore", "")
		write("_Quarantine/Artist/Album/01 Song.mp3", "song")
		write("_Quarantine/Loose/Dupe 1.mp3", "dupe!")
		write("Artist/Album/02 Kept.mp3", "kept")
		mf := model.MediaFile{ID: "mf-1", LibraryID: 1, Path: "Artist/Album/01 Song.mp3", Title: "Song", Artist: "Artist",
			Album: "Album", Missing: true, Participants: model.Participants{}}
		Expect(ds.MediaFile(ctx).Put(&mf)).To(Succeed())
		e := &model.EditEntry{Kind: model.EditQuarantine, State: model.EditWritten, LibraryID: 1, MediaFileID: "mf-1",
			Path: "Artist/Album/01 Song.mp3", NewPath: "_Quarantine/Artist/Album/01 Song.mp3"}
		Expect(ds.EditJournal(ctx).Put(e)).To(Succeed())

		c, err := Contents(ctx, ds, safefile.OS{})
		Expect(err).ToNot(HaveOccurred())
		Expect(c.Count).To(Equal(2))
		Expect(c.Size).To(Equal(int64(9)))
		Expect(c.Files).To(HaveLen(2))

		song := c.Files[0]
		Expect(song.LibraryID).To(Equal(1))
		Expect(song.Path).To(Equal("_Quarantine/Artist/Album/01 Song.mp3"))
		Expect(song.Size).To(Equal(int64(4)))
		Expect(song.Original).To(Equal("Artist/Album/01 Song.mp3"))
		Expect(song.EntryID).To(Equal(e.ID))
		Expect(song.TrackID).To(Equal("mf-1"))
		Expect(song.Title).To(Equal("Song"))
		Expect(song.Artist).To(Equal("Artist"))

		dupe := c.Files[1]
		Expect(dupe.Original).To(Equal("Loose/Dupe 1.mp3"), "moved by the cleanup recipe: its path beneath Quarantine")
		Expect(dupe.EntryID).To(BeEmpty())
		Expect(dupe.TrackID).To(BeEmpty())
	})

	It("names no move that no longer holds, and counts only what Empty Quarantine can delete", func() {
		write("_Quarantine/Artist/Song.mp3", "song")
		e := &model.EditEntry{Kind: model.EditQuarantine, State: model.EditWritten, LibraryID: 1, MediaFileID: "mf-1",
			Path: "Artist/Song.mp3", NewPath: "_Quarantine/Artist/Song.mp3"}
		Expect(ds.EditJournal(ctx).Put(e)).To(Succeed())
		undo := &model.EditEntry{Kind: model.EditQuarantine, State: model.EditWritten, LibraryID: 1, MediaFileID: "mf-1",
			Path: "_Quarantine/Artist/Song.mp3", NewPath: "Artist/Song.mp3", UndoOf: e.ID}
		Expect(ds.EditJournal(ctx).Put(undo)).To(Succeed())

		c, err := Contents(ctx, ds, safefile.OS{})
		Expect(err).ToNot(HaveOccurred())
		Expect(c.Files).To(HaveLen(1))
		Expect(c.Files[0].EntryID).To(BeEmpty(), "that move was undone; this File came another way")
		Expect(c.Files[0].Writable).To(BeFalse())
		Expect(c.Count).To(Equal(1))
		Expect(c.EmptyCount).To(Equal(0), "the library is read-only")

		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Writable: true}}})
		DeferCleanup(core.SetLibraryEditing, core.LibraryEditing{})
		c, err = Contents(ctx, ds, safefile.OS{})
		Expect(err).ToNot(HaveOccurred())
		Expect(c.EmptyCount).To(Equal(1))
		Expect(c.EmptySize).To(Equal(int64(4)))
	})

	It("is empty with no Quarantine", func() {
		c, err := Contents(ctx, ds, safefile.OS{})
		Expect(err).ToNot(HaveOccurred())
		Expect(c.Count).To(Equal(0))
		Expect(c.Files).To(BeEmpty())
	})
})
