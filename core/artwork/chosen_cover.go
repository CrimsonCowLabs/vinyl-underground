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

// ChosenSource was the model.ItemArtwork.Source of an album cover an admin chose (an album cover
// Correction). A chosen cover is written as the album's cover.jpg now (#164), and the choices kept
// before are written by the Correction migration (#165), so nothing resolves to it any more; an
// album's state row may still name it until the album is resolved again.
const ChosenSource = "chosen"

// chosenCoverFolder holds the images of picked releases' covers, of the cover picker's candidates,
// and of the choices kept before #164 (until migrated), under the artwork folder. It is apart from
// the hashed store on purpose: that one is pruned of images no item points at, but a pick must
// outlive its album's state rows (an album whose ID changes, or one missing for a while, gets it
// back).
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

// PickSource was the model.ItemArtwork.Source of the Cover Art Archive cover of an album's picked
// MusicBrainz release (Find on MusicBrainz). A picked release is written into the Files now
// (#170), its cover as the album's cover.jpg when the admin asks, and the picks kept before are
// written by the Correction migration, so nothing resolves to it any more; an album's state row
// may still name it until the album is resolved again.
const PickSource = "musicbrainzPick"

// albumCovers is what admins decided about an album's cover that stays app data: the images they
// marked wrong. The album's cover, its disc art and its Tracks' own art all skip them. (An image
// chosen or uploaded is the album's cover.jpg.)
type albumCovers struct {
	blocks coverBlocks
}

// loadAlbumCovers loads them with one indexed query on the album's folders (see
// model.AlbumCoverCorrections).
func loadAlbumCovers(ctx context.Context, ds model.DataStore, al model.Album) (albumCovers, error) {
	found, err := model.AlbumCoverCorrections(ds.Correction(ctx), al)
	if err != nil {
		return albumCovers{}, err
	}
	var covers albumCovers
	if len(found.Blocks) > 0 {
		covers.blocks = make(coverBlocks, len(found.Blocks))
		for _, b := range found.Blocks {
			covers.blocks[b.ImageHash] = struct{}{}
		}
	}
	return covers, nil
}

// key names the blocks, for a cache key of an image resolved under them.
func (a albumCovers) key() string {
	return ";" + a.blocks.key()
}

// tryCovers starts a priority walk under the album's covers: the walk skips its blocks from here
// on. Nothing outranks the priority order any more, so the walk is never decided here.
func (c *chainState) tryCovers(covers albumCovers) (_ resolution, decided bool) {
	c.blocks = covers.blocks
	return resolution{}, false
}
