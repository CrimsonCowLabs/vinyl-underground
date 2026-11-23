package quarantine

import (
	"os"
	"path/filepath"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core/safefile"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Quarantine", func() {
	var root string
	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		root = GinkgoT().TempDir()
	})
	mkdir := func(rel string) {
		GinkgoHelper()
		Expect(os.MkdirAll(filepath.Join(root, filepath.FromSlash(rel)), 0o755)).To(Succeed())
	}
	touch := func(rel string) {
		GinkgoHelper()
		mkdir(filepath.Dir(filepath.FromSlash(rel)))
		Expect(os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte("x"), 0o644)).To(Succeed())
	}

	Describe("Folder", func() {
		It("is the cleanup recipes' _Quarantine unless set", func() {
			Expect(Folder()).To(Equal("_Quarantine"))
		})
		It("is the configured folder, relative to the library", func() {
			conf.Server.QuarantineFolder = "/Trash/Quarantine/"
			Expect(Folder()).To(Equal("Trash/Quarantine"))
		})
		It("falls back to _Quarantine for a folder outside the library", func() {
			for _, bad := range []string{"..", "../elsewhere", "a/../../b", "."} {
				conf.Server.QuarantineFolder = bad
				Expect(Folder()).To(Equal("_Quarantine"), bad)
			}
		})
	})

	Describe("Within", func() {
		It("tells a path in Quarantine, in any letter case", func() {
			Expect(Within("_Quarantine/Artist/Song.mp3")).To(BeTrue())
			Expect(Within("_quarantine/Song.mp3")).To(BeTrue())
			Expect(Within("_Quarantine")).To(BeTrue())
			Expect(Within("_QuarantineX/Song.mp3")).To(BeFalse())
			Expect(Within("Artist/_Quarantine/Song.mp3")).To(BeFalse())
		})
	})

	Describe("Target", func() {
		It("keeps the File's relative path beneath Quarantine", func() {
			got, err := Target(safefile.OS{}, root, Into("Artist/Album/01 Song.mp3"))
			Expect(err).ToNot(HaveOccurred())
			Expect(got).To(Equal("_Quarantine/Artist/Album/01 Song.mp3"))
		})
		It("reuses folders already there whose names differ only in letter case", func() {
			mkdir("_quarantine/ARTIST/album")
			got, err := Target(safefile.OS{}, root, Into("Artist/Album/01 Song.mp3"))
			Expect(err).ToNot(HaveOccurred())
			Expect(got).To(Equal("_quarantine/ARTIST/album/01 Song.mp3"))
		})
		It("flags a File already at the target, in any letter case", func() {
			touch("_Quarantine/Artist/Album/01 song.MP3")
			_, err := Target(safefile.OS{}, root, Into("Artist/Album/01 Song.mp3"))
			Expect(err).To(MatchError(ErrTaken))
		})
		It("flags a folder at the target File's name", func() {
			mkdir("_Quarantine/Artist/Album/01 Song.mp3")
			_, err := Target(safefile.OS{}, root, Into("Artist/Album/01 Song.mp3"))
			Expect(err).To(MatchError(ErrTaken))
		})
		It("refuses a path through a File where a folder should be", func() {
			touch("_Quarantine/Artist")
			_, err := Target(safefile.OS{}, root, Into("Artist/Album/01 Song.mp3"))
			Expect(err).To(MatchError(ErrTaken))
		})
	})

	Describe("Original", func() {
		It("is a File's path with Quarantine taken off, whatever its letter case", func() {
			Expect(Original("_quarantine/Artist/Song.mp3")).To(Equal("Artist/Song.mp3"))
		})
	})

	Describe("Ensure", func() {
		It("makes the folder with a .ndignore, so the scanner skips it, and keeps one there", func() {
			Expect(Ensure(safefile.OS{}, root)).Error().ToNot(HaveOccurred())
			Expect(filepath.Join(root, "_Quarantine", ".ndignore")).To(BeARegularFile())
			Expect(os.WriteFile(filepath.Join(root, "_Quarantine", ".ndignore"), []byte("keep"), 0o644)).To(Succeed())
			Expect(Ensure(safefile.OS{}, root)).Error().ToNot(HaveOccurred())
			b, _ := os.ReadFile(filepath.Join(root, "_Quarantine", ".ndignore"))
			Expect(string(b)).To(Equal("keep"))
		})
		It("reuses a Quarantine folder in another letter case", func() {
			mkdir("_QUARANTINE")
			Expect(Ensure(safefile.OS{}, root)).Error().ToNot(HaveOccurred())
			entries, _ := os.ReadDir(root)
			Expect(entries).To(HaveLen(1))
			Expect(filepath.Join(root, "_QUARANTINE", ".ndignore")).To(BeARegularFile())
		})
	})

	Describe("Files", func() {
		It("lists every File in Quarantine but its .ndignore, with sizes, in path order", func() {
			touch("_Quarantine/.ndignore")
			touch("_Quarantine/B/2.mp3")
			touch("_Quarantine/A/1.mp3")
			touch("Outside/3.mp3")
			files, err := Files(safefile.OS{}, root)
			Expect(err).ToNot(HaveOccurred())
			Expect(files).To(Equal([]File{{Path: "_Quarantine/A/1.mp3", Size: 1}, {Path: "_Quarantine/B/2.mp3", Size: 1}}))
		})
		It("is empty with no Quarantine folder", func() {
			Expect(Files(safefile.OS{}, root)).To(BeEmpty())
		})
	})
})
