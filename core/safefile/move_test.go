package safefile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// noLinkFS has no hard links, as SMB1.
type noLinkFS struct{ FS }

func (noLinkFS) Link(string, string) error { return errors.ErrUnsupported }

// exdevFS can't rename a File to another folder, as across two mounts: the File's own renames
// (src to dest) fail with EXDEV; a move's temp renames still work.
type exdevFS struct{ FS }

func (f exdevFS) Rename(oldname, newname string) error {
	if !strings.Contains(oldname, "vu-move") {
		return &os.LinkError{Op: "rename", Old: oldname, New: newname, Err: syscall.EXDEV}
	}
	return f.FS.Rename(oldname, newname)
}

// racingFS is the real disk, where a File appears at appear just before a no-replace rename to
// it: the moment between the check that the destination is free and the rename.
type racingFS struct {
	OS
	appear string
}

func (f racingFS) RenameNoReplace(oldname, newname string) error {
	if newname == f.appear {
		_ = os.WriteFile(newname, []byte("raced"), 0o644)
	}
	return f.OS.RenameNoReplace(oldname, newname)
}

// lyingFS renames the File, then reports a failure, as SMB may; with swap, another File takes the
// renamed one's place at the new path.
type lyingFS struct {
	FS
	swap bool
}

func (f lyingFS) Rename(oldname, newname string) error {
	if err := f.FS.Rename(oldname, newname); err != nil {
		return err
	}
	if f.swap {
		_ = os.WriteFile(newname, []byte("another, longer File"), 0o644)
	}
	return errInjected
}

// backFaultyFS fails a rename from back: a move's rename back after its record failed.
type backFaultyFS struct {
	FS
	back string
}

func (f backFaultyFS) Rename(oldname, newname string) error {
	if oldname == f.back {
		return errInjected
	}
	return f.FS.Rename(oldname, newname)
}

// tree is every File under dir, by its path relative to dir: contents.
func tree(dir string) map[string]string {
	GinkgoHelper()
	res := map[string]string{}
	Expect(filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		res[filepath.ToSlash(rel)] = string(b)
		return nil
	})).To(Succeed())
	return res
}

