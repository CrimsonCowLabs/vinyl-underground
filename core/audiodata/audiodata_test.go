package audiodata

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// These are the cases of contrib/library-cleanup/test_audio.py, which this package ports.

// Two MPEG-1 Layer III frame headers (128 kbps, 44.1 kHz) with a little payload each.
var (
	frames      = cat([]byte("\xff\xfb\x90\x64"), rep(0x11, 60), []byte("\xff\xfb\x90\x64"), rep(0x22, 60))
	otherFrames = cat([]byte("\xff\xfb\x90\x64"), rep(0x33, 60), []byte("\xff\xfb\x90\x64"), rep(0x44, 60))
)

func cat(parts ...[]byte) []byte { return bytes.Join(parts, nil) }
func rep(b byte, n int) []byte   { return bytes.Repeat([]byte{b}, n) }

func be32(n int) []byte { return binary.BigEndian.AppendUint32(nil, uint32(n)) }
func le32(n int) []byte { return binary.LittleEndian.AppendUint32(nil, uint32(n)) }

func toSyncsafe(n int) []byte {
	return []byte{byte(n>>21) & 0x7f, byte(n>>14) & 0x7f, byte(n>>7) & 0x7f, byte(n) & 0x7f}
}

type id3opts struct {
	padding int
	version byte
	footer  bool
}

func id3v2(title string, o id3opts) []byte {
	if o.version == 0 {
		o.version = 3
	}
	text := cat([]byte{0}, []byte(title))
	frame := cat([]byte("TIT2"), be32(len(text)), []byte{0, 0}, text)
	body := cat(frame, rep(0, o.padding))
	var flags byte
	if o.footer {
		flags = 0x10
	}
	tag := cat([]byte("ID3"), []byte{o.version, 0, flags}, toSyncsafe(len(body)), body)
	if o.footer {
		tag = cat(tag, []byte("3DI"), []byte{o.version, 0, flags}, toSyncsafe(len(body)))
	}
	return tag
}

func id3v1(title string) []byte {
	t := []byte(title)
	t = append(t, rep(0, 30-len(t))...)
	return cat([]byte("TAG"), t, rep(0, 94), []byte{0x0c})
}

func apev2(value string, header bool) []byte {
	item := cat(le32(len(value)), le32(0), []byte("Title\x00"), []byte(value))
	size := len(item) + 32
	block := func(isHeader bool) []byte {
		var flags uint32
		if header {
			flags |= 0x80000000
		}
		if isHeader {
			flags |= 0x20000000
		}
		return cat([]byte("APETAGEX"), le32(2000), le32(size), le32(1), binary.LittleEndian.AppendUint32(nil, flags), rep(0, 8))
	}
	if header {
		return cat(block(true), item, block(false))
	}
	return cat(item, block(false))
}

func data(b []byte, ext string) []byte {
	GinkgoHelper()
	got, err := Data(bytes.NewReader(b), ext)
	Expect(err).ToNot(HaveOccurred())
	return got
}

