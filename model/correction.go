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
	// CorrectionArtistMerge renames an Artist to the name of another, existing Artist, merging
	// them. It matches like CorrectionArtistRename, but its stars, ratings and play counts join
	// the other Artist's, and undoing it moves only those back (see
	// CorrectionRepository.MergeArtistAnnotations).
	CorrectionArtistMerge CorrectionKind = "artistMerge"
	// CorrectionAlbumCoverBlock marks an image wrong for the album in a folder: resolving the
	// album's cover skips it, falling back to the next source. Its MatchKey is keyed like
	// CorrectionAlbumCover's, and its ImageHash is the blocked image's content hash. An album can
	// have several.
	CorrectionAlbumCoverBlock CorrectionKind = "albumCoverBlock"
	// CorrectionAlbum corrects an album's title, album artist, year and MusicBrainz release (its
	// AlbumName, AlbumArtist, Year, MbzAlbumID and MbzReleaseGroupID; an empty one keeps the
	// tagged value). Like CorrectionAlbumCover it is
	// keyed by the album's folder (see AlbumFolderKey), as the tag values are what is wrong and
	// two albums can share them, and it applies to every File in the album's folders.
	CorrectionAlbum CorrectionKind = "album"
)

// IsAlbumCover says whether the Correction is about an album's cover (a choice or a block). It
// changes no tag, so saving or undoing it refreshes only the album's artwork.
func (k CorrectionKind) IsAlbumCover() bool {
	return k == CorrectionAlbumCover || k == CorrectionAlbumCoverBlock
}

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
	// ImageHash and ImageMime name the image a cover Correction makes the album's cover. A cover
	// block has only the ImageHash of the image it blocks. An album Correction with a picked
	// MusicBrainz release has its Cover Art Archive front cover, when the archive had one (see
	// AlbumCovers.Pick). Empty for other kinds.
	ImageHash string `structs:"image_hash" json:"-"`
	ImageMime string `structs:"image_mime" json:"-"`
	// AlbumName, AlbumArtist and Year are what an album Correction sets (see CorrectionAlbum):
	// empty or zero for what it leaves as tagged, and for other kinds.
	AlbumName   string `structs:"album_name" json:"albumName,omitempty"`
	AlbumArtist string `structs:"album_artist" json:"albumArtist,omitempty"`
	Year        int    `structs:"year" json:"year,omitempty"`
	// MbzAlbumID and MbzReleaseGroupID are the MusicBrainz release and release group an album
	// Correction sets (Find on MusicBrainz), as if tagged musicbrainz_albumid and
	// musicbrainz_releasegroupid. Empty when it leaves them as tagged.
	MbzAlbumID        string `structs:"mbz_album_id" json:"mbzAlbumId,omitempty"`
	MbzReleaseGroupID string `structs:"mbz_release_group_id" json:"mbzReleaseGroupId,omitempty"`

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

// AlbumCorrections returns every Correction of the kind keyed by any of the album's folders,
// skipping those being undone.
func AlbumCorrections(repo CorrectionRepository, kind CorrectionKind, al Album) (Corrections, error) {
	return albumCorrections(repo, al, kind)
}

func albumCorrections(repo CorrectionRepository, al Album, kinds ...CorrectionKind) (Corrections, error) {
	all, err := repo.FindByMatchKeys(al.FolderIDs, kinds...)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(all, func(c Correction) bool { return c.Undoing }), nil
}

// AlbumCovers is what admins decided about an album's cover (see AlbumCoverCorrections).
type AlbumCovers struct {
	// Choice is the image chosen or uploaded as the album's cover, nil when it has none.
	Choice *Correction
	// Pick is the album Correction whose picked MusicBrainz release brought a Cover Art Archive
	// cover (its ImageHash and ImageMime), nil when it has none. The Choice outranks it.
	Pick *Correction
	// Blocks are the images marked wrong for the album.
	Blocks Corrections
}

// AlbumCoverCorrections returns the album's cover choice, its picked release's cover and its
// cover blocks, with one query, skipping those being undone. Like AlbumCorrection, the choice (and
// the pick) keyed by the album's earliest folder wins.
func AlbumCoverCorrections(repo CorrectionRepository, al Album) (AlbumCovers, error) {
	all, err := albumCorrections(repo, al, CorrectionAlbumCover, CorrectionAlbumCoverBlock, CorrectionAlbum)
	if err != nil {
		return AlbumCovers{}, err
	}
	var res AlbumCovers
	choiceAt, pickAt := len(al.FolderIDs), len(al.FolderIDs)
	for _, c := range all {
		at := slices.Index(al.FolderIDs, c.MatchKey)
		switch {
		case c.Kind == CorrectionAlbumCoverBlock:
			res.Blocks = append(res.Blocks, c)
		case c.Kind == CorrectionAlbumCover && at >= 0 && at < choiceAt:
			choiceAt, res.Choice = at, &c
		case c.Kind == CorrectionAlbum && c.ImageHash != "" && at >= 0 && at < pickAt:
			pickAt, res.Pick = at, &c
		}
	}
	return res, nil
}

type CorrectionRepository interface {
	// GetAll returns every Correction, newest first, with its TrackCount.
	GetAll() (Corrections, error)
	Get(id string) (*Correction, error)
	// GetByMatchKey returns the Correction of the kind matching key, or ErrNotFound.
	GetByMatchKey(kind CorrectionKind, key string) (*Correction, error)
	// FindByMatchKeys returns every Correction of the kinds matching any of the keys, oldest first,
	// for kinds with several per key (CorrectionAlbumCoverBlock), or several kinds in one query.
	FindByMatchKeys(keys []string, kinds ...CorrectionKind) (Corrections, error)
	// Add stores a new Correction, setting its ID and CreatedAt. It returns ErrAlreadyExists if a
	// Correction of the same Kind already has its MatchKey (for CorrectionAlbumCoverBlock: its
	// MatchKey and ImageHash).
	Add(c *Correction) error
	// SetUndoing marks the Correction as being undone (see Correction.Undoing).
	SetUndoing(id string) error
	Delete(id string) error
	// FolderIDs returns the folders of the Tracks the Correction applies to now.
	FolderIDs(id string) ([]string, error)
	// AlbumFolderIDs returns the folders an album Correction keyed by key matches (see
	// AlbumFolderKey): key itself, and every folder of the albums in it, e.g. the album's
	// other discs.
	AlbumFolderIDs(key string) ([]string, error)

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