var _ = Describe("Move", func() {
	var root, src, dest string

	BeforeEach(func() {
		root = GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(root, "old"), 0o755)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(root, "new"), 0o755)).To(Succeed())
		src = filepath.Join(root, "old", "a.mp3")
		dest = filepath.Join(root, "new", "01 A.mp3")
		Expect(os.WriteFile(src, []byte("audio"), 0o644)).To(Succeed())
	})

	Describe("on the same share: a rename", func() {
		It("renames the File, never copying it, and tells the hooks before and once it is placed", func() {
			var marked string
			placed := false
			err := Move(OS{}, src, dest, MoveHooks{
				Copied: func([]byte) error {
					Fail("a rename makes no copy")
					return nil
				},
				Renaming: func(info fs.FileInfo) error {
					marked = RenameMark(info)
					Expect(tree(root)).ToNot(HaveKey("new/01 A.mp3"))
					return nil
				},
				Placed: func() error {
					placed = true
					// No moment holds two copies.
					Expect(tree(root)).To(Equal(map[string]string{"new/01 A.mp3": "audio"}))
					return nil
				},
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(placed).To(BeTrue())
			Expect(marked).To(HavePrefix("moved:5:"))
			Expect(tree(root)).To(Equal(map[string]string{"new/01 A.mp3": "audio"}))
		})

		It("never replaces a File at the destination, in any letter case", func() {
			Expect(os.WriteFile(filepath.Join(root, "new", "01 a.MP3"), []byte("other"), 0o644)).To(Succeed())
			Expect(Move(OS{}, src, dest, MoveHooks{})).To(MatchError(ErrDestTaken))
			Expect(tree(root)).To(Equal(map[string]string{"old/a.mp3": "audio", "new/01 a.MP3": "other"}))
		})

		It("never replaces a File that appears at the destination after it was checked free", func() {
			// A plain rename on Linux CIFS may replace the File there: the no-replace rename refuses.
			err := Move(racingFS{OS: OS{}, appear: dest}, src, dest, MoveHooks{})
			Expect(err).To(MatchError(ErrDestTaken))
			Expect(tree(root)).To(Equal(map[string]string{"old/a.mp3": "audio", "new/01 A.mp3": "raced"}))
		})

		It("leaves the File where it was when the rename fails", func() {
			err := Move(faultyFS{FS: OS{}, op: "rename", match: "a.mp3"}, src, dest, MoveHooks{})
			Expect(err).To(MatchError(errInjected))
			Expect(tree(root)).To(Equal(map[string]string{"old/a.mp3": "audio"}))
		})

		It("takes a rename that reported an error, but happened, as made", func() {
			err := Move(lyingFS{FS: OS{}}, src, dest, MoveHooks{})
			Expect(err).ToNot(HaveOccurred())
			Expect(tree(root)).To(Equal(map[string]string{"new/01 A.mp3": "audio"}))
		})

		It("reports a File gone from both paths after a failed rename, never vouching for another at its new path", func() {
			err := Move(lyingFS{FS: OS{}, swap: true}, src, dest, MoveHooks{Placed: func() error {
				Fail("the File at the new path isn't the one renamed")
				return nil
			}})
			Expect(err).To(MatchError(ErrGone))
		})

		It("leaves the File where it was when the rename can't be recorded", func() {
			err := Move(OS{}, src, dest, MoveHooks{Renaming: func(fs.FileInfo) error { return errInjected }})
			Expect(err).To(MatchError(errInjected))
			Expect(tree(root)).To(Equal(map[string]string{"old/a.mp3": "audio"}))
		})

		It("renames the File back when the move can't be recorded once it is placed", func() {
			err := Move(OS{}, src, dest, MoveHooks{Placed: func() error { return errInjected }})
			Expect(err).To(MatchError(errInjected))
			Expect(tree(root)).To(Equal(map[string]string{"old/a.mp3": "audio"}))
		})

		It("leaves a renamed File for SettleRename when it can neither be recorded nor renamed back", func() {
			var mark string
			fsys := backFaultyFS{FS: OS{}, back: dest}
			err := Move(fsys, src, dest, MoveHooks{
				Renaming: func(info fs.FileInfo) error { mark = RenameMark(info); return nil },
				Placed:   func() error { return errInjected },
			})
			Expect(err).To(MatchError(ErrUnrecorded))
			Expect(tree(root)).To(Equal(map[string]string{"new/01 A.mp3": "audio"}))
			moved, err := SettleRename(OS{}, src, dest, mark)
			Expect(err).ToNot(HaveOccurred())
			Expect(moved).To(BeTrue())
		})
	})

	Describe("across filesystems (EXDEV): copy, check, delete", func() {
		for name, base := range map[string]FS{"with hard links": OS{}, "without hard links (SMB1)": noLinkFS{OS{}}} {
			It("copies the File, checked, and tells the hooks the copy's checksum before it is placed, "+name, func() {
				var copied []byte
				placed := false
				err := Move(exdevFS{base}, src, dest, MoveHooks{
					Copied: func(sum []byte) error {
						copied = sum
						Expect(tree(root)).ToNot(HaveKey("new/01 A.mp3"))
						return nil
					},
					Placed: func() error {
						placed = true
						// Both are there until the hook says the move is recorded.
						Expect(tree(root)).To(HaveKeyWithValue("old/a.mp3", "audio"))
						Expect(tree(root)).To(HaveKeyWithValue("new/01 A.mp3", "audio"))
						return nil
					},
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(copied).To(Equal(sum("audio")))
				Expect(placed).To(BeTrue())
				Expect(tree(root)).To(Equal(map[string]string{"new/01 A.mp3": "audio"}))
			})
		}

		It("keeps the File's modification time", func() {
			old, _ := os.Stat(src)
			Expect(Move(exdevFS{OS{}}, src, dest, MoveHooks{})).To(Succeed())
			info, err := os.Stat(dest)
			Expect(err).ToNot(HaveOccurred())
			Expect(info.ModTime().Unix()).To(Equal(old.ModTime().Unix()))
		})

		DescribeTable("leaves the File where it was, and nothing at the destination, when a step fails",
			func(fsys func() FS, hooks MoveHooks) {
				Expect(Move(fsys(), src, dest, hooks)).ToNot(Succeed())
				Expect(tree(root)).To(Equal(map[string]string{"old/a.mp3": "audio"}))
			},
			Entry("making the copy", func() FS { return exdevFS{faultyFS{FS: OS{}, op: "create", match: "vu-move"}} }, MoveHooks{}),
			Entry("placing the copy", func() FS { return exdevFS{noLinkFS{faultyFS{FS: OS{}, op: "rename", match: "vu-move"}}} }, MoveHooks{}),
			Entry("recording the copy", func() FS { return exdevFS{OS{}} }, MoveHooks{Copied: func([]byte) error { return errInjected }}),
			Entry("recording the move", func() FS { return exdevFS{OS{}} }, MoveHooks{Placed: func() error { return errInjected }}),
		)

		It("reports a moved File whose old copy can't be removed, for Settle to remove", func() {
			err := Move(exdevFS{faultyFS{FS: OS{}, op: "remove", match: "a.mp3"}}, src, dest, MoveHooks{})
			Expect(err).To(MatchError(ErrSourceLeft))
			Expect(tree(root)).To(Equal(map[string]string{"old/a.mp3": "audio", "new/01 A.mp3": "audio"}))

			moved, err := SettleMove(OS{}, src, dest, sum("audio"))
			Expect(err).ToNot(HaveOccurred())
			Expect(moved).To(BeTrue())
			Expect(tree(root)).To(Equal(map[string]string{"new/01 A.mp3": "audio"}))
		})

		It("leaves both copies for SettleMove when the move can't be recorded nor its copy removed", func() {
			err := Move(exdevFS{faultyFS{FS: OS{}, op: "remove", match: "01 A.mp3"}}, src, dest, MoveHooks{Placed: func() error { return errInjected }})
			Expect(err).To(MatchError(ErrUnrecorded))
			Expect(tree(root)).To(Equal(map[string]string{"old/a.mp3": "audio", "new/01 A.mp3": "audio"}))
			moved, err := SettleMove(OS{}, src, dest, sum("audio"))
			Expect(err).ToNot(HaveOccurred())
			Expect(moved).To(BeTrue())
			Expect(tree(root)).To(Equal(map[string]string{"new/01 A.mp3": "audio"}))
		})
	})

	It("refuses a File with other names: a copy would split it from them", func() {
		Expect(os.Link(src, filepath.Join(root, "old", "b.mp3"))).To(Succeed())
		Expect(Move(OS{}, src, dest, MoveHooks{})).To(MatchError(ErrLinked))
	})
})

var _ = Describe("SettleMove", func() {
	var root, src, dest string

	BeforeEach(func() {
		root = GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(root, "old"), 0o755)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(root, "new"), 0o755)).To(Succeed())
		src = filepath.Join(root, "old", "a.mp3")
		dest = filepath.Join(root, "new", "01 A.mp3")
	})
	write := func(name, s string) { Expect(os.WriteFile(name, []byte(s), 0o644)).To(Succeed()) }

	It("undoes a move stopped while copying: the File stays, the copy goes", func() {
		write(src, "audio")
		write(MoveTemp(dest), "aud")
		moved, err := SettleMove(OS{}, src, dest, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(moved).To(BeFalse())
		Expect(tree(root)).To(Equal(map[string]string{"old/a.mp3": "audio"}))
	})

	It("undoes a move stopped with the checked copy not yet placed", func() {
		write(src, "audio")
		write(MoveTemp(dest), "audio")
		moved, err := SettleMove(OS{}, src, dest, sum("audio"))
		Expect(err).ToNot(HaveOccurred())
		Expect(moved).To(BeFalse())
		Expect(tree(root)).To(Equal(map[string]string{"old/a.mp3": "audio"}))
	})

	It("finishes a move stopped once the copy was placed", func() {
		write(src, "audio")
		write(dest, "audio")
		moved, err := SettleMove(OS{}, src, dest, sum("audio"))
		Expect(err).ToNot(HaveOccurred())
		Expect(moved).To(BeTrue())
		Expect(tree(root)).To(Equal(map[string]string{"new/01 A.mp3": "audio"}))
	})

	It("finds a move that finished", func() {
		write(dest, "audio")
		moved, err := SettleMove(OS{}, src, dest, sum("audio"))
		Expect(err).ToNot(HaveOccurred())
		Expect(moved).To(BeTrue())
	})

	It("places the checked copy when it is all that is left", func() {
		write(MoveTemp(dest), "audio")
		moved, err := SettleMove(OS{}, src, dest, sum("audio"))
		Expect(err).ToNot(HaveOccurred())
		Expect(moved).To(BeTrue())
		Expect(tree(root)).To(Equal(map[string]string{"new/01 A.mp3": "audio"}))
	})

	It("leaves another File at the destination alone, and the File where it is", func() {
		write(src, "audio")
		write(dest, "other")
		moved, err := SettleMove(OS{}, src, dest, sum("audio"))
		Expect(err).ToNot(HaveOccurred())
		Expect(moved).To(BeFalse())
		Expect(tree(root)).To(Equal(map[string]string{"old/a.mp3": "audio", "new/01 A.mp3": "other"}))
	})

	It("never removes the last copy of a File it can't vouch for", func() {
		write(MoveTemp(dest), "half")
		_, err := SettleMove(OS{}, src, dest, sum("audio"))
		Expect(err).To(HaveOccurred())
		Expect(tree(root)).To(HaveKey("new/" + filepath.Base(MoveTemp(dest))))
	})
})