var _ = Describe("Data", func() {
	Describe("mp3", func() {
		It("finds the same audio in Files that differ only in their ID3 tags", func() {
			original := cat(id3v2("Car Alarm", id3opts{}), frames, id3v1("Car Alarm"))
			copy := cat(id3v2("Car Alarm (copy)", id3opts{padding: 300}), frames, id3v1("Car Alarm 1"))
			Expect(data(original, ".mp3")).To(Equal(frames))
			Expect(data(copy, "mp3")).To(Equal(frames))
		})

		It("leaves out APE tags before or after an ID3v1 tag", func() {
			// Seen in the Library: ID3v2, audio, APEv2 (from a gain tool), then ID3v1.
			withHeader := cat(id3v2("A", id3opts{}), frames, apev2("Song", true), id3v1("Song"))
			footerOnly := cat(frames, apev2("Song 1", false))
			twoTags := cat(frames, apev2("x", true), id3v1("Song"), apev2("y", true))
			for _, f := range [][]byte{withHeader, footerOnly, twoTags} {
				Expect(data(f, "mp3")).To(Equal(frames))
			}
		})

		It("leaves out chained and appended ID3v2 tags, and Lyrics3", func() {
			// Seen in the Library: an ID3v2.3 tag followed by an ID3v2.4 tag.
			chained := cat(id3v2("A", id3opts{version: 3}), id3v2("A", id3opts{version: 4, padding: 50}), frames)
			appended := cat(frames, id3v2("A", id3opts{version: 4, footer: true}))
			lyrics := []byte("LYRICSBEGIN" + "IND00002" + "11" + "000021" + "LYRICS200")
			withLyrics := cat(frames, lyrics, id3v1("A"))
			for _, f := range [][]byte{chained, appended, withLyrics} {
				Expect(data(f, "mp3")).To(Equal(frames))
			}
		})

		It("has no audio to compare in a File that is all tag", func() {
			// e.g. a truncated download whose ID3v2 tag claims more bytes than the File holds.
			tag := id3v2("A", id3opts{padding: 10})
			Expect(data(tag[:len(tag)-5], "mp3")).To(BeNil())
			Expect(data(cat(id3v2("A", id3opts{}), id3v1("A")), "mp3")).To(BeNil())
		})

		It("keeps audio that only looks like the end of a tag", func() {
			// Frame data can end in "3DI", "APETAGEX" or "LYRICS200" by chance; with no tag
			// header where the size points, those bytes are audio and are compared.
			for _, fake := range [][]byte{
				cat([]byte("3DI\x04\x00\x10"), toSyncsafe(40)),
				cat([]byte("APETAGEX"), le32(2000), le32(60), le32(1), le32(0x80000000), rep(0, 8)),
				[]byte("000030LYRICS200"),
			} {
				d := cat(frames, fake)
				Expect(data(d, "mp3")).To(Equal(d))
			}
		})

		It("finds different audio in different frames", func() {
			Expect(data(cat(id3v2("A", id3opts{}), frames), "mp3")).ToNot(Equal(data(cat(id3v2("A", id3opts{}), otherFrames), "mp3")))
		})
	})

	Describe("m4a", func() {
		atom := func(kind string, payload []byte, wide bool) []byte {
			if wide {
				return cat(be32(1), []byte(kind), binary.BigEndian.AppendUint64(nil, uint64(len(payload)+16)), payload)
			}
			return cat(be32(len(payload)+8), []byte(kind), payload)
		}
		samples := bytes.Repeat([]byte("\x21\x10\x05"), 40)
		m4a := func(title string, samples []byte, mdatFirst, wide bool) []byte {
			ftyp := atom("ftyp", []byte("M4A \x00\x00\x00\x00M4A mp42isom"), false)
			moov := atom("moov", atom("udta", atom("meta", cat(rep(0, 4), atom("\xa9nam", []byte(title), false)), false), false), false)
			mdat := atom("mdat", samples, wide)
			body := cat(moov, atom("free", rep(0, 16), false), mdat)
			if mdatFirst {
				body = cat(mdat, moov)
			}
			return cat(ftyp, body)
		}

		It("finds the audio in the media data, whatever the metadata around it", func() {
			for _, f := range [][]byte{m4a("Song", samples, false, false), m4a("Song 1", samples, true, false), m4a("Song (copy)", samples, false, true)} {
				Expect(data(f, "m4a")).To(Equal(samples))
			}
		})

		It("finds different audio in different media data", func() {
			Expect(data(m4a("Song", samples, false, false), "m4a")).ToNot(Equal(data(m4a("Song", rep(0, 120), false, false), "m4a")))
		})

		It("has no audio to compare in a File without media data", func() {
			Expect(data(cat(atom("ftyp", []byte("M4A "), false), atom("moov", nil, false)), "m4a")).To(BeNil())
			Expect(data([]byte("not an mp4 file at all"), "m4a")).To(BeNil())
			Expect(data(m4a("Song", nil, false, false), "m4a")).To(BeNil())
		})
	})

	Describe("aiff and wav", func() {
		chunk := func(kind string, payload []byte, le bool) []byte {
			size := be32(len(payload))
			if le {
				size = le32(len(payload))
			}
			var pad []byte
			if len(payload)%2 == 1 {
				pad = []byte{0}
			}
			return cat([]byte(kind), size, payload, pad)
		}
		comm := cat([]byte("\x00\x02"), []byte("\x00\x00\x00\x28"), []byte("\x00\x10"), []byte("\x40\x0e\xac\x44"), rep(0, 6))
		pcm := bytes.Repeat([]byte("\x01\x02\x03\x04"), 20)
		aiff := func(title string, comm []byte, tagsAfter bool) []byte {
			tags := cat(chunk("NAME", []byte(title), false), chunk("ID3 ", id3v2(title, id3opts{}), false))
			sound := cat(chunk("COMM", comm, false), chunk("SSND", pcm, false))
			body := cat([]byte("AIFF"), tags, sound)
			if tagsAfter {
				body = cat([]byte("AIFF"), sound, tags)
			}
			return cat([]byte("FORM"), be32(len(body)), body)
		}
		stereo := []byte("\x01\x00\x02\x00\x44\xac\x00\x00\x10\xb1\x02\x00\x04\x00\x10\x00")
		wav := func(title string, fmtChunk []byte) []byte {
			info := chunk("LIST", cat([]byte("INFO"), chunk("INAM", cat([]byte(title), []byte{0}), true)), true)
			body := cat([]byte("WAVE"), chunk("fmt ", fmtChunk, true), info, chunk("data", pcm, true))
			return cat([]byte("RIFF"), le32(len(body)), body)
		}

		It("finds the same audio in Files that differ only in their tag chunks", func() {
			Expect(data(aiff("Song", comm, false), "aif")).To(Equal(data(aiff("Song 1 (odd)", comm, true), "aiff")))
			Expect(data(wav("Song", stereo), "wav")).To(Equal(data(wav("Song 1", stereo), "wav")))
			Expect(data(aiff("Song", comm, false), "aif")).To(ContainSubstring(string(pcm)))
		})

		It("finds different audio in the same samples in a different format", func() {
			mono := cat([]byte("\x00\x01"), comm[2:])
			Expect(data(aiff("Song", comm, false), "aif")).ToNot(Equal(data(aiff("Song", mono, false), "aif")))
			Expect(data(wav("Song", stereo), "wav")).ToNot(Equal(data(wav("Song", cat([]byte("\x01\x00\x01\x00"), rep(0, 12))), "wav")))
		})
	})

	It("has no audio to compare in a format it can't parse", func() {
		Expect(data(cat([]byte("OggS"), rep(0, 60)), "ogg")).To(BeNil())
	})
})

