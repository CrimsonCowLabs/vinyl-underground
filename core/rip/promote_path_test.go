package rip

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("promoteLayout", func() {
	DescribeTable("names the folders and the File after the File's tags",
		func(tags FileTags, src string, artist, album, file string) {
			got := promoteLayout(tags, src)
			Expect(got).To(Equal(promoteNames{Artist: artist, Album: album, File: file}))
		},
		Entry("all tags", FileTags{AlbumArtist: "Daft Punk", Artist: "Daft Punk feat. Pharrell", Album: "Random Access Memories",
			Year: "2013", Title: "Get Lucky", TrackNumber: "8"}, "x.m4a",
			"Daft Punk", "Random Access Memories (2013)", "08 Get Lucky.m4a"),
		Entry("a full release date keeps only the year", FileTags{AlbumArtist: "A", Album: "B", Year: "2004-05-01", Title: "C", TrackNumber: "1"}, "x.opus",
			"A", "B (2004)", "01 C.opus"),
		Entry("track number with a total", FileTags{AlbumArtist: "A", Album: "B", Title: "C", TrackNumber: "3/12"}, "x.mp3",
			"A", "B", "03 C.mp3"),
		Entry("three-digit track number", FileTags{AlbumArtist: "A", Album: "B", Title: "C", TrackNumber: "112"}, "x.mp3",
			"A", "B", "112 C.mp3"),
		Entry("no album artist: the artist", FileTags{Artist: "Solo", Album: "B", Title: "C"}, "x.mp3",
			"Solo", "B", "C.mp3"),
		Entry("no artist at all", FileTags{Album: "B", Title: "C"}, "x.mp3",
			"Unknown Artist", "B", "C.mp3"),
		Entry("no album", FileTags{AlbumArtist: "A", Title: "C", Year: "1999"}, "x.mp3",
			"A", "Unknown Album (1999)", "C.mp3"),
		Entry("no year, no track number", FileTags{AlbumArtist: "A", Album: "B", Title: "C"}, "x.mp3",
			"A", "B", "C.mp3"),
		Entry("a year that isn't one", FileTags{AlbumArtist: "A", Album: "B", Title: "C", Year: "soon", TrackNumber: "0"}, "x.mp3",
			"A", "B", "C.mp3"),
		Entry("no title: the File's own name", FileTags{AlbumArtist: "A", Album: "B"}, "Some Song [abc].OPUS",
			"A", "B", "Some Song [abc].opus"),
		Entry("characters SMB refuses", FileTags{AlbumArtist: `AC/DC`, Album: `What? "Live" <1> | 2*`, Title: `a\b:c`}, "x.mp3",
			"AC_DC", `What_ _Live_ _1_ _ 2_`, "a_b_c.mp3"),
		Entry("trailing dots and spaces", FileTags{AlbumArtist: "R.E.M.", Album: "Up. . ", Title: "Song...  "}, "x.mp3",
			"R.E.M", "Up", "Song.mp3"),
		Entry("a single leading dot would hide the folder", FileTags{AlbumArtist: ".38 Special", Album: "...And Justice for All", Title: ".hidden"}, "x.mp3",
			"38 Special", "...And Justice for All", "hidden.mp3"),
		Entry("names that are only forbidden characters fall back", FileTags{AlbumArtist: " . ", Album: "..", Title: "  "}, "....mp3",
			"Unknown Artist", "Unknown Album", "Untitled.mp3"),
		Entry("control characters go", FileTags{AlbumArtist: "A\tB\x00", Album: "B\nC", Title: "C"}, "x.mp3",
			"AB", "BC", "C.mp3"),
		Entry("reserved device names", FileTags{AlbumArtist: "CON", Album: "aux", Title: "nul"}, "x.mp3",
			"CON_", "aux_", "nul_.mp3"),
		Entry("decomposed accents are composed (NFC)", FileTags{AlbumArtist: "Beyoncé", Album: "B", Title: "Café"}, "x.mp3",
			"Beyoncé", "B", "Café.mp3"),
	)

	It("caps long names, keeping the year, the track number and the extension, and valid UTF-8", func() {
		long := strings.Repeat("é", 200) // 400 bytes
		got := promoteLayout(FileTags{AlbumArtist: long, Album: long, Year: "2001", Title: long, TrackNumber: "7"}, "x.flac")
		Expect(len(got.Artist)).To(BeNumerically("<=", maxNameBytes))
		Expect(got.Album).To(HaveSuffix("é (2001)"))
		Expect(len(got.Album)).To(BeNumerically("<=", maxNameBytes+len(" (2001)")))
		Expect(got.File).To(HavePrefix("07 é"))
		Expect(got.File).To(HaveSuffix("é.flac"))
		Expect(len(got.File)).To(BeNumerically("<=", maxNameBytes+len("07 .flac")))
		for _, s := range []string{got.Artist, got.Album, got.File} {
			Expect(strings.ToValidUTF8(s, "?")).To(Equal(s))
		}
	})
})