var _ = Describe("SettleRename", func() {
	var root, src, dest, mark string

	BeforeEach(func() {
		root = GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(root, "old"), 0o755)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(root, "new"), 0o755)).To(Succeed())
		src = filepath.Join(root, "old", "a.mp3")
		dest = filepath.Join(root, "new", "01 A.mp3")
		Expect(os.WriteFile(src, []byte("audio"), 0o644)).To(Succeed())
		info, err := os.Stat(src)
		Expect(err).ToNot(HaveOccurred())
		mark = RenameMark(info)
	})

	It("finds a rename that never happened: the File is at its old path only", func() {
		moved, err := SettleRename(OS{}, src, dest, mark)
		Expect(err).ToNot(HaveOccurred())
		Expect(moved).To(BeFalse())
	})

	It("finds a rename that happened: the File is at its new path only", func() {
		Expect(os.Rename(src, dest)).To(Succeed())
		moved, err := SettleRename(OS{}, src, dest, mark)
		Expect(err).ToNot(HaveOccurred())
		Expect(moved).To(BeTrue())
	})

	It("takes a File at both paths as not moved: the one at the new path is another", func() {
		Expect(os.WriteFile(dest, []byte("other"), 0o644)).To(Succeed())
		moved, err := SettleRename(OS{}, src, dest, mark)
		Expect(err).ToNot(HaveOccurred())
		Expect(moved).To(BeFalse())
		Expect(tree(root)).To(HaveLen(2))
	})

	It("doesn't vouch for a File at the new path only that isn't the one renamed", func() {
		Expect(os.Remove(src)).To(Succeed())
		Expect(os.WriteFile(dest, []byte("other File"), 0o644)).To(Succeed())
		_, err := SettleRename(OS{}, src, dest, mark)
		Expect(err).To(HaveOccurred())
	})

	It("reports a File that is at neither path", func() {
		Expect(os.Remove(src)).To(Succeed())
		_, err := SettleRename(OS{}, src, dest, mark)
		Expect(err).To(HaveOccurred())
	})

	It("removes the temp of a copy across filesystems a stop left, journaled as a rename", func() {
		Expect(os.WriteFile(MoveTemp(dest), []byte("aud"), 0o644)).To(Succeed())
		moved, err := SettleRename(OS{}, src, dest, mark)
		Expect(err).ToNot(HaveOccurred())
		Expect(moved).To(BeFalse())
		Expect(tree(root)).To(Equal(map[string]string{"old/a.mp3": "audio"}))
	})

	It("tells a rename's mark from a copy's checksum", func() {
		Expect(IsRenameMark(mark)).To(BeTrue())
		Expect(IsRenameMark("ab12")).To(BeFalse())
	})
})

