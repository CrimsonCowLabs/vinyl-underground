package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigrationContext(upAddMediaFileHasAlbumArtist, downAddMediaFileHasAlbumArtist)
}

// upAddMediaFileHasAlbumArtist records whether each File has an album artist tag, for the Library
// manager's "no album artist" filter (#166): a Track's album_artist falls back to its artist, so
// it can't tell. The value only comes from the File's tags, so existing Files need a full rescan
// to be re-read (a quick scan skips Files whose modification time hasn't changed). Until then
// every File counts as having one, so the filter finds none rather than nearly all.
func upAddMediaFileHasAlbumArtist(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
ALTER TABLE media_file ADD COLUMN has_album_artist bool default true not null;
`)
	if err != nil {
		return err
	}
	notice(ctx, tx, "A full rescan will be performed to find the Files with no album artist tag")
	return forceFullRescan(ctx, tx)
}

func downAddMediaFileHasAlbumArtist(_ context.Context, _ *sql.Tx) error {
	return nil
}
