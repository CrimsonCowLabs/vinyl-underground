package persistence

import (
	"context"

	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The Library manager's filters (#166), on the Track listing: each alone, combined, and paged.
var _ = Describe("MediaRepository Library manager filters", func() {
	var repo model.ResourceRepository
	tracks := []model.MediaFile{
		{ID: "lm-1", LibraryID: 1, Path: "lm/Artist A/Album 1/01 One.mp3", Title: "One", Album: "Album 1",
			Artist: "Artist A", AlbumArtist: "Artist A", HasAlbumArtist: true, Year: 1999, TrackNumber: 1, Suffix: "mp3"},
		{ID: "lm-2", LibraryID: 1, Path: "lm/Artist A/Album 1/02 Two.flac", Title: "Two", Album: "Album 1",
			Artist: "Artist A", AlbumArtist: "Artist A", Year: 1999, TrackNumber: 2, Suffix: "flac"},
		{ID: "lm-3", LibraryID: 1, Path: "lm/Artist A/Album 10/01 Three.mp3", Title: "Three", Album: "[Unknown Album]",
			Artist: "Artist A", AlbumArtist: "Artist A", HasAlbumArtist: true, Suffix: "mp3"},
		{ID: "lm-4", LibraryID: 1, Path: "lm/Rips/Four.m4a", Title: "Four Ripped", Album: "Ripped",
			Artist: "Artist B", AlbumArtist: "Artist B", HasAlbumArtist: true, Year: 2020, TrackNumber: 4, Suffix: "m4a",
			Source: "youtube", SourceURL: "https://www.youtube.com/watch?v=x"},
		{ID: "lm-5", LibraryID: 1, Path: "lm/CD_Rips/Five.MP3", Title: "Five", Album: "From CD",
			Artist: "Artist B", AlbumArtist: "Artist B", HasAlbumArtist: true, Year: 2001, TrackNumber: 5, Suffix: "MP3",
			Source: "CD"},
	}

	BeforeEach(func() {
		ctx := log.NewContext(context.TODO())
		ctx = request.WithUser(ctx, model.User{ID: "userid", IsAdmin: true})
		mr := NewMediaFileRepository(ctx, GetDBXBuilder())
		repo = mr.(model.ResourceRepository)
		for i := range tracks {
			t := tracks[i]
			Expect(mr.Put(&t)).To(Succeed())
		}
		DeferCleanup(func() {
			for _, t := range tracks {
				_ = mr.Delete(t.ID)
			}
		})
	})

	// ids reads the Tracks matching the filters (only the Library manager's fixtures), in path order.
	ids := func(filters map[string]any) []string {
		filters["folder"] = cmpOr(filters["folder"], "lm")
		res, err := repo.ReadAll(rest.QueryOptions{Filters: filters, Sort: "path"})
		Expect(err).ToNot(HaveOccurred())
		var out []string
		for _, mf := range res.(model.MediaFiles) {
			out = append(out, mf.ID)
		}
		n, err := repo.Count(rest.QueryOptions{Filters: filters})
		Expect(err).ToNot(HaveOccurred())
		Expect(n).To(BeEquivalentTo(len(out)), "Count agrees with ReadAll")
		return out
	}

	Describe("folder", func() {
		It("matches the Files inside the folder, and in its subfolders", func() {
			Expect(ids(map[string]any{"folder": "lm/Artist A"})).To(Equal([]string{"lm-1", "lm-2", "lm-3"}))
		})
		It("doesn't match a sibling folder whose name starts the same", func() {
			Expect(ids(map[string]any{"folder": "lm/Artist A/Album 1"})).To(Equal([]string{"lm-1", "lm-2"}))
		})
		It("ignores slashes around it and the letter case", func() {
			Expect(ids(map[string]any{"folder": "/LM/artist a/album 1/"})).To(Equal([]string{"lm-1", "lm-2"}))
		})
		It("takes LIKE wildcards literally", func() {
			Expect(ids(map[string]any{"folder": "lm/CD_Rips"})).To(Equal([]string{"lm-5"}))
			Expect(ids(map[string]any{"folder": "lm/CD%"})).To(BeEmpty())
		})
	})

	Describe("format", func() {
		It("matches the Files of a format, in any case", func() {
			Expect(ids(map[string]any{"format": "mp3"})).To(Equal([]string{"lm-1", "lm-3", "lm-5"}))
			Expect(ids(map[string]any{"format": "FLAC"})).To(Equal([]string{"lm-2"}))
		})
	})

	Describe("rip", func() {
		It("matches the Files whose SOURCEURL is an http(s) URL, not those with only a SOURCE", func() {
			Expect(ids(map[string]any{"rip": "true"})).To(Equal([]string{"lm-4"}))
		})
		It("matches every other File when false", func() {
			Expect(ids(map[string]any{"rip": "false"})).To(Equal([]string{"lm-1", "lm-2", "lm-3", "lm-5"}))
		})
	})

	Describe("missing_tag", func() {
		It("finds the Files with no album", func() {
			Expect(ids(map[string]any{"missing_tag": "album"})).To(Equal([]string{"lm-3"}))
		})
		It("finds the Files with no album artist tag, though their album artist reads as the artist", func() {
			Expect(ids(map[string]any{"missing_tag": "album_artist"})).To(Equal([]string{"lm-2"}))
		})
		It("finds the Files with no year", func() {
			Expect(ids(map[string]any{"missing_tag": "year"})).To(Equal([]string{"lm-3"}))
		})
		It("finds the Files with no track number", func() {
			Expect(ids(map[string]any{"missing_tag": "track_number"})).To(Equal([]string{"lm-3"}))
		})
		It("finds the Files missing every tag of a list", func() {
			Expect(ids(map[string]any{"missing_tag": []any{"year", "album"}})).To(Equal([]string{"lm-3"}))
			Expect(ids(map[string]any{"missing_tag": []any{"year", "album_artist"}})).To(BeEmpty())
		})
		It("matches nothing for an unknown tag", func() {
			Expect(ids(map[string]any{"missing_tag": "mood"})).To(BeEmpty())
		})
	})

	Describe("combined", func() {
		It("matches only the Files every filter matches", func() {
			Expect(ids(map[string]any{"folder": "lm/Artist A", "format": "mp3"})).To(Equal([]string{"lm-1", "lm-3"}))
			Expect(ids(map[string]any{"format": "mp3", "missing_tag": "year"})).To(Equal([]string{"lm-3"}))
			Expect(ids(map[string]any{"rip": "false", "format": "mp3", "folder": "lm/CD_Rips"})).To(Equal([]string{"lm-5"}))
		})
		It("combines with the free-text search", func() {
			Expect(ids(map[string]any{"title": "ripped", "rip": "true"})).To(Equal([]string{"lm-4"}))
			Expect(ids(map[string]any{"title": "ripped", "rip": "false"})).To(BeEmpty())
		})
		It("combines with the album", func() {
			res, err := repo.ReadAll(rest.QueryOptions{Filters: map[string]any{"folder": "lm"}})
			Expect(err).ToNot(HaveOccurred())
			var albumID string
			for _, mf := range res.(model.MediaFiles) {
				if mf.ID == "lm-2" {
					albumID = mf.AlbumID
				}
			}
			Expect(ids(map[string]any{"album_id": albumID, "format": "flac"})).To(Equal([]string{"lm-2"}))
		})
	})

	It("pages the matching Files, and counts them all", func() {
		filters := map[string]any{"folder": "lm", "format": "mp3"}
		page := func(offset int) []string {
			res, err := repo.ReadAll(rest.QueryOptions{Filters: filters, Sort: "path", Offset: offset, Max: 2})
			Expect(err).ToNot(HaveOccurred())
			var out []string
			for _, mf := range res.(model.MediaFiles) {
				out = append(out, mf.ID)
			}
			return out
		}
		Expect(page(0)).To(Equal([]string{"lm-1", "lm-3"}))
		Expect(page(2)).To(Equal([]string{"lm-5"}))
		Expect(repo.Count(rest.QueryOptions{Filters: filters})).To(BeEquivalentTo(3))
	})
})

func cmpOr(v any, def string) any {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}
