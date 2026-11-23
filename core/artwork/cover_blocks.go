package artwork

import (
	"bytes"
	"io"
	"slices"
	"strings"
)

// coverBlocks is the set of images, by content hash, an admin marked wrong for an album (album
// cover block Corrections, see model.CorrectionAlbumCoverBlock). Resolving the album's cover, its
// disc art or its tracks' own art skips them, falling back to the next source.
//
// The hash is the one the processor stores for every resolved image (xxh3 of its bytes), so the
// album's current cover is blocked by its item artwork hash, with no image to re-read. Checking a
// candidate means hashing its bytes, but only for an album that has blocks, and only for the
// candidate that would win; the bytes are then handed on in memory, so they are read once.
// They are loaded with the album's pick, see loadAlbumCovers.
type coverBlocks map[string]struct{}

// screen hands back a hit unless its image is blocked. It reads the hit's bytes (capped like the
// processor) only when there are blocks, returning the error of a read that failed.
func (b coverBlocks) screen(res resolution) (_ resolution, blocked bool, _ error) {
	if len(b) == 0 || res.reader == nil {
		return res, false, nil
	}
	data, err := readCapped(res.reader)
	res.reader.Close()
	if err != nil {
		return resolution{}, false, err
	}
	hash, err := hashImage(bytes.NewReader(data))
	if err != nil {
		return resolution{}, false, err
	}
	if _, ok := b[hash]; ok {
		return resolution{}, true, nil
	}
	res.reader = io.NopCloser(bytes.NewReader(data))
	return res, false, nil
}

// key names the set, for a cache key of an image chosen past it ("" when there are none).
func (b coverBlocks) key() string {
	hashes := make([]string, 0, len(b))
	for h := range b {
		hashes = append(hashes, h)
	}
	slices.Sort(hashes)
	return strings.Join(hashes, ",")
}
