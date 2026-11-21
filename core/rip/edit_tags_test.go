package rip

import (
	"errors"
	"os"
	"path/filepath"

	"go.senan.xyz/taglib"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// ripFixture copies one of the hand-tagged Rip fixtures (tests/fixtures/rips: title, artist,
// album "Rip Album", SOURCE and SOURCEURL) into a temp folder.
func ripFixture(name string) string {
	data, err := os.ReadFile(filepath.Join("tests", "fixtures", "rips", name))
	Expect(err).ToNot(HaveOccurred())
	path := filepath.Join(GinkgoT().TempDir(), name)
	Expect(os.WriteFile(path, data, 0o640)).To(Succeed())
	return path
}

func readTags(path string) map[string][]string {
	tags, err := taglib.ReadTags(path)
	Expect(err).ToNot(HaveOccurred())
	return tags
}

func str(s string) *string { return &s }

var _ = Describe("editTags", func() {
	const youTubeURL = "https://www.youtube.com/watch?v=dQw4w9WgXcQ"

	DescribeTable("writes the edited tags and keeps every other tag, SOURCE and SOURCEURL included",
		func(name string) {
			path := ripFixture(name)

			changed, err := editTags(path, TagEdit{
				Title: str("New Title"), Artist: str("New Artist"),
				Album: str("New Album"), AlbumArtist: str("Album Band"),
				Year: str("1999"), TrackNumber: str("3"),
			}, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(changed).To(BeTrue())

			tags := readTags(path)
			Expect(tags).To(HaveKeyWithValue("TITLE", []string{"New Title"}))
			Expect(tags).To(HaveKeyWithValue("ARTIST", []string{"New Artist"}))
			Expect(tags).To(HaveKeyWithValue("ALBUM", []string{"New Album"}))
			Expect(tags).To(HaveKeyWithValue("ALBUMARTIST", []string{"Album Band"}))
			Expect(tags).To(HaveKeyWithValue("DATE", []string{"1999"}))
			Expect(tags).To(HaveKeyWithValue("TRACKNUMBER", []string{"3"}))
			Expect(tags).To(HaveKeyWithValue("SOURCE", []string{"YouTube"}))
			Expect(tags).To(HaveKeyWithValue("SOURCEURL", []string{youTubeURL}))

			// And ffprobe, which doesn't use TagLib, still reads the File and its new title.
			Expect(probeTags(path)).To(HaveKeyWithValue(titleKey(name), "New Title"))
		},
		Entry("opus", "ripped.opus"),
		Entry("m4a", "ripped.m4a"),
		Entry("mp3", "ripped.mp3"),
	)

	It("leaves tags it isn't given alone", func() {
		path := ripFixture("ripped.opus")

		_, err := editTags(path, TagEdit{Title: str("Only The Title")}, nil)
		Expect(err).ToNot(HaveOccurred())

		tags := readTags(path)
		Expect(tags).To(HaveKeyWithValue("TITLE", []string{"Only The Title"}))
		Expect(tags).To(HaveKeyWithValue("ARTIST", []string{"Rip Artist"}))
		Expect(tags).To(HaveKeyWithValue("ALBUM", []string{"Rip Album"}))
		Expect(tags).To(HaveKeyWithValue("SOURCE", []string{"YouTube"}))
	})

	It("removes a tag edited to nothing", func() {
		path := ripFixture("ripped.mp3")

		_, err := editTags(path, TagEdit{Album: str("")}, nil)
		Expect(err).ToNot(HaveOccurred())

		Expect(readTags(path)).ToNot(HaveKey("ALBUM"))
		Expect(readTags(path)).To(HaveKeyWithValue("SOURCEURL", []string{youTubeURL}))
	})

	It("doesn't rewrite the File when nothing changed", func() {
		path := ripFixture("ripped.m4a")
		before, err := os.Stat(path)
		Expect(err).ToNot(HaveOccurred())

		changed, err := editTags(path, TagEdit{Title: str("Ripped M4A"), Album: str("Rip Album")},
			func(string, map[string][]string) error {
				Fail("nothing should be written")
				return nil
			})
		Expect(err).ToNot(HaveOccurred())
		Expect(changed).To(BeFalse())
		after, _ := os.Stat(path)
		Expect(os.SameFile(before, after)).To(BeTrue())
	})

	It("keeps a track total and a full date the edit didn't change", func() {
		path := ripFixture("ripped.opus")
		Expect(taglib.WriteTags(path, map[string][]string{"TRACKNUMBER": {"2/12"}, "DATE": {"2001-05-03"}}, 0)).To(Succeed())

		_, err := editTags(path, TagEdit{TrackNumber: str("4"), Year: str("2001")}, nil)
		Expect(err).ToNot(HaveOccurred())

		tags := readTags(path)
		Expect(tags).To(HaveKeyWithValue("TRACKNUMBER", []string{"4/12"}))
		Expect(tags).To(HaveKeyWithValue("DATE", []string{"2001-05-03"}))
	})

	It("leaves the File as it was, and no temp File behind, when the write fails", func() {
		path := ripFixture("ripped.opus")
		original, err := os.ReadFile(path)
		Expect(err).ToNot(HaveOccurred())

		_, err = editTags(path, TagEdit{Title: str("Never Saved")}, func(tmp string, _ map[string][]string) error {
			// Half-written: the copy is ruined before the writer gives up.
			Expect(os.WriteFile(tmp, []byte("garbage"), 0o600)).To(Succeed())
			return errors.New("disk full")
		})
		Expect(err).To(MatchError(ContainSubstring("disk full")))

		Expect(os.ReadFile(path)).To(Equal(original))
		entries, _ := os.ReadDir(filepath.Dir(path))
		Expect(entries).To(HaveLen(1))
	})

	It("refuses a value that would split into several tags", func() {
		path := ripFixture("ripped.opus")

		_, err := editTags(path, TagEdit{Title: str("A\vB")}, nil)
		Expect(err).To(MatchError(ErrInvalidTags))
		_, err = editTags(path, TagEdit{Year: str("next year")}, nil)
		Expect(err).To(MatchError(ErrInvalidTags))
		_, err = editTags(path, TagEdit{TrackNumber: str("1/2")}, nil)
		Expect(err).To(MatchError(ErrInvalidTags))
	})
})

// titleKey is how ffprobe names the title tag: Vorbis comments keep their case.
func titleKey(name string) string {
	if filepath.Ext(name) == ".opus" {
		return "TITLE"
	}
	return "title"
}
