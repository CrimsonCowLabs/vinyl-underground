package tagedit

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.senan.xyz/taglib"
)

// Tagger reads and writes a File's tags as TagLib properties.
type Tagger interface {
	Read(path string) (map[string][]string, error)
	// Write sets the given properties in the File at path (no values removes one), and leaves
	// every other tag as it is.
	Write(path string, set map[string][]string) error
}

// taglibTagger is TagLib. It saves mp3s as ID3v2.4, and keeps a tag it doesn't map: TagLib
// writes the File's whole property map back, and leaves frames and atoms with no property
// (pictures, private frames, iTunes atoms) alone.
type taglibTagger struct{}

func (taglibTagger) Read(path string) (map[string][]string, error) {
	tags, err := taglib.ReadTags(path)
	if err != nil {
		return nil, fmt.Errorf("reading the tags: %w", err)
	}
	if tags == nil {
		tags = map[string][]string{}
	}
	return tags, nil
}

func (taglibTagger) Write(path string, set map[string][]string) error {
	mp3 := strings.EqualFold(filepath.Ext(path), ".mp3")
	hadV1 := false
	if mp3 {
		var err error
		if hadV1, err = hasID3v1(path); err != nil {
			return err
		}
	}
	if err := taglib.WriteTags(path, set, 0); err != nil {
		return fmt.Errorf("writing the tags: %w", err)
	}
	if mp3 && !hadV1 {
		// TagLib copies the ID3v2 tag into a new ID3v1 one when it saves an mp3; an ID3v1 tag is
		// only updated where there is one already.
		return stripID3v1(path)
	}
	return nil
}

const id3v1Size = 128

// hasID3v1 reports whether the File at path ends in an ID3v1 tag.
func hasID3v1(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	if info.Size() < id3v1Size {
		return false, nil
	}
	head := make([]byte, 3)
	if _, err := f.ReadAt(head, info.Size()-id3v1Size); err != nil && err != io.EOF {
		return false, err
	}
	return string(head) == "TAG", nil
}

func stripID3v1(path string) error {
	has, err := hasID3v1(path)
	if err != nil || !has {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if err := os.Truncate(path, info.Size()-id3v1Size); err != nil {
		return fmt.Errorf("removing the ID3v1 tag TagLib added: %w", err)
	}
	return nil
}

// readHead reads the first n bytes of the File at path.
func readHead(path string, n int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b := make([]byte, n)
	if _, err := io.ReadFull(f, b); err != nil {
		return nil, err
	}
	return b, nil
}