var _ = Describe("Same", func() {
	write := func(dir, name string, b []byte) string {
		p := filepath.Join(dir, name)
		Expect(os.WriteFile(p, b, 0o600)).To(Succeed())
		return p
	}

	It("is true for Files whose audio is identical, whatever their tags", func() {
		dir := GinkgoT().TempDir()
		a := write(dir, "a.mp3", cat(id3v2("A", id3opts{}), frames, id3v1("A")))
		b := write(dir, "b.mp3", cat(id3v2("B, retagged", id3opts{padding: 100, version: 4}), frames))
		Expect(Same(a, b)).To(BeTrue())
	})

	It("is false for different audio", func() {
		dir := GinkgoT().TempDir()
		a := write(dir, "a.mp3", cat(id3v2("A", id3opts{}), frames))
		b := write(dir, "b.mp3", cat(id3v2("A", id3opts{}), otherFrames))
		Expect(Same(a, b)).To(BeFalse())
	})

	It("fails for a format it can't parse, rather than saying the audio is the same", func() {
		dir := GinkgoT().TempDir()
		a := write(dir, "a.ogg", []byte("OggS"+strings.Repeat("\x00", 60)))
		_, err := Same(a, a)
		Expect(err).To(MatchError(ErrUnsupported))
	})
})
