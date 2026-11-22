package model

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/navidrome/navidrome/utils/str"
)

// CorrectionKind says what a Correction fixes and how it matches Files.
type CorrectionKind string

const (
	// CorrectionArtistRename renames an Artist everywhere in the Library: a Track artist or album
	// artist whose name has the Correction's MatchKey (see ArtistNameKey) gets its To name.
	CorrectionArtistRename CorrectionKind = "artistRename"
	// CorrectionAlbumCover makes a chosen image the cover of the album in a folder, over every
	// source in CoverArtPriority. Its MatchKey is one of the album's folder IDs (see
	// AlbumFolderKey), and its ImageHash and ImageMime name the image in the chosen-cover store.
	CorrectionAlbumCover CorrectionKind = "albumCover"
)

// Correction is an admin's fix to wrong tag values. It is kept in the app's database, never
// written to the Files, and the scanner applies it whenever it reads a File's tags, so every
// client sees the corrected value. A Correction never changes a Track's ID.
type Correction struct {
	ID   string         `structs:"id" json:"id"`
	Kind CorrectionKind `structs:"kind" json:"kind"`
	// MatchKey is what the Correction matches, in a form that depends on its Kind.
	MatchKey string `structs:"match_key" json:"-"`
	// From and To are the wrong and the corrected values, as shown to the admin.
	From      string    `structs:"from_value" db:"from_value" json:"from"`
	To        string    `structs:"to_value" db:"to_value" json:"to"`
	CreatedBy string    `structs:"created_by" json:"createdBy"`
	CreatedAt time.Time `structs:"created_at" json:"createdAt"`
	// Undoing marks a Correction being undone: the scanner no longer applies it, but moves what
	// followed it (e.g. an Artist's stars) back. It is deleted once its folders are rescanned.
	Undoing bool `structs:"undoing" json:"undoing"`
	// ImageHash and ImageMime name the image a cover Correction makes the album's cover; empty
	// for other kinds.
	ImageHash string `structs:"image_hash" json:"-"`
	ImageMime string `structs:"image_mime" json:"-"`

	// TrackCount is how many Tracks in the Library the Correction applies to now. Zero means it
	// is orphaned: it matches nothing.
	TrackCount int `structs:"-" json:"trackCount"`
}

type Corrections []Correction

// ArtistNameKey is how an Artist rename matches names: like the Artist ID, regardless of
// capitals and typographic quotes and dashes, and also of extra spaces.
func ArtistNameKey(name string) string {
	return str.Clear(strings.ToLower(strings.Join(strings.Fields(name), " ")))
}

// AlbumFolderKey is how a Correction of an album matches it: by its folder, not by its tag values,
// which may be what is wrong. An album on several folders (one per disc) is keyed by the first of
// its folder IDs in sort order, and matched by any of them, so adding a disc keeps it matching.
// "" when the album has no folder.
func AlbumFolderKey(al Album) string {
	if len(al.FolderIDs) == 0 {
		return ""
	}
	return slices.Min(al.FolderIDs)
}

// AlbumCorrection returns the Correction of the kind keyed by any of the album's folders, skipping
// one being undone, or ErrNotFound.
func AlbumCorrection(repo CorrectionRepository, kind CorrectionKind, al Album) (*Correction, error) {
	for _, folderID := range al.FolderIDs {
		c, err := repo.GetByMatchKey(kind, folderID)
		if errors.Is(err, ErrNotFound) || (err == nil && c.Undoing) {
			continue
		}
		return c, err
	}
	return nil, ErrNotFound
}

type CorrectionRepository interface {
	// GetAll returns every Correction, newest first, with its TrackCount.
	GetAll() (Corrections, error)
	Get(id string) (*Correction, error)
	// GetByMatchKey returns the Correction of the kind matching key, or ErrNotFound.
	GetByMatchKey(kind CorrectionKind, key string) (*Correction, error)
	// Add stores a new Correction, setting its ID and CreatedAt. It returns ErrAlreadyExists if a
	// Correction of the same Kind already has its MatchKey.
	Add(c *Correction) error
	// SetUndoing marks the Correction as being undone (see Correction.Undoing).
	SetUndoing(id string) error
	Delete(id string) error
	// FolderIDs returns the folders of the Tracks the Correction applies to now.
	FolderIDs(id string) ([]string, error)
}
