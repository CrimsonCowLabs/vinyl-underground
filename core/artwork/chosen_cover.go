package artwork

import (
	"bytes"
	"context"
	"errors"
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

// resolveChosenCover opens the album's chosen cover, if an admin chose one. A choice whose image is
// gone is a miss, so the album falls back to its default cover order.
func resolveChosenCover(ctx context.Context, ds model.DataStore, al model.Album) (resolution, bool, error) {
	cr, err := model.AlbumCorrection(ds.Correction(ctx), model.CorrectionAlbumCover, al)
	if errors.Is(err, model.ErrNotFound) {
		return resolution{}, false, nil
	}
	if err != nil {
		return resolution{}, false, err
	}
	if !validHash(cr.ImageHash) {
		return resolution{}, false, nil
	}
	res, ok := resolveLocalFile(ChosenCoverStore().path(cr.ImageHash, cr.ImageMime), ChosenSource)
	return res, ok, nil
}
