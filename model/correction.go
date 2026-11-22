package model

import (
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
	// CorrectionArtistMerge renames an Artist to the name of another, existing Artist, merging
	// them. It matches like CorrectionArtistRename, but its stars, ratings and play counts join
	// the other Artist's, and undoing it moves only those back (see
	// CorrectionRepository.MergeArtistAnnotations).
	CorrectionArtistMerge CorrectionKind = "artistMerge"
)

// IsArtistRename says whether the Correction renames an Artist, merging it or not.
func (k CorrectionKind) IsArtistRename() bool {
	return k == CorrectionArtistRename || k == CorrectionArtistMerge
}

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

	// MergeArtistAnnotations merges every Listener's star, rating and play count of Artist fromID into
	// Artist toID's, for the artist merge Correction with correctionID: play counts are added
	// together, and toID keeps its own star and rating, taking fromID's only where it has none.
	// What it moved is kept with the Correction, for SplitArtistAnnotations.
	MergeArtistAnnotations(correctionID, fromID, toID string) error
	// SplitArtistAnnotations moves back from Artist fromID to Artist toID what the merge with
	// correctionID moved in: the play counts it added, and the star and rating it gave fromID
	// unless they have changed since.
	SplitArtistAnnotations(correctionID, fromID, toID string) error
}
