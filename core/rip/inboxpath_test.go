package rip

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("inboxPath", func() {
	var inbox string

	BeforeEach(func() {
		inbox = GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(inbox, "Album"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(inbox, "Album", "song.opus"), []byte("x"), 0o600)).To(Succeed())
	})

	It("resolves a File inside the Inbox", func() {
		p, err := inboxPath(inbox, "Album/song.opus")
		Expect(err).ToNot(HaveOccurred())
		real, _ := filepath.EvalSymlinks(filepath.Join(inbox, "Album", "song.opus"))
		Expect(p).To(Equal(real))
	})

	It("resolves a File the Rip hasn't written yet, as long as its folder is inside", func() {
		p, err := inboxPath(inbox, "Album/gone.opus")
		Expect(err).ToNot(HaveOccurred())
		Expect(filepath.Base(p)).To(Equal("gone.opus"))
	})

	DescribeTable("refuses paths that leave the Inbox",
		func(rel string) {
			_, err := inboxPath(inbox, rel)
			Expect(err).To(MatchError(ErrOutsideInbox))
		},
		Entry("parent", "../etc/passwd"),
		Entry("parent in the middle", "Album/../../x.opus"),
		Entry("absolute", "/etc/passwd"),
		Entry("empty", ""),
		Entry("the Inbox itself", "."),
	)

	It("refuses a symlink that points out of the Inbox", func() {
		outside := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(outside, "secret.opus"), []byte("x"), 0o600)).To(Succeed())
		Expect(os.Symlink(filepath.Join(outside, "secret.opus"), filepath.Join(inbox, "link.opus"))).To(Succeed())
		Expect(os.Symlink(outside, filepath.Join(inbox, "Elsewhere"))).To(Succeed())

		_, err := inboxPath(inbox, "link.opus")
		Expect(err).To(MatchError(ErrOutsideInbox))
		_, err = inboxPath(inbox, "Elsewhere/secret.opus")
		Expect(err).To(MatchError(ErrOutsideInbox))
	})

	It("refuses a File that is a symlink, even one inside the Inbox", func() {
		Expect(os.Symlink(filepath.Join(inbox, "Album", "song.opus"), filepath.Join(inbox, "alias.opus"))).To(Succeed())

		_, err := inboxPath(inbox, "alias.opus")
		Expect(err).To(MatchError(ErrOutsideInbox))
	})
})
