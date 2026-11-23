package safefile

import (
	"errors"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// noLinkFS has no hard links, as SMB1.
type noLinkFS struct{ FS }

func (noLinkFS) Link(string, string) error { return errors.ErrUnsupported }

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

	for name, fsys := range map[string]FS{"with hard links": OS{}, "without hard links (SMB1)": noLinkFS{OS{}}} {
		It("moves the File, checked, and tells the hooks the copy's checksum before it is placed, "+name, func() {
			var copied []byte
			placed := false
			err := Move(fsys, src, dest, MoveHooks{
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
		Expect(Move(OS{}, src, dest, MoveHooks{})).To(Succeed())
		info, err := os.Stat(dest)
		Expect(err).ToNot(HaveOccurred())
		Expect(info.ModTime().Unix()).To(Equal(old.ModTime().Unix()))
	})

	It("never replaces a File at the destination, in any letter case", func() {
		Expect(os.WriteFile(filepath.Join(root, "new", "01 a.MP3"), []byte("other"), 0o644)).To(Succeed())
		Expect(Move(OS{}, src, dest, MoveHooks{})).To(MatchError(ErrDestTaken))
		Expect(tree(root)).To(Equal(map[string]string{"old/a.mp3": "audio", "new/01 a.MP3": "other"}))
	})

	DescribeTable("leaves the File where it was, and nothing at the destination, when a step fails",
		func(fsys func() FS, hooks MoveHooks) {
			Expect(Move(fsys(), src, dest, hooks)).ToNot(Succeed())
			Expect(tree(root)).To(Equal(map[string]string{"old/a.mp3": "audio"}))
		},
		Entry("making the copy", func() FS { return faultyFS{FS: OS{}, op: "create", match: "vu-move"} }, MoveHooks{}),
		Entry("placing the copy", func() FS { return noLinkFS{faultyFS{FS: OS{}, op: "rename", match: "vu-move"}} }, MoveHooks{}),
		Entry("recording the copy", func() FS { return OS{} }, MoveHooks{Copied: func([]byte) error { return errInjected }}),
		Entry("recording the move", func() FS { return OS{} }, MoveHooks{Placed: func() error { return errInjected }}),
	)

	It("reports a moved File whose old copy can't be removed, for Settle to remove", func() {
		err := Move(faultyFS{FS: OS{}, op: "remove", match: "a.mp3"}, src, dest, MoveHooks{})
		Expect(err).To(MatchError(ErrSourceLeft))
		Expect(tree(root)).To(Equal(map[string]string{"old/a.mp3": "audio", "new/01 A.mp3": "audio"}))

		moved, err := SettleMove(OS{}, src, dest, sum("audio"))
		Expect(err).ToNot(HaveOccurred())
		Expect(moved).To(BeTrue())
		Expect(tree(root)).To(Equal(map[string]string{"new/01 A.mp3": "audio"}))
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
