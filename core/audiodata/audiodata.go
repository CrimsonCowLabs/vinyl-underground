// Package audiodata finds the audio in a File, leaving out its tags and embedded art, so two
// Files hold the same audio when those bytes are identical, whatever their tags. It is a port of
// contrib/library-cleanup/audio.py, and reads only the tag headers until asked for the audio
// itself, so it is cheap over a slow share.
package audiodata

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrUnsupported is returned for a File whose audio can't be found: a format this package
// doesn't parse, or a File that can't be parsed (e.g. one that is all tag).
var ErrUnsupported = errors.New("can't find the audio data in this File")

// span is a byte range [start, end) of a File.
type span struct{ start, end int64 }

// Ranges returns the byte ranges holding a File's audio, or nil if the format (by its
// extension, with or without the dot) isn't supported or the File can't be parsed.
func ranges(r io.ReadSeeker, ext string) ([]span, error) {
	size, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	switch strings.TrimPrefix(strings.ToLower(ext), ".") {
	case "mp3":
		s, err := mp3Range(r, size)
		if err != nil || s.start >= s.end {
			return nil, err
		}
		return []span{s}, nil
	case "m4a", "mp4", "m4b":
		rs, err := mp4Ranges(r, size)
		if err != nil || total(rs) == 0 {
			return nil, err
		}
		return rs, nil
	case "aif", "aiff", "aifc", "wav":
		return iffRanges(r, size)
	}
	return nil, nil
}

func total(rs []span) int64 {
	var n int64
	for _, s := range rs {
		n += s.end - s.start
	}
	return n
}

// readAt reads n bytes at pos, or fewer at the end of the File.
func readAt(r io.ReadSeeker, pos int64, n int) ([]byte, error) {
	if _, err := r.Seek(pos, io.SeekStart); err != nil {
		return nil, err
	}
	b := make([]byte, n)
	m, err := io.ReadFull(r, b)
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		err = nil
	}
	return b[:m], err
}

func syncsafe(b []byte) int64 {
	return int64(b[0])<<21 | int64(b[1])<<14 | int64(b[2])<<7 | int64(b[3])
}

func mp3Range(r io.ReadSeeker, size int64) (span, error) {
	var start int64
	for {
		head, err := readAt(r, start, 10)
		if err != nil {
			return span{}, err
		}
		if len(head) < 10 || string(head[:3]) != "ID3" {
			break
		}
		start += 10 + syncsafe(head[6:10])
		if head[5]&0x10 != 0 {
			start += 10
		}
	}
	end := size
	for {
		tag, err := trailingTag(r, start, end)
		if err != nil {
			return span{}, err
		}
		if tag == 0 || end-tag < start {
			return span{start, end}, nil
		}
		end -= tag
	}
}

func at(r io.ReadSeeker, pos int64, magic string) bool {
	if pos < 0 {
		return false
	}
	b, err := readAt(r, pos, len(magic))
	return err == nil && string(b) == magic
}

// trailingTag returns the size of the tag that ends at end, or 0 if none does. A tag is only
// recognised when its header sits where its footer says, so audio that happens to end in a
// tag's marker is still compared.
func trailingTag(r io.ReadSeeker, start, end int64) (int64, error) {
	if end-start < 32 {
		return 0, nil
	}
	from := max(start, end-128)
	tail, err := readAt(r, from, int(end-from))
	if err != nil {
		return 0, err
	}
	n := len(tail)
	if n == 128 && string(tail[:3]) == "TAG" { // ID3v1
		return 128, nil
	}
	le := binary.LittleEndian
	if string(tail[n-32:n-24]) == "APETAGEX" { // APEv2 footer; its size counts the items and footer, not the header
		size := int64(le.Uint32(tail[n-20 : n-16]))
		hasHeader := le.Uint32(tail[n-12:n-8])&0x80000000 != 0
		tag := size
		if hasHeader {
			tag += 32
		}
		version := le.Uint32(tail[n-24 : n-20])
		if (version == 1000 || version == 2000) && size >= 32 && (!hasHeader || at(r, end-tag, "APETAGEX")) {
			return tag, nil
		}
	}
	if n >= 15 && string(tail[n-9:]) == "LYRICS200" { // Lyrics3v2: a 6-digit size, then the marker
		if d, err := strconv.Atoi(string(tail[n-15 : n-9])); err == nil && d >= 0 && isDigits(tail[n-15:n-9]) {
			tag := int64(d) + 15
			if at(r, end-tag, "LYRICSBEGIN") {
				return tag, nil
			}
		}
	}
	// An ID3v2 tag appended at the end, with its footer.
	if string(tail[n-10:n-7]) == "3DI" && tail[n-4] < 0x80 && tail[n-3] < 0x80 && tail[n-2] < 0x80 && tail[n-1] < 0x80 {
		tag := 20 + syncsafe(tail[n-4:])
		if at(r, end-tag, "ID3"+string(tail[n-7:n-6])) {
			return tag, nil
		}
	}
	return 0, nil
}