var _ = Describe("EnsureDir", func() {
	var root string

	BeforeEach(func() {
		root = GinkgoT().TempDir()
	})

	It("makes the folders that aren't there", func() {
		rel, err := EnsureDir(OS{}, root, "Daft Punk/Discovery (2001)")
		Expect(err).ToNot(HaveOccurred())
		Expect(rel).To(Equal("Daft Punk/Discovery (2001)"))
		Expect(filepath.Join(root, "Daft Punk", "Discovery (2001)")).To(BeADirectory())
	})

	It("reuses a folder whose name differs only in letter case, never making its twin", func() {
		Expect(os.MkdirAll(filepath.Join(root, "DAFT PUNK", "discovery (2001)"), 0o755)).To(Succeed())
		rel, err := EnsureDir(OS{}, root, "Daft Punk/Discovery (2001)")
		Expect(err).ToNot(HaveOccurred())
		Expect(rel).To(Equal("DAFT PUNK/discovery (2001)"))
		entries, _ := os.ReadDir(root)
		Expect(entries).To(HaveLen(1))
	})

	It("refuses a folder with a twin already: which one a path reaches on the NAS is unpredictable", func() {
		Expect(os.MkdirAll(filepath.Join(root, "Dire Straits"), 0o755)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(root, "DIRE STRAITS"), 0o755)).To(Succeed())
		_, err := EnsureDir(OS{}, root, "Dire Straits/Brothers in Arms (1985)")
		Expect(err).To(MatchError(ErrCaseTwin))
	})

	It("refuses a File in the way", func() {
		Expect(os.WriteFile(filepath.Join(root, "A"), nil, 0o644)).To(Succeed())
		_, err := EnsureDir(OS{}, root, "a/B")
		Expect(err).To(HaveOccurred())
	})

	It("refuses a folder that leads out of the root", func() {
		outside := GinkgoT().TempDir()
		Expect(os.Symlink(outside, filepath.Join(root, "A"))).To(Succeed())
		_, err := EnsureDir(OS{}, root, "A/B")
		Expect(err).To(MatchError(ErrOutside))
	})
})

var _ = Describe("RemoveEmptyDirs", func() {
	It("removes the folder and each folder above it left empty, up to the root", func() {
		root := GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(root, "iTunes", "Artist", "Album"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(root, "iTunes", "keep.txt"), nil, 0o644)).To(Succeed())
		removed := RemoveEmptyDirs(OS{}, root, "iTunes/Artist/Album")
		Expect(removed).To(Equal([]string{"iTunes/Artist/Album", "iTunes/Artist"}))
		Expect(tree(root)).To(Equal(map[string]string{"iTunes/keep.txt": ""}))
		Expect(filepath.Join(root, "iTunes")).To(BeADirectory())
	})

	It("leaves a folder with anything in it", func() {
		root := GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(root, "A"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(root, "A", ".DS_Store"), nil, 0o644)).To(Succeed())
		Expect(RemoveEmptyDirs(OS{}, root, "A")).To(BeEmpty())
	})
})
