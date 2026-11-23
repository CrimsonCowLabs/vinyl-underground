package tagedit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/persistence"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func sumOfBytes(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// fixtureImage reads an image from the test fixtures.
func fixtureImage(name string) []byte {
	GinkgoHelper()
	b, err := os.ReadFile(filepath.Join("tests", "fixtures", "artist", "an-album", name))
	Expect(err).ToNot(HaveOccurred())
	return b
}

var _ = Describe("Cover writes", func() {
	var (
		ctx     context.Context
		ds      model.DataStore
		scanner *tests.MockScanner
		editor  *Editor
		libDir  string
		dir     string // the album's folder, absolute
		folder  *model.Folder
		al      model.Album
		cover   []byte // a JPEG
	)

	BeforeEach(func() {
		ctx = request.WithUser(context.Background(), model.User{ID: "admin-1", UserName: "admin", IsAdmin: true})
		ds = persistence.New(db.Db())
		for _, table := range []string{"correction", "media_file", "folder", "album", "edit_journal", "artwork_queue"} {
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

		dir = filepath.Join(libDir, "Band", "Record")
		Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
		folder = addFolder(ctx, ds, "Band/Record")
		al = model.Album{ID: "al-1", Name: "Record", AlbumArtist: "Band", LibraryID: 1, FolderIDs: []string{folder.ID}}
		Expect(ds.Album(ctx).Put(&al)).To(Succeed())
		addAlbumTrack(ctx, ds, "mf-1", al.ID, folder)
		addAlbumTrack(ctx, ds, "mf-2", al.ID, folder)

		scanner = tests.NewMockScanner()
		editor = New(ds, scanner)
		cover = fixtureImage("cover.jpg")
	})

	coverFile := func(name string) []byte {
		GinkgoHelper()
		b, err := os.ReadFile(filepath.Join(dir, name))
		Expect(err).ToNot(HaveOccurred())
		return b
	}
	names := func() []string {
		GinkgoHelper()
		entries, err := os.ReadDir(dir)
		Expect(err).ToNot(HaveOccurred())
		var res []string
		for _, e := range entries {
			res = append(res, e.Name())
		}
		return res
	}
	entry := func(id string) *model.EditEntry {
		GinkgoHelper()
		e, err := ds.EditJournal(ctx).Get(id)
		Expect(err).ToNot(HaveOccurred())
		return e
	}

	Describe("WriteCover", func() {
		It("writes a new cover.jpg into the album's folder, journals it, and refreshes the album's cover", func() {
			res, err := editor.WriteCover(ctx, al.ID, cover)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Changed).To(BeTrue())

			Expect(coverFile("cover.jpg")).To(Equal(cover))
			Expect(names()).To(ConsistOf("cover.jpg"), "no temp left behind")

			e := entry(res.Entry)
			Expect(e.Kind).To(Equal(model.EditCover))
			Expect(e.State).To(Equal(model.EditWritten))
			Expect(e.UserID).To(Equal("admin-1"))
			Expect(e.Path).To(Equal("Band/Record/cover.jpg"))
			Expect(e.NewPath).To(Equal("Band/Record/cover.jpg"))
			Expect(e.OriginalImage.Sum).To(BeEmpty(), "there was no cover")
			Expect(e.Image.Sum).To(Equal(sumOfBytes(cover)))

			// The folder is rescanned, so the app sees the new image, and the album's cover is
			// resolved again.
			Expect(scanner.GetScanFoldersCalls()).To(HaveLen(1))
			Expect(scanner.GetScanFoldersCalls()[0].Targets).To(Equal([]model.ScanTarget{{LibraryID: 1, FolderPath: "Band/Record"}}))
			_, err = ds.ArtworkQueue(ctx).Get(model.KindAlbumArtwork, al.ID, model.ImageTypePrimary)
			Expect(err).ToNot(HaveOccurred())
		})

		It("replaces an existing cover.jpg, keeping its original bytes for the undo", func() {
			old := fixtureImage("front.png")
			Expect(os.WriteFile(filepath.Join(dir, "cover.jpg"), old, 0o644)).To(Succeed())

			res, err := editor.WriteCover(ctx, al.ID, cover)
			Expect(err).ToNot(HaveOccurred())
			Expect(coverFile("cover.jpg")).To(Equal(cover))
			Expect(names()).To(ConsistOf("cover.jpg"))

			e := entry(res.Entry)
			Expect(e.OriginalImage.Sum).To(Equal(sumOfBytes(old)))
			kept, err := editor.images.read(e.OriginalImage.Sum)
			Expect(err).ToNot(HaveOccurred())
			Expect(kept).To(Equal(old))
		})

		It("reuses a cover File named in other letter case, so a case-insensitive share gets no second one", func() {
			old := fixtureImage("front.png")
			Expect(os.WriteFile(filepath.Join(dir, "Cover.JPG"), old, 0o644)).To(Succeed())

			res, err := editor.WriteCover(ctx, al.ID, cover)
			Expect(err).ToNot(HaveOccurred())
			Expect(names()).To(ConsistOf("Cover.JPG"))
			Expect(coverFile("Cover.JPG")).To(Equal(cover))
			Expect(entry(res.Entry).NewPath).To(Equal("Band/Record/Cover.JPG"))
		})

		It("reuses a cover.jpeg, which would otherwise win over a new cover.jpg", func() {
			Expect(os.WriteFile(filepath.Join(dir, "cover.jpeg"), fixtureImage("front.png"), 0o644)).To(Succeed())
			_, err := editor.WriteCover(ctx, al.ID, cover)
			Expect(err).ToNot(HaveOccurred())
			Expect(names()).To(ConsistOf("cover.jpeg"))
			Expect(coverFile("cover.jpeg")).To(Equal(cover))
		})

		It("writes nothing when the folder's cover is that image already", func() {
			Expect(os.WriteFile(filepath.Join(dir, "cover.jpg"), cover, 0o644)).To(Succeed())
			res, err := editor.WriteCover(ctx, al.ID, cover)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Changed).To(BeFalse())
			Expect(ds.EditJournal(ctx).CountAll()).To(BeZero())
		})

		It("writes a PNG as JPEG, since the File is cover.jpg", func() {
			res, err := editor.WriteCover(ctx, al.ID, fixtureImage("front.png"))
			Expect(err).ToNot(HaveOccurred())
			Expect(coverFile("cover.jpg")[:3]).To(Equal([]byte{0xFF, 0xD8, 0xFF}))
			Expect(entry(res.Entry).Image.Mime).To(Equal("image/jpeg"))
		})

		It("removes a chosen cover Correction even when the folder holds the image already", func() {
			Expect(os.WriteFile(filepath.Join(dir, "cover.jpg"), cover, 0o644)).To(Succeed())
			choice := model.Correction{Kind: model.CorrectionAlbumCover, MatchKey: model.AlbumFolderKey(al),
				From: "Band – Record", To: "Last.fm", ImageHash: "0123456789abcdef", ImageMime: "image/jpeg"}
			Expect(ds.Correction(ctx).Add(&choice)).To(Succeed())

			res, err := editor.WriteCover(ctx, al.ID, cover)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Changed).To(BeFalse())
			_, err = ds.Correction(ctx).Get(choice.ID)
			Expect(err).To(MatchError(model.ErrNotFound))
			_, err = ds.ArtworkQueue(ctx).Get(model.KindAlbumArtwork, al.ID, model.ImageTypePrimary)
			Expect(err).ToNot(HaveOccurred(), "the album's cover is resolved again")
		})

		It("refuses a truncated JPEG", func() {
			_, err := editor.WriteCover(ctx, al.ID, cover[:len(cover)/2])
			Expect(err).To(MatchError(ErrInvalidImage))
			Expect(names()).To(BeEmpty())
		})

		It("writes a transparent PNG on white", func() {
			img := image.NewNRGBA(image.Rect(0, 0, 4, 4)) // all transparent
			var buf bytes.Buffer
			Expect(png.Encode(&buf, img)).To(Succeed())
			_, err := editor.WriteCover(ctx, al.ID, buf.Bytes())
			Expect(err).ToNot(HaveOccurred())
			written, err := jpeg.Decode(bytes.NewReader(coverFile("cover.jpg")))
			Expect(err).ToNot(HaveOccurred())
			r, g, b, _ := written.At(1, 1).RGBA()
			Expect([]uint32{r >> 8, g >> 8, b >> 8}).To(HaveEach(BeNumerically(">", 250)))
		})

		It("refreshes the cover of its own folder's albums only, taking _ in a folder's name literally", func() {
			odd := addFolder(ctx, ds, "Band/Re_ord")
			Expect(os.MkdirAll(filepath.Join(libDir, "Band", "Re_ord"), 0o755)).To(Succeed())
			other := model.Album{ID: "al-odd", Name: "Re_ord", AlbumArtist: "Band", LibraryID: 1, FolderIDs: []string{odd.ID}}
			Expect(ds.Album(ctx).Put(&other)).To(Succeed())
			addAlbumTrack(ctx, ds, "mf-odd", other.ID, odd)

			_, err := editor.WriteCover(ctx, other.ID, cover)
			Expect(err).ToNot(HaveOccurred())
			_, err = ds.ArtworkQueue(ctx).Get(model.KindAlbumArtwork, other.ID, model.ImageTypePrimary)
			Expect(err).ToNot(HaveOccurred())
			_, err = ds.ArtworkQueue(ctx).Get(model.KindAlbumArtwork, al.ID, model.ImageTypePrimary)
			Expect(err).To(MatchError(model.ErrNotFound), "Band/Record isn't Band/Re_ord")
		})

		It("refuses bytes that aren't an image", func() {
			_, err := editor.WriteCover(ctx, al.ID, []byte("not an image"))
			Expect(err).To(MatchError(ErrInvalidImage))
			Expect(names()).To(BeEmpty())
		})

		It("removes the album's chosen cover Correction once written, so it no longer hides the File", func() {
			choice := model.Correction{Kind: model.CorrectionAlbumCover, MatchKey: model.AlbumFolderKey(al),
				From: "Band – Record", To: "Upload: x.jpg", ImageHash: "0123456789abcdef", ImageMime: "image/jpeg"}
			Expect(ds.Correction(ctx).Add(&choice)).To(Succeed())
			block := model.Correction{Kind: model.CorrectionAlbumCoverBlock, MatchKey: model.AlbumFolderKey(al),
				From: "Band – Record", To: "folder.jpg", ImageHash: "fedcba9876543210"}
			Expect(ds.Correction(ctx).Add(&block)).To(Succeed())

			_, err := editor.WriteCover(ctx, al.ID, cover)
			Expect(err).ToNot(HaveOccurred())

			_, err = ds.Correction(ctx).Get(choice.ID)
			Expect(err).To(MatchError(model.ErrNotFound))
			// Marking an image wrong stays as it is.
			Expect(ds.Correction(ctx).Get(block.ID)).ToNot(BeNil())
			all, err := ds.Correction(ctx).GetAll()
			Expect(err).ToNot(HaveOccurred())
			Expect(all).To(HaveLen(1), "no Correction is created")
		})

		It("keeps the chosen cover Correction when the write fails", func() {
			choice := model.Correction{Kind: model.CorrectionAlbumCover, MatchKey: model.AlbumFolderKey(al),
				From: "Band – Record", To: "Upload: x.jpg", ImageHash: "0123456789abcdef", ImageMime: "image/jpeg"}
			Expect(ds.Correction(ctx).Add(&choice)).To(Succeed())
			editor.fsys = faultyFS{FS: editor.fsys, match: "vu-edit"}

			_, err := editor.WriteCover(ctx, al.ID, cover)
			Expect(err).To(HaveOccurred())
			Expect(names()).To(BeEmpty(), "nothing is left in the folder")
			Expect(ds.Correction(ctx).Get(choice.ID)).ToNot(BeNil())
			Expect(ds.EditJournal(ctx).GetAll()).To(ConsistOf(HaveField("State", model.EditFailed)))
		})

		It("puts the old cover back when the new one can't take its place", func() {
			old := fixtureImage("front.png")
			Expect(os.WriteFile(filepath.Join(dir, "cover.jpg"), old, 0o644)).To(Succeed())
			editor.fsys = faultyFS{FS: editor.fsys, match: "vu-edit"}

			_, err := editor.WriteCover(ctx, al.ID, cover)
			Expect(err).To(HaveOccurred())
			Expect(names()).To(ConsistOf("cover.jpg"))
			Expect(coverFile("cover.jpg")).To(Equal(old))
		})

		It("writes into the album's own folder above its disc folders", func() {
			cd1, cd2 := addFolder(ctx, ds, "Band/Box/CD1"), addFolder(ctx, ds, "Band/Box/CD2")
			addFolder(ctx, ds, "Band/Box")
			Expect(os.MkdirAll(filepath.Join(libDir, "Band", "Box", "CD1"), 0o755)).To(Succeed())
			box := model.Album{ID: "al-box", Name: "Box", AlbumArtist: "Band", LibraryID: 1, FolderIDs: []string{cd1.ID, cd2.ID}}
			Expect(ds.Album(ctx).Put(&box)).To(Succeed())
			addAlbumTrack(ctx, ds, "mf-b1", box.ID, cd1)
			addAlbumTrack(ctx, ds, "mf-b2", box.ID, cd2)

			res, err := editor.WriteCover(ctx, box.ID, cover)
			Expect(err).ToNot(HaveOccurred())
			Expect(entry(res.Entry).NewPath).To(Equal("Band/Box/cover.jpg"))
			Expect(filepath.Join(libDir, "Band", "Box", "cover.jpg")).To(BeAnExistingFile())
		})

		It("refuses a folder another album's Tracks are in too, as the cover would be theirs as well", func() {
			addAlbumTrack(ctx, ds, "mf-other", "al-other", folder)
			_, err := editor.WriteCover(ctx, al.ID, cover)
			Expect(err).To(MatchError(ErrSharedFolder))
			Expect(names()).To(BeEmpty())
		})

		It("refuses while the album's library is read-only", func() {
			core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Writable: false}}})
			_, err := editor.WriteCover(ctx, al.ID, cover)
			Expect(err).To(MatchError(ErrEditingOff))
			Expect(names()).To(BeEmpty())
		})

		It("warns when another image outranks cover.jpg in CoverArtPriority", func() {
			conf.Server.CoverArtPriority = "folder.*, cover.*, embedded"
			Expect(os.WriteFile(filepath.Join(dir, "folder.jpg"), fixtureImage("front.png"), 0o644)).To(Succeed())
			folder.ImageFiles = []string{"folder.jpg"}
			Expect(ds.Folder(ctx).Put(folder)).To(Succeed())

			res, err := editor.WriteCover(ctx, al.ID, cover)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Warning).To(ContainSubstring("folder.jpg"))
		})
	})

	Describe("undoing a cover write", func() {
		It("puts the replaced cover back byte for byte, and redoing writes the new one again", func() {
			old := fixtureImage("front.png")
			Expect(os.WriteFile(filepath.Join(dir, "cover.jpg"), old, 0o644)).To(Succeed())
			res, err := editor.WriteCover(ctx, al.ID, cover)
			Expect(err).ToNot(HaveOccurred())

			undo, err := editor.Undo(ctx, res.Entry)
			Expect(err).ToNot(HaveOccurred())
			Expect(coverFile("cover.jpg")).To(Equal(old))
			Expect(names()).To(ConsistOf("cover.jpg"))
			u := entry(undo.Entry)
			Expect(u.Kind).To(Equal(model.EditCover))
			Expect(u.UndoOf).To(Equal(res.Entry))

			_, err = editor.Undo(ctx, res.Entry)
			Expect(err).To(MatchError(ErrAlreadyUndone))

			_, err = editor.Undo(ctx, undo.Entry)
			Expect(err).ToNot(HaveOccurred())
			Expect(coverFile("cover.jpg")).To(Equal(cover))
		})

		It("removes a cover.jpg the write created, and redoing writes it again", func() {
			res, err := editor.WriteCover(ctx, al.ID, cover)
			Expect(err).ToNot(HaveOccurred())

			undo, err := editor.Undo(ctx, res.Entry)
			Expect(err).ToNot(HaveOccurred())
			Expect(names()).To(BeEmpty())
			Expect(scanner.GetScanFoldersCalls()).To(HaveLen(2), "the folder is rescanned after the undo too")

			_, err = editor.Undo(ctx, undo.Entry)
			Expect(err).ToNot(HaveOccurred())
			Expect(coverFile("cover.jpg")).To(Equal(cover))
		})

		It("refuses when the cover File was changed since, and leaves it", func() {
			res, err := editor.WriteCover(ctx, al.ID, cover)
			Expect(err).ToNot(HaveOccurred())
			other := fixtureImage("front.png")
			Expect(os.WriteFile(filepath.Join(dir, "cover.jpg"), other, 0o644)).To(Succeed())

			_, err = editor.Undo(ctx, res.Entry)
			Expect(err).To(MatchError(ErrCoverChanged))
			Expect(coverFile("cover.jpg")).To(Equal(other))
		})

		It("refuses an earlier write a later one replaced, until that one is undone", func() {
			first, err := editor.WriteCover(ctx, al.ID, cover)
			Expect(err).ToNot(HaveOccurred())
			second, err := editor.WriteCover(ctx, al.ID, fixtureImage("front.png"))
			Expect(err).ToNot(HaveOccurred())

			_, err = editor.Undo(ctx, first.Entry)
			Expect(err).To(MatchError(ErrChangedLater))
			_, err = editor.Undo(ctx, second.Entry)
			Expect(err).ToNot(HaveOccurred())
			_, err = editor.Undo(ctx, first.Entry)
			Expect(err).ToNot(HaveOccurred())
			Expect(names()).To(BeEmpty())
		})
	})

	Describe("Settle", func() {
		// pending records an interrupted cover write, as the write path does before it starts.
		pending := func(orig, img model.EditImage, copySum string) *model.EditEntry {
			GinkgoHelper()
			e := &model.EditEntry{JobID: "job-1", Kind: model.EditCover, State: model.EditPending, LibraryID: 1,
				Path: "Band/Record/cover.jpg", NewPath: "Band/Record/cover.jpg", OriginalImage: orig, Image: img, CopySum: copySum}
			Expect(ds.EditJournal(ctx).Put(e)).To(Succeed())
			return e
		}
		abs := func(name string) string { return filepath.Join(dir, name) }

		It("finishes a new cover whose checked copy is all that is there", func() {
			Expect(os.WriteFile(abs(".cover.vu-edit.jpg"), cover, 0o644)).To(Succeed())
			e := pending(model.EditImage{}, model.EditImage{Sum: sumOfBytes(cover)}, sumOfBytes(cover))

			editor.Settle(ctx)
			Expect(names()).To(ConsistOf("cover.jpg"))
			Expect(entry(e.ID).State).To(Equal(model.EditWritten))
			Expect(scanner.GetScanFoldersCalls()).To(HaveLen(1))
		})

		It("puts the old cover back when it was left moved aside", func() {
			old := fixtureImage("front.png")
			Expect(os.WriteFile(abs(".cover.vu-original.jpg"), old, 0o644)).To(Succeed())
			Expect(os.WriteFile(abs(".cover.vu-edit.jpg"), cover, 0o644)).To(Succeed())
			e := pending(model.EditImage{Sum: sumOfBytes(old)}, model.EditImage{Sum: sumOfBytes(cover)}, sumOfBytes(cover))

			editor.Settle(ctx)
			Expect(names()).To(ConsistOf("cover.jpg"))
			Expect(coverFile("cover.jpg")).To(Equal(old))
			Expect(entry(e.ID).State).To(Equal(model.EditFailed))
		})

		It("closes a write that stopped before its copy was checked, removing the copy", func() {
			Expect(os.WriteFile(abs(".cover.vu-edit.jpg"), cover[:100], 0o644)).To(Succeed())
			e := pending(model.EditImage{}, model.EditImage{Sum: sumOfBytes(cover)}, "")

			editor.Settle(ctx)
			Expect(names()).To(BeEmpty())
			Expect(entry(e.ID).State).To(Equal(model.EditFailed))
		})

		It("tells whether an interrupted removal (an undo) happened", func() {
			e := pending(model.EditImage{Sum: sumOfBytes(cover)}, model.EditImage{}, "")
			editor.Settle(ctx)
			Expect(entry(e.ID).State).To(Equal(model.EditWritten), "the cover is gone: it was removed")

			Expect(os.WriteFile(abs("cover.jpg"), cover, 0o644)).To(Succeed())
			e = pending(model.EditImage{Sum: sumOfBytes(cover)}, model.EditImage{}, "")
			editor.Settle(ctx)
			Expect(entry(e.ID).State).To(Equal(model.EditFailed), "the cover is still there")
		})
	})
})

// addFolder records a folder of library 1, as the scanner would.
func addFolder(ctx context.Context, ds model.DataStore, path string) *model.Folder {
	GinkgoHelper()
	f := model.NewFolder(model.Library{ID: 1}, path)
	Expect(ds.Folder(ctx).Put(f)).To(Succeed())
	return f
}

// addAlbumTrack records a Track of the album in the folder.
func addAlbumTrack(ctx context.Context, ds model.DataStore, id, albumID string, f *model.Folder) {
	GinkgoHelper()
	p := strings.TrimPrefix(f.Path+"/"+f.Name+"/"+id+".mp3", "/")
	mf := model.MediaFile{ID: id, LibraryID: 1, FolderID: f.ID, AlbumID: albumID, Path: p, Participants: model.Participants{}}
	Expect(ds.MediaFile(ctx).Put(&mf)).To(Succeed())
}
