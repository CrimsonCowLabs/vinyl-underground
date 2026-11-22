package persistence

import (
	"context"

	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DJRepository", func() {
	var mine, theirs model.DJRepository

	BeforeEach(func() {
		ctx := log.NewContext(context.TODO())
		mine = NewDJRepository(request.WithUser(ctx, adminUser), GetDBXBuilder())
		theirs = NewDJRepository(request.WithUser(ctx, regularUser), GetDBXBuilder())
		_, err := GetDBXBuilder().NewQuery("DELETE FROM dj").Execute()
		Expect(err).ToNot(HaveOccurred())
	})

	It("creates a Listener's DJs and lists them by name", func() {
		b := &model.DJ{Name: "Mr. Mellow", Description: "Slow and soft."}
		a := &model.DJ{Name: "Anna Analog", Description: "Only tape hiss."}
		Expect(mine.Put(b)).To(Succeed())
		Expect(mine.Put(a)).To(Succeed())
		Expect(a.ID).ToNot(BeEmpty())
		Expect(a.ID).ToNot(Equal(b.ID))

		got, err := mine.GetAll()
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(HaveLen(2))
		Expect(got[0].Name).To(Equal("Anna Analog"))
		Expect(got[0].Description).To(Equal("Only tape hiss."))
		Expect(got[1].Name).To(Equal("Mr. Mellow"))
		Expect(got[0].CreatedAt).ToNot(BeZero())
	})

	It("updates the Listener's DJ in place", func() {
		dj := &model.DJ{Name: "Mr. Mellow", Description: "Slow and soft."}
		Expect(mine.Put(dj)).To(Succeed())
		Expect(mine.Put(&model.DJ{ID: dj.ID, Name: "Mr. Loud", Description: "Fast and loud."})).To(Succeed())

		got, err := mine.Get(dj.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Name).To(Equal("Mr. Loud"))
		Expect(got.Description).To(Equal("Fast and loud."))
		Expect(mine.GetAll()).To(HaveLen(1))
	})

	It("keeps a DJ's genres and where they came from", func() {
		dj := &model.DJ{Name: "Techno Tina", Description: "Pounding techno.",
			Genres: model.DJGenres{"Techno", "Hardcore Techno"}, GenreSource: model.DJGenresFromWords}
		Expect(mine.Put(dj)).To(Succeed())
		got, err := mine.Get(dj.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Genres).To(Equal(model.DJGenres{"Techno", "Hardcore Techno"}))
		Expect(got.GenreSource).To(Equal(model.DJGenresFromWords))

		dj.Genres, dj.GenreSource = model.DJGenres{"House"}, model.DJGenresByListener
		Expect(mine.Put(dj)).To(Succeed())
		all, err := mine.GetAll()
		Expect(err).ToNot(HaveOccurred())
		Expect(all[0].Genres).To(Equal(model.DJGenres{"House"}))
		Expect(all[0].GenreSource).To(Equal(model.DJGenresByListener))
	})

	It("gives a DJ saved without genres none", func() {
		dj := &model.DJ{Name: "Mr. Mellow", Description: "Slow and soft."}
		Expect(mine.Put(dj)).To(Succeed())
		got, err := mine.Get(dj.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Genres).To(BeEmpty())
		Expect(got.GenreSource).To(Equal(model.DJGenresUnset))
	})

	It("reads a DJ saved before DJs had genres as having none, never worked out", func() {
		_, err := GetDBXBuilder().NewQuery(`INSERT INTO dj (id, user_id, name, description, created_at, updated_at)
			VALUES ('old-dj', {:user}, 'Old Timer', 'From before genres.', datetime('now'), datetime('now'))`).
			Bind(map[string]any{"user": adminUser.ID}).Execute()
		Expect(err).ToNot(HaveOccurred())

		got, err := mine.Get("old-dj")
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Genres).To(BeEmpty())
		Expect(got.GenreSource).To(Equal(model.DJGenresUnset))
	})

	It("deletes the Listener's DJ", func() {
		dj := &model.DJ{Name: "Mr. Mellow"}
		Expect(mine.Put(dj)).To(Succeed())
		Expect(mine.Delete(dj.ID)).To(Succeed())
		_, err := mine.Get(dj.ID)
		Expect(err).To(MatchError(model.ErrNotFound))
		Expect(mine.Delete(dj.ID)).To(MatchError(model.ErrNotFound))
	})

	Describe("another Listener", func() {
		var dj *model.DJ

		BeforeEach(func() {
			dj = &model.DJ{Name: "Mr. Mellow", Description: "Slow and soft."}
			Expect(mine.Put(dj)).To(Succeed())
		})

		It("doesn't see the DJ", func() {
			Expect(theirs.GetAll()).To(BeEmpty())
			_, err := theirs.Get(dj.ID)
			Expect(err).To(MatchError(model.ErrNotFound))
		})

		It("keeps the admin's DJ from them, and theirs from the admin", func() {
			own := &model.DJ{Name: "Bossa Bea", Description: "Bossa nova only."}
			Expect(theirs.Put(own)).To(Succeed())

			got, err := mine.GetAll()
			Expect(err).ToNot(HaveOccurred())
			Expect(got).To(HaveLen(1))
			Expect(got[0].ID).To(Equal(dj.ID))
			_, err = mine.Get(own.ID)
			Expect(err).To(MatchError(model.ErrNotFound))
			Expect(mine.Delete(own.ID)).To(MatchError(model.ErrNotFound))
		})

		It("can't change or delete it", func() {
			Expect(theirs.Put(&model.DJ{ID: dj.ID, Name: "Hijacked"})).To(MatchError(model.ErrNotFound))
			Expect(theirs.Delete(dj.ID)).To(MatchError(model.ErrNotFound))

			got, err := mine.Get(dj.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.Name).To(Equal("Mr. Mellow"))
		})
	})
})
