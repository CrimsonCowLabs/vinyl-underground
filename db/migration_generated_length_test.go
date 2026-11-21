package db_test

import (
	"context"

	"github.com/pressly/goose/v3"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Generated Playlists saved before their length was stored (#10) still need one to be
// regenerated (#11), so the migration guesses it from their Track count.
var _ = Describe("Generated Playlist length migration", func() {
	const promptMigration = 20261121030000 // last migration before the length column

	It("gives existing Generated Playlists their Track count, within 10 to 100", func() {
		ctx := context.Background()
		d := newDB(ctx, promptMigration)
		_, err := d.ExecContext(ctx, `INSERT INTO user (id, user_name, name, email, password, is_admin, created_at, updated_at)
			VALUES ('u1', 'listener', 'Listener', '', '', false, datetime('now'), datetime('now'))`)
		Expect(err).ToNot(HaveOccurred())
		for _, p := range []struct {
			id, prompt string
			songs      int
		}{
			{"generated", "rainy Sunday 90s indie", 30},
			{"generated-then-trimmed", "late-night drive", 4},
			{"ordinary", "", 30},
		} {
			_, err := d.ExecContext(ctx, `INSERT INTO playlist (id, name, owner_id, prompt, song_count, created_at, updated_at)
				VALUES (?, ?, 'u1', ?, ?, datetime('now'), datetime('now'))`, p.id, p.id, p.prompt, p.songs)
			Expect(err).ToNot(HaveOccurred())
		}

		Expect(goose.UpContext(ctx, d, "migrations")).To(Succeed())

		lengths := map[string]int{}
		rows, err := d.QueryContext(ctx, `SELECT id, generated_length FROM playlist`)
		Expect(err).ToNot(HaveOccurred())
		defer rows.Close()
		for rows.Next() {
			var id string
			var n int
			Expect(rows.Scan(&id, &n)).To(Succeed())
			lengths[id] = n
		}
		Expect(rows.Err()).ToNot(HaveOccurred())
		Expect(lengths).To(Equal(map[string]int{"generated": 30, "generated-then-trimmed": 10, "ordinary": 0}))
	})
})
