package rip

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("placeCopy", func() {
	var (
		src, destDir, dest string
		content            []byte
		mtime              time.Time
	)

	BeforeEach(func() {
		src = filepath.Join(GinkgoT().TempDir(), "song.opus")
		destDir = GinkgoT().TempDir()
		dest = filepath.Join(destDir, "01 Song.opus")
		content = bytes.Repeat([]byte("vinyl underground "), 50_000) // ~900 KB: several writes
		Expect(os.WriteFile(src, content, 0o644)).To(Succeed())
		mtime = time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)
		Expect(os.Chtimes(src, mtime, mtime)).To(Succeed())
	})

	// onlyDest checks that destDir holds just the placed File, or nothing at all: no temp left.
	destDirHolds := func(names ...string) {
		entries, err := os.ReadDir(destDir)
		Expect(err).ToNot(HaveOccurred())
		var got []string
		for _, e := range entries {
			got = append(got, e.Name())
		}
		if len(names) == 0 {
			Expect(got).To(BeEmpty())
		} else {
			Expect(got).To(ConsistOf(names))
		}
	}
	srcIntact := func() {
		Expect(os.ReadFile(src)).To(Equal(content))
	}

	It("copies the File into place, keeps its time, and leaves the source alone", func() {
		Expect(placeCopy(osFS{}, src, dest)).To(Succeed())
		Expect(os.ReadFile(dest)).To(Equal(content))
		info, err := os.Stat(dest)
		Expect(err).ToNot(HaveOccurred())
		Expect(info.ModTime().Equal(mtime)).To(BeTrue())
		destDirHolds("01 Song.opus")
		srcIntact()
	})

	It("never replaces a File that appeared at the destination meanwhile", func() {
		Expect(os.WriteFile(dest, []byte("someone else's"), 0o644)).To(Succeed())
		Expect(placeCopy(osFS{}, src, dest)).To(MatchError(ErrDestTaken))
		Expect(os.ReadFile(dest)).To(Equal([]byte("someone else's")))
		destDirHolds("01 Song.opus")
		srcIntact()
	})

	It("falls back to a rename where hard links aren't supported (SMB1), still never replacing", func() {
		fsys := &faultyFS{promoteFS: osFS{}, op: "link", match: dest, err: &os.LinkError{Op: "link", Err: syscall.EOPNOTSUPP}}
		Expect(placeCopy(fsys, src, dest)).To(Succeed())
		Expect(os.ReadFile(dest)).To(Equal(content))
		destDirHolds("01 Song.opus")

		other := filepath.Join(destDir, "02 Other.opus")
		Expect(os.WriteFile(other, []byte("keep"), 0o644)).To(Succeed())
		fsys = &faultyFS{promoteFS: osFS{}, op: "link", match: other, err: &os.LinkError{Op: "link", Err: syscall.EPERM}}
		Expect(placeCopy(fsys, src, other)).To(MatchError(ErrDestTaken))
		Expect(os.ReadFile(other)).To(Equal([]byte("keep")))
	})

	It("finds a File at the destination that differs only by letter case, as the NAS would", func() {
		Expect(os.WriteFile(filepath.Join(destDir, "01 SONG.opus"), []byte("theirs"), 0o644)).To(Succeed())
		fsys := &faultyFS{promoteFS: caseInsensitiveFS{root: destDir}, op: "link", match: dest, err: &os.LinkError{Op: "link", Err: syscall.EOPNOTSUPP}}
		Expect(placeCopy(fsys, src, dest)).To(MatchError(ErrDestTaken))
		Expect(os.ReadFile(filepath.Join(destDir, "01 SONG.opus"))).To(Equal([]byte("theirs")))
		destDirHolds("01 SONG.opus")
	})

	DescribeTable("a failure at any step leaves no File and no temp at the destination, and the source as it was",
		func(op string, corrupt bool) {
			fsys := &faultyFS{promoteFS: osFS{}, op: op, match: destDir, err: errInjected, corrupt: corrupt}
			if op == "link" {
				// A link that fails for another reason than "exists" falls back to a rename, so fail both.
				fsys = &faultyFS{promoteFS: &faultyFS{promoteFS: osFS{}, op: "rename", match: destDir, err: errInjected},
					op: "link", match: destDir, err: errInjected}
			}
			err := placeCopy(fsys, src, dest)
			Expect(err).To(HaveOccurred())
			if !corrupt {
				Expect(err).To(MatchError(errInjected))
			}
			destDirHolds()
			srcIntact()
		},
		Entry("making the temp", "create", false),
		Entry("writing", "write", false),
		Entry("flushing to disk", "sync", false),
		Entry("closing", "close", false),
		Entry("reading it back", "open", false),
		Entry("the copy doesn't match", "", true),
		Entry("linking and renaming into place", "link", false),
	)

	It("fails cleanly when the source can't be read, and when the destination folder is gone", func() {
		Expect(placeCopy(osFS{}, filepath.Join(filepath.Dir(src), "missing.opus"), dest)).To(HaveOccurred())
		destDirHolds()
		Expect(placeCopy(osFS{}, src, filepath.Join(destDir, "gone", "x.opus"))).To(HaveOccurred())
		destDirHolds()
	})

	It("replaces a temp left behind by an interrupted earlier try", func() {
		Expect(os.WriteFile(promoteTemp(dest), []byte("half"), 0o644)).To(Succeed())
		Expect(placeCopy(osFS{}, src, dest)).To(Succeed())
		destDirHolds("01 Song.opus")
	})
})