var _ = Describe("promoteDest", func() {
	var (
		root string
		fsys caseInsensitiveFS
	)

	BeforeEach(func() {
		root = GinkgoT().TempDir()
		fsys = caseInsensitiveFS{root: root}
	})

	mkdir := func(rel string) {
		Expect(os.MkdirAll(filepath.Join(root, rel), 0o755)).To(Succeed())
	}
	touch := func(rel string) {
		Expect(os.WriteFile(filepath.Join(root, rel), []byte("x"), 0o644)).To(Succeed())
	}
	names := promoteNames{Artist: "The Beatles", Album: "Abbey Road (1969)", File: "01 Come Together.mp3"}

	It("makes the folders that aren't there yet", func() {
		dest, err := promoteDest(fsys, root, names)
		Expect(err).ToNot(HaveOccurred())
		Expect(dest).To(Equal(filepath.Join(root, "The Beatles", "Abbey Road (1969)", "01 Come Together.mp3")))
		Expect(filepath.Join(root, "The Beatles", "Abbey Road (1969)")).To(BeADirectory())
		Expect(dest).ToNot(BeAnExistingFile())
	})

	It("reuses folders whose names differ only by letter case", func() {
		mkdir("THE BEATLES/abbey road (1969)")
		dest, err := promoteDest(fsys, root, names)
		Expect(err).ToNot(HaveOccurred())
		Expect(dest).To(Equal(filepath.Join(root, "THE BEATLES", "abbey road (1969)", "01 Come Together.mp3")))
		entries, _ := os.ReadDir(root)
		Expect(entries).To(HaveLen(1))
	})

	It("reuses a folder whose name is the same once both are composed (NFC)", func() {
		mkdir("Beyoncé")
		dest, err := promoteDest(fsys, root, promoteNames{Artist: "BEYONCÉ", Album: "B", File: "C.mp3"})
		Expect(err).ToNot(HaveOccurred())
		Expect(dest).To(Equal(filepath.Join(root, "Beyoncé", "B", "C.mp3")))
	})

	It("never takes a File's name, even in other letter case: it adds (2), (3)...", func() {
		mkdir("The Beatles/Abbey Road (1969)")
		touch("The Beatles/Abbey Road (1969)/01 come together.MP3")
		touch("The Beatles/Abbey Road (1969)/01 Come Together (2).mp3")
		dest, err := promoteDest(fsys, root, names)
		Expect(err).ToNot(HaveOccurred())
		Expect(filepath.Base(dest)).To(Equal("01 Come Together (3).mp3"))
	})

	It("doesn't put a folder where a File of that name is", func() {
		touch("the beatles")
		_, err := promoteDest(fsys, root, names)
		Expect(err).To(MatchError(ContainSubstring("not a folder")))
	})

	It("refuses a folder that is a symlink leading out of the promote folder", func() {
		outside := GinkgoT().TempDir()
		Expect(os.Symlink(outside, filepath.Join(root, "The Beatles"))).To(Succeed())
		_, err := promoteDest(fsys, root, names)
		Expect(err).To(MatchError(ErrOutsidePromoteFolder))
		Expect(filepath.Join(outside, "Abbey Road (1969)")).ToNot(BeADirectory())
	})
})
