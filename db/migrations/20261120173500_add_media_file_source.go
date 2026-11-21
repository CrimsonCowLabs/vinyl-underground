package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigrationContext(upAddMediaFileSource, downAddMediaFileSource)
}

// upAddMediaFileSource stores a Rip's SOURCE and SOURCEURL tags (issue #29). The values only
// ever come from the File's tags, so existing Files need a full rescan to be re-read: a quick
// scan skips Files whose modification time hasn't changed.
func upAddMediaFileSource(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
ALTER TABLE media_file ADD COLUMN source varchar default '' not null;
ALTER TABLE media_file ADD COLUMN source_url varchar default '' not null;
`)
	if err != nil {
		return err
	}
	notice(ctx, tx, "A full rescan will be performed to read the source tags of Rips")
	return forceFullRescan(ctx, tx)
}

func downAddMediaFileSource(_ context.Context, _ *sql.Tx) error {
	return nil
}
