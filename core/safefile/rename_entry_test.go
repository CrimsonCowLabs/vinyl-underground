package safefile

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// twinFS is the case-insensitive NAS over SMB1, where a path into one of two names differing
// only in letter case may reach the other: a rename of from renames to instead.
type twinFS struct {
	OS
	from, to string
}

func (f twinFS) RenameNoReplace(oldname, newname string) error {
	if oldname == f.from {
		oldname = f.to
	}
	return f.OS.RenameNoReplace(oldname, newname)
}

var _ = Describe("RenameEntry", func() {
	var root string

	BeforeEach(func() {
		root = GinkgoT().TempDir()
		for _, f := range []string{"Music/ATB/a.mp3", "Music/Atb/b.mp3", "Music/Queen/c.mp3"} {
			abs := filepath.Join(root, filepath.FromSlash(f))
			Expect(os.MkdirAll(filepath.Dir(abs), 0o755)).To(Succeed())
			Expect(os.WriteFile(abs, []byte(f), 0o644)).To(Succeed())
		}
	})
	at := func(rel string) string { return filepath.Join(root, filepath.FromSlash(rel)) }

	It("renames a folder with all it holds, and tells the name it renamed", func() {
		got, err := RenameEntry(OS{}, at("Music/Atb"), at("Music/Atb [case-merge]"))
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(Equal(at("Music/Atb")))
		Expect(tree(root)).To(Equal(map[string]string{
			"Music/ATB/a.mp3":              "Music/ATB/a.mp3",
			"Music/Atb [case-merge]/b.mp3": "Music/Atb/b.mp3",
			"Music/Queen/c.mp3":            "Music/Queen/c.mp3",
		}))
	})

	It("tells the twin's name when the server renames the twin instead (SMB1)", func() {
		fsys := twinFS{from: at("Music/Atb"), to: at("Music/ATB")}
		got, err := RenameEntry(fsys, at("Music/Atb"), at("Music/Atb [case-merge]"))
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(Equal(at("Music/ATB")))
		Expect(tree(root)).To(HaveKeyWithValue("Music/Atb [case-merge]/a.mp3", "Music/ATB/a.mp3"))
	})

	It("moves a folder into another folder", func() {
		got, err := RenameEntry(OS{}, at("Music/Queen"), at("Music/Atb/Queen"))
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(Equal(at("Music/Queen")))
		Expect(tree(root)).To(HaveKey("Music/Atb/Queen/c.mp3"))
	})

	It("renames a File", func() {
		got, err := RenameEntry(OS{}, at("Music/Queen/c.mp3"), at("Music/Queen/c [case-merge].mp3"))
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(Equal(at("Music/Queen/c.mp3")))
		Expect(tree(root)).To(HaveKey("Music/Queen/c [case-merge].mp3"))
	})

	It("never renames onto a name taken, in any letter case", func() {
		_, err := RenameEntry(OS{}, at("Music/Queen"), at("Music/atb"))
		Expect(err).To(MatchError(ErrDestTaken))
		Expect(tree(root)).To(HaveKey("Music/Queen/c.mp3"))
	})

	It("refuses what isn't there", func() {
		_, err := RenameEntry(OS{}, at("Music/Nope"), at("Music/Nope 2"))
		Expect(err).To(HaveOccurred())
	})

	It("refuses a link", func() {
		Expect(os.Symlink(at("Music/Queen"), at("Music/Link"))).To(Succeed())
		_, err := RenameEntry(OS{}, at("Music/Link"), at("Music/Link 2"))
		Expect(err).To(MatchError(ErrLinked))
	})
})

// plainFS has no rename that refuses a File at the new name by itself.
type plainFS struct{ FS }

var _ = Describe("RenameBack", func() {
	var root string

	BeforeEach(func() {
		root = GinkgoT().TempDir()
		for _, f := range []string{"Music/ATB/a.mp3", "Music/Atb [case-merge]/b.mp3"} {
			abs := filepath.Join(root, filepath.FromSlash(f))
			Expect(os.MkdirAll(filepath.Dir(abs), 0o755)).To(Succeed())
			Expect(os.WriteFile(abs, []byte(f), 0o644)).To(Succeed())
		}
	})
	at := func(rel string) string { return filepath.Join(root, filepath.FromSlash(rel)) }

	It("puts back a name beside its twin where the filesystem holds both", func() {
		got, err := RenameBack(OS{}, at("Music/Atb [case-merge]"), at("Music/Atb"))
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(Equal(at("Music/Atb [case-merge]")))
		Expect(tree(root)).To(Equal(map[string]string{"Music/ATB/a.mp3": "Music/ATB/a.mp3", "Music/Atb/b.mp3": "Music/Atb [case-merge]/b.mp3"}))
	})

	It("refuses a twin where only a check before the rename could tell it (no RENAME_NOREPLACE)", func() {
		_, err := RenameBack(plainFS{OS{}}, at("Music/Atb [case-merge]"), at("Music/Atb"))
		Expect(err).To(MatchError(ErrDestTaken))
		Expect(tree(root)).To(HaveKey("Music/Atb [case-merge]/b.mp3"))
	})

	It("never renames onto the name itself", func() {
		_, err := RenameBack(OS{}, at("Music/Atb [case-merge]"), at("Music/ATB"))
		Expect(err).To(MatchError(ErrDestTaken))
	})

	It("is a plain RenameEntry when no twin is there", func() {
		_, err := RenameBack(plainFS{OS{}}, at("Music/Atb [case-merge]"), at("Music/Queen"))
		Expect(err).ToNot(HaveOccurred())
		Expect(tree(root)).To(HaveKey("Music/Queen/b.mp3"))
	})
})
