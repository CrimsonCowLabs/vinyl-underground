package safefile

import (
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var errInjected = errors.New("injected failure")

// faultyFS fails every call of op on a path containing match.
type faultyFS struct {
	FS
	op, match string
}

func (f faultyFS) fail(op string, names ...string) bool {
	for _, n := range names {
		if op == f.op && strings.Contains(n, f.match) {
			return true
		}
	}
	return false
}

func (f faultyFS) Rename(oldname, newname string) error {
	if f.fail("rename", oldname) {
		return errInjected
	}
	return f.FS.Rename(oldname, newname)
}

func (f faultyFS) Remove(name string) error {
	if f.fail("remove", name) {
		return errInjected
	}
	return f.FS.Remove(name)
}

// list is what is in dir, by name: contents.
func list(dir string) map[string]string {
	GinkgoHelper()
	entries, err := os.ReadDir(dir)
	Expect(err).ToNot(HaveOccurred())
	res := map[string]string{}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		Expect(err).ToNot(HaveOccurred())
		res[e.Name()] = string(b)
	}
	return res
}

func sum(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

var _ = Describe("Replace", func() {
	var dir, path, temp string

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		path = filepath.Join(dir, "01 Song.mp3")
		temp = TempName(path)
		Expect(os.WriteFile(path, []byte("original"), 0o644)).To(Succeed())
		Expect(os.WriteFile(temp, []byte("edited"), 0o644)).To(Succeed())
	})

	It("puts the copy in the File's place, and leaves nothing else behind", func() {
		Expect(Replace(OS{}, path, temp)).To(Succeed())
		Expect(list(dir)).To(Equal(map[string]string{"01 Song.mp3": "edited"}))
	})

	It("keeps the temp and the backup hidden, with the File's extension (TagLib picks the format by it)", func() {
		Expect(filepath.Base(TempName(path))).To(Equal(".01 Song.vu-edit.mp3"))
		Expect(filepath.Base(BackupName(path))).To(Equal(".01 Song.vu-original.mp3"))
	})

	It("leaves the File untouched when it can't be moved aside", func() {
		err := Replace(faultyFS{FS: OS{}, op: "rename", match: "01 Song.mp3"}, path, temp)
		Expect(err).To(MatchError(errInjected))
		Expect(list(dir)).To(HaveKeyWithValue("01 Song.mp3", "original"))
		Expect(list(dir)).ToNot(HaveKey(filepath.Base(BackupName(path))))
	})

	It("puts the File back when the copy can't take its place", func() {
		err := Replace(faultyFS{FS: OS{}, op: "rename", match: "vu-edit"}, path, temp)
		Expect(err).To(MatchError(errInjected))
		Expect(list(dir)).To(HaveKeyWithValue("01 Song.mp3", "original"))
		Expect(list(dir)).ToNot(HaveKey(filepath.Base(BackupName(path))))
	})

	It("keeps the original as the hidden backup when it can't even be put back, so Settle can", func() {
		fsys := faultyFS{FS: faultyFS{FS: OS{}, op: "rename", match: "vu-edit"}, op: "rename", match: "vu-original"}
		err := Replace(fsys, path, temp)
		Expect(err).To(MatchError(ErrInterrupted))
		Expect(list(dir)).To(HaveKeyWithValue(filepath.Base(BackupName(path)), "original"))

		written, err := Settle(OS{}, path, sum("edited"))
		Expect(err).ToNot(HaveOccurred())
		Expect(written).To(BeFalse())
		Expect(list(dir)).To(Equal(map[string]string{"01 Song.mp3": "original"}))
	})

	It("succeeds when only the backup can't be removed, leaving it for Settle", func() {
		Expect(Replace(faultyFS{FS: OS{}, op: "remove", match: "vu-original"}, path, temp)).To(MatchError(ErrBackupLeft))
		Expect(list(dir)).To(HaveKeyWithValue("01 Song.mp3", "edited"))

		written, err := Settle(OS{}, path, sum("edited"))
		Expect(err).ToNot(HaveOccurred())
		Expect(written).To(BeTrue())
		Expect(list(dir)).To(Equal(map[string]string{"01 Song.mp3": "edited"}))
	})

	It("refuses to start while an earlier replace's backup is still there", func() {
		Expect(os.WriteFile(BackupName(path), []byte("older"), 0o644)).To(Succeed())
		Expect(Replace(OS{}, path, temp)).To(MatchError(ErrInterrupted))
		Expect(list(dir)).To(HaveKeyWithValue("01 Song.mp3", "original"))
		Expect(list(dir)).To(HaveKeyWithValue(filepath.Base(BackupName(path)), "older"))
	})
})

var _ = Describe("Settle", func() {
	var dir, path string

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		path = filepath.Join(dir, "01 Song.mp3")
	})
	write := func(name, s string) {
		Expect(os.WriteFile(name, []byte(s), 0o644)).To(Succeed())
	}

	It("finds a replace that was never started: the File is the original, the temp goes", func() {
		write(path, "original")
		write(TempName(path), "edited")
		written, err := Settle(OS{}, path, sum("edited"))
		Expect(err).ToNot(HaveOccurred())
		Expect(written).To(BeFalse())
		Expect(list(dir)).To(Equal(map[string]string{"01 Song.mp3": "original"}))
	})

	It("finds a replace that finished", func() {
		write(path, "edited")
		written, err := Settle(OS{}, path, sum("edited"))
		Expect(err).ToNot(HaveOccurred())
		Expect(written).To(BeTrue())
	})

	It("puts the original back when the app stopped with the File moved aside", func() {
		write(BackupName(path), "original")
		write(TempName(path), "edited")
		written, err := Settle(OS{}, path, sum("edited"))
		Expect(err).ToNot(HaveOccurred())
		Expect(written).To(BeFalse())
		Expect(list(dir)).To(Equal(map[string]string{"01 Song.mp3": "original"}))
	})

	It("puts a checked copy in place when it is all that is left", func() {
		write(TempName(path), "edited")
		written, err := Settle(OS{}, path, sum("edited"))
		Expect(err).ToNot(HaveOccurred())
		Expect(written).To(BeTrue())
		Expect(list(dir)).To(Equal(map[string]string{"01 Song.mp3": "edited"}))
	})

	It("never removes the last copy of a File it can't vouch for", func() {
		write(TempName(path), "half-written")
		_, err := Settle(OS{}, path, sum("edited"))
		Expect(err).To(HaveOccurred())
		Expect(list(dir)).To(HaveKey(filepath.Base(TempName(path))))
	})
})