func isDigits(b []byte) bool {
	for _, c := range b {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// mp4Ranges returns the top-level "mdat" atoms: the samples live there, while tags and art live
// in "moov", which also moves when a tagger rewrites it, so only the media data is compared.
func mp4Ranges(r io.ReadSeeker, size int64) ([]span, error) {
	var rs []span
	var pos int64
	for pos+8 <= size {
		head, err := readAt(r, pos, 16)
		if err != nil {
			return nil, err
		}
		length, kind := int64(binary.BigEndian.Uint32(head[:4])), string(head[4:8])
		header := int64(8)
		if length == 1 && len(head) == 16 {
			length, header = int64(binary.BigEndian.Uint64(head[8:16])), 16
		} else if length == 0 {
			length = size - pos
		}
		if length < header || pos+length > size {
			return nil, nil
		}
		if kind == "mdat" {
			rs = append(rs, span{pos + header, pos + length})
		}
		pos += length
	}
	return rs, nil
}

// iffChunks are, by a chunked format's magic, its byte order and the chunks that make up the
// sound (its format and its samples). Every other chunk (NAME, ID3, LIST, ...) is metadata.
var iffChunks = map[string]struct {
	order binary.ByteOrder
	sound []string
}{
	"FORM": {binary.BigEndian, []string{"COMM", "SSND"}},
	"RIFF": {binary.LittleEndian, []string{"fmt ", "data"}},
}

func iffRanges(r io.ReadSeeker, size int64) ([]span, error) {
	magic, err := readAt(r, 0, 4)
	if err != nil {
		return nil, err
	}
	spec, ok := iffChunks[string(magic)]
	if !ok {
		return nil, nil
	}
	var rs []span
	pos := int64(12)
	for pos+8 <= size {
		head, err := readAt(r, pos, 8)
		if err != nil {
			return nil, err
		}
		end := pos + 8 + int64(spec.order.Uint32(head[4:8]))
		if end > size {
			return nil, nil
		}
		for _, s := range spec.sound {
			if string(head[:4]) == s {
				rs = append(rs, span{pos, end}) // with its header, so a chunk's kind and length count too
			}
		}
		pos = end + (end-pos)%2 // chunks are padded to an even length
	}
	return rs, nil
}

// copyAudio writes the bytes in rs to w.
func copyAudio(w io.Writer, r io.ReadSeeker, rs []span) error {
	for _, s := range rs {
		if _, err := r.Seek(s.start, io.SeekStart); err != nil {
			return err
		}
		n, err := io.CopyN(w, r, s.end-s.start)
		if err != nil {
			return fmt.Errorf("the File ended early, at byte %d: %w", s.start+n, err)
		}
	}
	return nil
}

// Data returns the bytes of a File's audio, or nil if its format (by ext) isn't supported or it
// can't be parsed.
func Data(r io.ReadSeeker, ext string) ([]byte, error) {
	rs, err := ranges(r, ext)
	if err != nil || rs == nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := copyAudio(&buf, r, rs); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Digest returns the size and SHA-256 of the audio of the File at path. It is ErrUnsupported
// when the audio can't be found.
func Digest(path string) (int64, []byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, nil, err
	}
	defer f.Close()
	rs, err := ranges(f, filepath.Ext(path))
	if err != nil {
		return 0, nil, err
	}
	if rs == nil {
		return 0, nil, fmt.Errorf("%w: %s", ErrUnsupported, filepath.Base(path))
	}
	h := sha256.New()
	if err := copyAudio(h, f, rs); err != nil {
		return 0, nil, err
	}
	return total(rs), h.Sum(nil), nil
}

// Same reports whether the Files at a and b hold identical audio, whatever their tags. It is
// ErrUnsupported, never false, when the audio of either can't be found.
func Same(a, b string) (bool, error) {
	na, ha, err := Digest(a)
	if err != nil {
		return false, err
	}
	nb, hb, err := Digest(b)
	if err != nil {
		return false, err
	}
	return na == nb && bytes.Equal(ha, hb), nil
}
