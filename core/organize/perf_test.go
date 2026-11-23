package organize

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/navidrome/navidrome/core/librarymanager"
	"github.com/navidrome/navidrome/model"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The Library is ~23k Tracks (LOCAL-NOTES.md). A Library-wide plan reads every Track and folder
// once, and lists from disk only the existing folders albums go to (none here). Run with
// ORGANIZE_PERF=1 to measure it.
var _ = Describe("Plan over a Library-sized Library", func() {
	It("plans ~23k Tracks in a few seconds", func() {
		if os.Getenv("ORGANIZE_PERF") == "" {
			Skip("set ORGANIZE_PERF=1 to measure")
		}
		l := newLibrary()
		const albums, perAlbum = 1920, 12 // 23,040 Tracks
		Expect(l.ds.WithTx(func(tx model.DataStore) error {
			for a := range albums {
				dir := fmt.Sprintf("iTunes_Library/Artist %d/Album %d", a/8, a)
				Expect(os.MkdirAll(filepath.Join(l.dir, filepath.FromSlash(dir)), 0o755)).To(Succeed())
				Expect(os.WriteFile(filepath.Join(l.dir, filepath.FromSlash(dir), "cover.jpg"), nil, 0o644)).To(Succeed())
				for d := dir; d != "."; d = filepath.ToSlash(filepath.Dir(d)) {
					f := model.NewFolder(l.lib, d)
					if d == dir {
						f.ImageFiles = []string{"cover.jpg"}
					}
					if err := tx.Folder(l.ctx).Put(f); err != nil {
						return err
					}
				}
				for t := 1; t <= perAlbum; t++ {
					rel := fmt.Sprintf("%s/%02d track.mp3", dir, t)
					Expect(os.WriteFile(filepath.Join(l.dir, filepath.FromSlash(rel)), nil, 0o644)).To(Succeed())
					mf := model.MediaFile{LibraryID: 1, Path: rel, FolderID: model.FolderID(l.lib, dir), Suffix: "mp3",
						Title: fmt.Sprintf("Song %d", t), Artist: fmt.Sprintf("Artist %d", a/8), AlbumArtist: fmt.Sprintf("Artist %d", a/8),
						HasAlbumArtist: true, Album: fmt.Sprintf("Album %d", a), AlbumID: fmt.Sprintf("al-%d", a), Year: 1990 + a%30,
						TrackNumber: t, Participants: model.Participants{}}
					if err := tx.MediaFile(l.ctx).Put(&mf); err != nil {
						return err
					}
				}
			}
			return nil
		})).To(Succeed())

		o := New(l.ds, nil, nil)
		start := time.Now()
		p, err := o.Plan(l.ctx, Scope{Selection: librarymanager.Selection{Filter: map[string]any{}}})
		took := time.Since(start)
		Expect(err).ToNot(HaveOccurred())
		Expect(p.Moves).To(HaveLen(albums*perAlbum + albums))
		fmt.Printf("ORGANIZE_PERF: planned %d moves over %d Tracks in %s\n", len(p.Moves), albums*perAlbum, took)
		Expect(took).To(BeNumerically("<", 30*time.Second))
	})
})
