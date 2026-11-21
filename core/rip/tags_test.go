package rip

import (
	"encoding/json"
	"os/exec"
	"path/filepath"

	"go.senan.xyz/taglib"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// makeAudio encodes one second of tone with real ffmpeg, standing in for a Rip.
func makeAudio(name string, codecArgs ...string) string {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		Skip("ffmpeg is not installed")
	}
	path := filepath.Join(GinkgoT().TempDir(), name)
	args := append([]string{"-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=1"}, codecArgs...)
	out, err := exec.Command("ffmpeg", append(args, path)...).CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), string(out))
	return path
}

// probeTags reads the File's tags with ffprobe, independently of the tag writer.
func probeTags(path string) map[string]string {
	out, err := exec.Command("ffprobe", "-v", "error", "-print_format", "json",
		"-show_entries", "format_tags:stream_tags", path).Output()
	Expect(err).ToNot(HaveOccurred())
	var res struct {
		Format  struct{ Tags map[string]string }   `json:"format"`
		Streams []struct{ Tags map[string]string } `json:"streams"`
	}
	Expect(json.Unmarshal(out, &res)).To(Succeed())
	tags := map[string]string{}
	for _, s := range res.Streams {
		for k, v := range s.Tags {
			tags[k] = v
		}
	}
	for k, v := range res.Format.Tags {
		tags[k] = v
	}
	return tags
}

var _ = Describe("WriteTags", func() {
	info := &Info{
		Title: "Song Title", Artist: "Some Artist",
		Source: "YouTube", SourceURL: "https://www.youtube.com/watch?v=abc",
	}

	It("writes SOURCE and SOURCEURL Vorbis comments into an Opus File", func() {
		path := makeAudio("rip.opus", "-c:a", "libopus")

		Expect(WriteTags(path, info)).To(Succeed())

		tags := probeTags(path)
		Expect(tags).To(HaveKeyWithValue("SOURCE", "YouTube"))
		Expect(tags).To(HaveKeyWithValue("SOURCEURL", "https://www.youtube.com/watch?v=abc"))
		Expect(tags).To(HaveKeyWithValue("TITLE", "Song Title"))
		Expect(tags).To(HaveKeyWithValue("ARTIST", "Some Artist"))
	})

	It("writes SOURCE and SOURCEURL as iTunes freeform atoms into an M4A File", func() {
		path := makeAudio("rip.m4a", "-c:a", "aac", "-b:a", "64k")

		Expect(WriteTags(path, info)).To(Succeed())

		atoms, err := taglib.ReadMP4Atoms(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(atoms).To(HaveKeyWithValue("----:com.apple.iTunes:SOURCE", []string{"YouTube"}))
		Expect(atoms).To(HaveKeyWithValue("----:com.apple.iTunes:SOURCEURL", []string{"https://www.youtube.com/watch?v=abc"}))
		Expect(atoms).To(HaveKeyWithValue("©nam", []string{"Song Title"}))
		Expect(atoms).To(HaveKeyWithValue("©ART", []string{"Some Artist"}))

		tags := probeTags(path)
		Expect(tags).To(HaveKeyWithValue("title", "Song Title"))
	})

	It("leaves out tags it has no value for", func() {
		path := makeAudio("rip.opus", "-c:a", "libopus")

		Expect(WriteTags(path, &Info{Source: "YouTube", SourceURL: "https://www.youtube.com/watch?v=abc"})).To(Succeed())

		tags := probeTags(path)
		Expect(tags).ToNot(HaveKey("ARTIST"))
		Expect(tags).To(HaveKeyWithValue("SOURCE", "YouTube"))
	})
})
