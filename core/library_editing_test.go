package core

import (
	"os"

	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DetectLibraryEditing", func() {
	var (
		ds    *tests.MockDataStore
		libs  *tests.MockLibraryRepo
		props *tests.MockedPropertyRepo
		music string
	)

	BeforeEach(func() {
		music = GinkgoT().TempDir()
		libs = &tests.MockLibraryRepo{}
		libs.SetData(model.Libraries{{ID: 1, Name: "Music", Path: music}})
		props = &tests.MockedPropertyRepo{}
		ds = &tests.MockDataStore{MockedLibrary: libs, MockedProperty: props}
	})

	It("turns editing on for a writable Library, and leaves nothing behind", func() {
		got, err := DetectLibraryEditing(GinkgoT().Context(), ds)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Enabled).To(BeTrue())
		Expect(got.Libraries).To(ConsistOf(LibraryWritable{ID: 1, Name: "Music", Writable: true}))
		entries, _ := os.ReadDir(music)
		Expect(entries).To(BeEmpty())
	})

	It("turns editing off when the Library's folder can't be written to", func() {
		if os.Geteuid() == 0 {
			Skip("root can write to a read-only folder")
		}
		Expect(os.Chmod(music, 0o555)).To(Succeed())
		DeferCleanup(os.Chmod, music, os.FileMode(0o755))

		got, err := DetectLibraryEditing(GinkgoT().Context(), ds)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Enabled).To(BeFalse())
		Expect(got.Libraries).To(ConsistOf(LibraryWritable{ID: 1, Name: "Music", Writable: false}))
	})

	It("turns editing off for a library that isn't a local folder, without writing anywhere", func() {
		cwd := GinkgoT().TempDir()
		GinkgoT().Chdir(cwd)
		Expect(os.MkdirAll("s3:/bucket", 0o755)).To(Succeed())
		libs.SetData(model.Libraries{{ID: 1, Name: "Music", Path: "s3://bucket"}})

		got, err := DetectLibraryEditing(GinkgoT().Context(), ds)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Enabled).To(BeFalse())
		entries, _ := os.ReadDir("s3:/bucket")
		Expect(entries).To(BeEmpty())
	})

	It("leaves the Inbox out: a writable Inbox doesn't turn editing on", func() {
		libs.SetData(model.Libraries{
			{ID: 1, Name: "Music", Path: GinkgoT().TempDir() + "/gone"},
			{ID: 2, Name: "Inbox", Path: GinkgoT().TempDir()},
		})
		Expect(props.Put(inboxLibraryIDKey, "2")).To(Succeed())

		got, err := DetectLibraryEditing(GinkgoT().Context(), ds)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Enabled).To(BeFalse())
		Expect(got.Libraries).To(ConsistOf(LibraryWritable{ID: 1, Name: "Music", Writable: false}))
	})
})
