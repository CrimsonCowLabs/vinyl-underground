package rip

import (
	"os"
	"path/filepath"

	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CheckPromoteFolder", func() {
	var (
		ds           *tests.MockDataStore
		music, inbox string
		promote      string
	)

	BeforeEach(func() {
		music = GinkgoT().TempDir()
		inbox = GinkgoT().TempDir()
		promote = filepath.Join(music, "Vinyl Underground")
		Expect(os.Mkdir(promote, 0o755)).To(Succeed())
		libs := &tests.MockLibraryRepo{}
		libs.SetData(model.Libraries{{ID: 1, Name: "Music", Path: music}, {ID: 2, Name: "Inbox", Path: inbox}})
		props := &tests.MockedPropertyRepo{}
		Expect(props.Put("InboxLibraryID", "2")).To(Succeed())
		ds = &tests.MockDataStore{MockedLibrary: libs, MockedProperty: props}
	})

	check := func(path string) (string, error) {
		ctx := request.WithUser(GinkgoT().Context(), model.User{ID: "admin", IsAdmin: true})
		return CheckPromoteFolder(ctx, ds, path)
	}

	It("is off, with no error, when no folder is set", func() {
		Expect(check("")).To(BeEmpty())
	})

	It("accepts a writable folder inside the Library, and leaves nothing behind", func() {
		Expect(check(promote + "/")).To(Equal(promote))
		entries, _ := os.ReadDir(promote)
		Expect(entries).To(BeEmpty())
	})

	DescribeTable("refuses a folder Promote can't safely write to",
		func(path func() string, msg string) {
			got, err := check(path())
			Expect(got).To(BeEmpty())
			Expect(err).To(MatchError(ContainSubstring(msg)))
		},
		Entry("relative", func() string { return "Vinyl Underground" }, "absolute"),
		Entry("missing", func() string { return filepath.Join(music, "Nope") }, "no such file"),
		Entry("a File", func() string {
			p := filepath.Join(music, "file.mp3")
			Expect(os.WriteFile(p, nil, 0o644)).To(Succeed())
			return p
		}, "not a folder"),
		Entry("the Library's own folder", func() string { return music }, "inside a library"),
		Entry("outside every library", func() string { return GinkgoT().TempDir() }, "inside a library"),
		Entry("inside the Inbox", func() string {
			p := filepath.Join(inbox, "Vinyl Underground")
			Expect(os.Mkdir(p, 0o755)).To(Succeed())
			return p
		}, "inside a library"),
		Entry("read-only", func() string {
			Expect(os.Chmod(promote, 0o555)).To(Succeed())
			DeferCleanup(os.Chmod, promote, os.FileMode(0o755))
			return promote
		}, "not writable"),
	)
})
