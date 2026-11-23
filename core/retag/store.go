package retag

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/navidrome/navidrome/log"
)

// DirStore keeps MusicBrainz's and AcoustID's answers as files in a folder, for good, so a run
// stopped part-way, or run again, doesn't ask again (musicbrainz.Store).
type DirStore struct {
	Dir string
}

func (s DirStore) file(key string) string {
	sum := sha256.Sum256([]byte(key))
	h := hex.EncodeToString(sum[:])
	return filepath.Join(s.Dir, h[:2], h+".json")
}

func (s DirStore) Get(key string) ([]byte, bool) {
	b, err := os.ReadFile(s.file(key))
	return b, err == nil
}

// Set writes the answer to a temp file first, so a run stopped part-way leaves no half answer.
func (s DirStore) Set(key string, body []byte) {
	f := s.file(key)
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		log.Warn("Could not keep an answer for the next run", "dir", s.Dir, err)
		return
	}
	tmp := f + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil { //nolint:gosec
		log.Warn("Could not keep an answer for the next run", "file", f, err)
		return
	}
	if err := os.Rename(tmp, f); err != nil {
		log.Warn("Could not keep an answer for the next run", "file", f, err)
	}
}
