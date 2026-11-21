package db_test

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// goose refuses to start ("missing migrations") when a pending migration is older than one the
// database already ran, so the media_file source columns (the "Ripped" badge, #29) must come
// from a migration that sorts after #28's rip_job table.
var _ = Describe("Media file source columns migration", func() {
	const ripJobMigration = 20261120190000 // #28

	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	sourceColumns := func(d *sql.DB) []string {
		GinkgoHelper()
		rows, err := d.QueryContext(ctx,
			`SELECT name FROM pragma_table_info('media_file') WHERE name IN ('source', 'source_url') ORDER BY name`)
		Expect(err).ToNot(HaveOccurred())
		defer rows.Close()
		var names []string
		for rows.Next() {
			var n string
			Expect(rows.Scan(&n)).To(Succeed())
			names = append(names, n)
		}
		return names
	}

	It("runs after the rip_job migration", func() {
		d := newDB(ctx, ripJobMigration)
		Expect(sourceColumns(d)).To(BeEmpty())

		Expect(goose.UpContext(ctx, d, "migrations")).To(Succeed())
		Expect(sourceColumns(d)).To(Equal([]string{"source", "source_url"}))
	})

	It("migrates a fresh database up cleanly", func() {
		d := newDB(ctx, 0)

		Expect(sourceColumns(d)).To(Equal([]string{"source", "source_url"}))
		var ripJob int
		Expect(d.QueryRowContext(ctx,
			`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'rip_job'`).Scan(&ripJob)).To(Succeed())
		Expect(ripJob).To(Equal(1))
	})
})
