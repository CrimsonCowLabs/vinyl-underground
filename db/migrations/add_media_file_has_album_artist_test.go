package migrations

import (
	"context"
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
	"github.com/navidrome/navidrome/consts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("upAddMediaFileHasAlbumArtist", func() {
	var db *sql.DB
	ctx := context.Background()

	BeforeEach(func() {
		var err error
		db, err = sql.Open("sqlite3", "file::memory:")
		Expect(err).ToNot(HaveOccurred())
		db.SetMaxOpenConns(1)
		DeferCleanup(func() { _ = db.Close() })

		_, err = db.Exec(`
			CREATE TABLE media_file (id text primary key, title text);
			CREATE TABLE property (id text primary key, value text);
			INSERT INTO media_file VALUES ('mf-1', 'Scanned before');
		`)
		Expect(err).ToNot(HaveOccurred())

		tx, err := db.BeginTx(ctx, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(upAddMediaFileHasAlbumArtist(ctx, tx)).To(Succeed())
		Expect(tx.Commit()).To(Succeed())
	})

	// Until the rescan, the "no album artist" filter finds no File rather than every one.
	It("counts existing Files as having an album artist", func() {
		var has bool
		Expect(db.QueryRow(`SELECT has_album_artist FROM media_file WHERE id = 'mf-1'`).Scan(&has)).To(Succeed())
		Expect(has).To(BeTrue())
	})

	It("forces a full rescan so existing Files are re-read", func() {
		var value string
		Expect(db.QueryRow(`SELECT value FROM property WHERE id = ?`, consts.FullScanAfterMigrationFlagKey).
			Scan(&value)).To(Succeed())
		Expect(value).To(Equal("1"))
	})
})
