package artwork

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/model"
)

// ChosenSource is the model.ItemArtwork.Source of an album cover an admin chose (an album cover
// Correction, see model.CorrectionAlbumCover). It outranks every source in CoverArtPriority.
const ChosenSource = "chosen"

// chosenCoverFolder holds the chosen covers under the artwork folder. It is apart from the hashed
// store on purpose: that one is pruned of images no item points at, but a choice must outlive its
// album's state rows (an album whose ID changes, or one missing for a while, gets it back).
const chosenCoverFolder = "chosen"

// ChosenCoverStore is the content-addressed store of the images admins chose as album covers.
func ChosenCoverStore() *ImageStore {
	return NewImageStore(filepath.Join(conf.Server.DataFolder.String(), consts.ArtworkFolder, chosenCoverFolder))
}

// StoredCover is an image kept in the ChosenCoverStore.
type StoredCover struct {
	Hash string
	Mime string
}

// StoreChosenCover checks the bytes are an image and keeps them in the ChosenCoverStore, once per
// content. An image already stored is left untouched: its mtime backs the albums serving it.
func StoreChosenCover(data []byte) (StoredCover, error) {
	if int64(len(data)) > maxImageBytes() {
		return StoredCover{}, fmt.Errorf("%w: the image exceeds the size cap %d", model.ErrValidation, maxImageBytes())
	}
	_, format, err := decodeCapped(data)
	if err != nil {
		return StoredCover{}, fmt.Errorf("%w: %w", model.ErrValidation, err)
	}
	hash, err := hashImage(bytes.NewReader(data))
	if err != nil {
		return StoredCover{}, err
	}
	stored := StoredCover{Hash: hash, Mime: mimeForFormat(format)}
	store := ChosenCoverStore()
	if _, err := os.Stat(store.path(hash, stored.Mime)); err == nil {
		return stored, nil
	}
	return stored, store.Write(hash, stored.Mime, bytes.NewReader(data))
}

// albumCovers is what admins decided about an album's cover: the image they chose or uploaded, if
// any, and the images they marked wrong. The album's cover, its disc art and its Tracks' own art
// all follow it: the choice is tried before their priority order, and the blocks are skipped.
type albumCovers struct {
	choice *model.Correction // nil when the album has none
	blocks coverBlocks
}

// loadAlbumCovers loads both with one indexed query on the album's folders (see
// model.AlbumCoverCorrections), so it costs a Track's own art no more than the blocks alone did.
func loadAlbumCovers(ctx context.Context, ds model.DataStore, al model.Album) (albumCovers, error) {
	choice, blocks, err := model.AlbumCoverCorrections(ds.Correction(ctx), al)
	if err != nil {
		return albumCovers{}, err
	}
	covers := albumCovers{choice: choice}
	if len(blocks) > 0 {
		covers.blocks = make(coverBlocks, len(blocks))
		for _, b := range blocks {
			covers.blocks[b.ImageHash] = struct{}{}
		}
	}
	return covers, nil
}

// openChoice opens the chosen image. A choice whose image is gone is a miss, so the item falls
// back to its default order; one with no choice has no source at all.
func (a albumCovers) openChoice() (resolution, bool) {
	if a.choice == nil || !validHash(a.choice.ImageHash) {
		return resolution{}, false
	}
	return resolveLocalFile(ChosenCoverStore().path(a.choice.ImageHash, a.choice.ImageMime), ChosenSource)
}

// key names the choice and the blocks, for a cache key of an image resolved under them.
func (a albumCovers) key() string {
	var choice string
	if a.choice != nil {
		choice = a.choice.ImageHash
	}
	return choice + ";" + a.blocks.key()
}

// tryCovers starts a priority walk under the album's covers: the walk skips its blocks from here
// on, and its choice is tried first. decided means the walk is over: the choice won, or its image
// could not be read, as falling through would show (and persist) a lower-priority image as if the
// choice were gone. A blocked choice is gone.
func (c *chainState) tryCovers(covers albumCovers) (_ resolution, decided bool) {
	c.blocks = covers.blocks
	chosen, opened := covers.openChoice()
	switch {
	case opened || chosen.localError:
		if res, ok := c.try(ChosenSource, chosen, opened); ok {
			return res, true
		}
		if c.localErr {
			return c.exhausted(), true
		}
	case chosen.source != "":
		c.record(ChosenSource, OutcomeMiss, "")
	}
	return resolution{}, false
}
