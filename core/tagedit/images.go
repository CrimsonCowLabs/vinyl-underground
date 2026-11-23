package tagedit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"github.com/navidrome/navidrome/conf"
)

// journalImages keeps the images cover writes and embeds replaced (and wrote), by their SHA-256,
// so an undo puts one back byte for byte: in the data folder's edit-journal/images, beside the
// database, not in it. They are never removed, like the journal's entries. A cover can be several
// MB and an embed job replaces one per File, so keeping them out of SQLite keeps the database (and
// its backups, which are whole copies) small, and the Edit history's reads of the journal cheap;
// keyed by content, an image replaced in many Files, or replaced again and again, is kept once.
type journalImages struct {
	// dir, when set, is the folder (tests); otherwise it is under conf.Server.DataFolder.
	dir string
}

var sumPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (s journalImages) folder() string {
	if s.dir != "" {
		return s.dir
	}
	return filepath.Join(conf.Server.DataFolder.String(), "edit-journal", "images")
}

func (s journalImages) path(sum string) string { return filepath.Join(s.folder(), sum) }

// hexSum is the SHA-256 of b, in hex, as the journal records it.
func hexSum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// put keeps the image, flushed to disk and checked, and returns its SHA-256 (hex). An image kept
// already is left as it is.
func (s journalImages) put(data []byte) (string, error) {
	sum := hexSum(data)
	if got, err := s.read(sum); err == nil && bytes.Equal(got, data) {
		return sum, nil
	}
	if err := os.MkdirAll(s.folder(), 0o700); err != nil {
		return "", fmt.Errorf("keeping the replaced image: %w", err)
	}
	f, err := os.CreateTemp(s.folder(), ".tmp-*")
	if err != nil {
		return "", fmt.Errorf("keeping the replaced image: %w", err)
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), s.path(sum))
	}
	if err != nil {
		return "", fmt.Errorf("keeping the replaced image: %w", err)
	}
	if _, err := s.read(sum); err != nil {
		return "", fmt.Errorf("keeping the replaced image: %w", err)
	}
	return sum, nil
}

// read returns the image kept under sum, checked against it.
func (s journalImages) read(sum string) ([]byte, error) {
	if !sumPattern.MatchString(sum) {
		return nil, fmt.Errorf("no image is kept as %q", sum)
	}
	data, err := os.ReadFile(s.path(sum))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("the kept image %s is gone from %s: %w", sum, s.folder(), err)
	}
	if err != nil {
		return nil, err
	}
	if hexSum(data) != sum {
		return nil, fmt.Errorf("the kept image %s doesn't match its checksum", sum)
	}
	return data, nil
}
