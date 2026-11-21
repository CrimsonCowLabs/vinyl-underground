package migrations

import (
	"context"
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
	"github.com/navidrome/navidrome/consts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("upAddMediaFileSource", func() {
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
			INSERT INTO media_file VALUES ('mf-1', 'Bought on CD');
		`)
		Expect(err).ToNot(HaveOccurred())

		tx, err := db.BeginTx(ctx, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(upAddMediaFileSource(ctx, tx)).To(Succeed())
		Expect(tx.Commit()).To(Succeed())
	})

	It("gives existing Tracks an empty source", func() {
		var source, sourceURL string
		Expect(db.QueryRow(`SELECT source, source_url FROM media_file WHERE id = 'mf-1'`).
			Scan(&source, &sourceURL)).To(Succeed())
		Expect(source).To(BeEmpty())
		Expect(sourceURL).To(BeEmpty())
	})

	// Existing Files were read before the SOURCE tags were mapped, so only a full rescan
	// re-reads them; a quick scan skips Files whose modification time hasn't changed.
	It("forces a full rescan so existing Files are re-read", func() {
		var value string
		Expect(db.QueryRow(`SELECT value FROM property WHERE id = ?`, consts.FullScanAfterMigrationFlagKey).
			Scan(&value)).To(Succeed())
		Expect(value).To(Equal("1"))
	})
})
