// Package corrections writes an admin's fixes to wrong tag values into the Files (ADR-0011): the
// album Edit dialog, Track order and artist renames and merges are edit jobs (write.go), and the
// Corrections the app kept before, which it applied whenever it read a File's tags, are written
// into the Files by the Correction migration (migrate.go, #165). Nothing applies a Correction on
// read any more. A MusicBrainz release picked with Find on MusicBrainz is written into the Files
// too (release.go, #170). What stays app data is kept here: an image marked wrong for an album (a
// cover block), which artwork skips.
package corrections

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/musicbrainz"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

var (
	// ErrArtistExists is returned when renaming an Artist to the name of another Artist: that
	// merges them, which PlanArtistMerge does.
	ErrArtistExists = fmt.Errorf("%w: an Artist with that name already exists", model.ErrAlreadyExists)
	// ErrRenamedAway is returned when renaming an Artist to a name a Correction not yet migrated
	// renames away: the migration would rename it again.
	ErrRenamedAway = fmt.Errorf("%w: a Correction renames that name away; migrate it first", model.ErrValidation)
	// ErrCoverChanged is returned when marking an album's cover wrong, but the album no longer
	// shows the image the admin saw (or shows none).
	ErrCoverChanged = fmt.Errorf("%w: the album's cover has changed", model.ErrValidation)
	// ErrMigrateFirst is returned when undoing a Correction the migration writes into the Files:
	// only the app data a Correction still is (a cover block, a picked release) can be undone.
	ErrMigrateFirst = fmt.Errorf("%w: this Correction is written into the Files by the migration", model.ErrValidation)
)

// Applied is the outcome of saving or undoing a Correction.
type Applied struct {
	Correction model.Correction `json:"correction"`
	// Done is always true: saving or undoing one rescans nothing.
	Done bool `json:"done"`
}

type Corrections interface {
	// GetAll returns every Correction, newest first, with how many Tracks each applies to now.
	GetAll(ctx context.Context) (model.Corrections, error)
	// BlockAlbumCover marks the image the album shows wrong for it, by its content hash (the one
	// the client saw, ErrCoverChanged if the album shows another now). The album, its disc art and
	// its tracks' own art skip it from then on, falling back to the next source in the cover
	// order.
	BlockAlbumCover(ctx context.Context, albumID, imageHash string) (*Applied, error)
	// Undo deletes a cover block, and resolves the covers of the albums it applied to again. The
	// Corrections the migration writes can't be undone (ErrMigrateFirst).
	Undo(ctx context.Context, id string) (*Applied, error)

	// The album Edit dialog, Track order and artist rename and merge write to the Files (#163):
	// these work out the edit job each queues (see write.go).
	PlanAlbumEdit(ctx context.Context, albumID string, fix *AlbumFix, numbers []TrackNumber) (*Plan, error)
	PlanArtistRename(ctx context.Context, artistID, name string) (*Plan, error)
	PlanArtistMerge(ctx context.Context, artistID, intoID string) (*Plan, error)

	// Find on MusicBrainz writes the picked release into the album's Files (#170, release.go):
	// MatchRelease pairs the album's Tracks with the release, as the dialog shows it before
	// saving, and PlanRelease works out the job (JobRelease) writing it.
	MatchRelease(ctx context.Context, albumID string, rel *musicbrainz.Release) (*ReleaseMatch, error)
	PlanRelease(ctx context.Context, editor *tagedit.Editor, albumID string, rel *musicbrainz.Release, w ReleaseWrite) (*Plan, error)

	// PlanMigration lists every Correction the Correction migration writes into the Files (#165),
	// with what writing it changes and the Files it can't write; it writes nothing.
	PlanMigration(ctx context.Context, editor *tagedit.Editor) (*Migration, error)
	// MigrationJob works out the migration's edit job (JobMigrate). With no File to write it
	// removes the Corrections that apply to nothing, and returns ErrNothingToWrite.
	MigrationJob(ctx context.Context, editor *tagedit.Editor) (*Plan, error)
}

// Options: PickedCover fetches and stores a picked release's Cover Art Archive front cover (see
// artwork.FetchPickedCover); nil asks nothing, and a pick has no cover.
type Options struct {
	PickedCover func(ctx context.Context, releaseID, releaseGroupID string) (artwork.StoredCover, error)
}

func New(ds model.DataStore) Corrections {
	return NewWithOptions(ds, Options{PickedCover: artwork.FetchPickedCover})
}

func NewWithOptions(ds model.DataStore, opts Options) Corrections {
	return &corrections{ds: ds, opts: opts}
}

type corrections struct {
	ds   model.DataStore
	opts Options
}

func (c *corrections) GetAll(ctx context.Context) (model.Corrections, error) {
	return c.ds.Correction(ctx).GetAll()
}

func (c *corrections) BlockAlbumCover(ctx context.Context, albumID, imageHash string) (*Applied, error) {
	al, err := c.ds.Album(ctx).Get(albumID)
	if err != nil {
		return nil, err
	}
	key := model.AlbumFolderKey(*al)
	if key == "" {
		return nil, fmt.Errorf("%w: the album has no folder", model.ErrValidation)
	}
	// The hash the album's cover was stored under is the one its resolution compares (see
	// artwork's coverBlocks), whatever the source
	shown, err := c.ds.Artwork(ctx).GetItemArtwork(model.KindAlbumArtwork, al.ID, model.ImageTypePrimary)
	// An album settled absent has an empty hash, which an empty imageHash must not match
	if errors.Is(err, model.ErrNotFound) || (err == nil && (shown.Hash == "" || shown.Hash != imageHash)) {
		return nil, ErrCoverChanged
	}
	if err != nil {
		return nil, err
	}
	cr := model.Correction{Kind: model.CorrectionAlbumCoverBlock, MatchKey: key, From: albumLabel(*al),
		To: coverSourceLabel(shown), ImageHash: shown.Hash}
	if err := c.ds.Correction(ctx).Add(&cr); err != nil {
		return nil, err
	}
	log.Info(ctx, "Corrections: marking an album cover wrong", "album", al.Name, "image", cr.To, "hash", cr.ImageHash)
	// The block is saved either way, and applies whenever the albums are next resolved
	if err := c.refreshFolderCovers(ctx, key); err != nil {
		log.Warn(ctx, "Corrections: could not refresh the covers", "album", al.Name, err)
	}
	return &Applied{Correction: cr, Done: true}, nil
}

// coverSourceLabel names the image an album shows, for the Corrections page: its file name, or
// where it came from.
func coverSourceLabel(ia *model.ItemArtwork) string {
	if ia.SourcePath != "" {
		switch ia.Source {
		case "folder":
			return filepath.Base(ia.SourcePath)
		case "embedded":
			return "Embedded art: " + filepath.Base(ia.SourcePath)
		}
	}
	// "lastfm", "coverartarchive", "chosen"
	return strings.TrimPrefix(ia.Source, artwork.ExternalPrefix)
}

// albumLabel names an album for the Corrections page: "Band – Record".
func albumLabel(al model.Album) string {
	if al.AlbumArtist == "" {
		return al.Name
	}
	return al.AlbumArtist + " – " + al.Name
}

// refreshFolderCovers refreshes the covers of every album a Correction keyed by the folder applies
// to (albums sharing a folder share its Corrections), and their Tracks' art. A failure for one
// album is only logged: the Correction applies whenever it is next resolved.
func (c *corrections) refreshFolderCovers(ctx context.Context, folderKey string) error {
	albums, err := c.ds.Album(ctx).GetAll(model.QueryOptions{
		Filters: squirrel.Expr("exists (select 1 from json_each(album.folder_ids) af where af.value = ?)", folderKey),
	})
	if err != nil {
		return err
	}
	for _, al := range albums {
		if err := artwork.RefreshAlbumCover(ctx, c.ds, al.ID); err != nil {
			log.Warn(ctx, "Corrections: could not refresh the album's cover", "album", al.Name, err)
		}
	}
	return nil
}

// artistCorrection returns the artist rename or merge matching the name key, or ErrNotFound.
func (c *corrections) artistCorrection(ctx context.Context, key string) (*model.Correction, error) {
	cr, err := c.ds.Correction(ctx).GetByMatchKey(model.CorrectionArtistRename, key)
	if errors.Is(err, model.ErrNotFound) {
		return c.ds.Correction(ctx).GetByMatchKey(model.CorrectionArtistMerge, key)
	}
	return cr, err
}

func isPlaceholder(name string) bool {
	return name == consts.UnknownArtist || name == consts.VariousArtists
}

func (c *corrections) Undo(ctx context.Context, id string) (*Applied, error) {
	repo := c.ds.Correction(ctx)
	cr, err := repo.Get(id)
	if err != nil {
		return nil, err
	}
	if migrates(*cr) {
		return nil, ErrMigrateFirst
	}
	if err := repo.Delete(cr.ID); err != nil {
		return nil, err
	}
	log.Info(ctx, "Corrections: undoing", "kind", cr.Kind, "album", cr.From, "to", cr.To)
	// A block: only the covers change.
	if err := c.refreshFolderCovers(ctx, cr.MatchKey); err != nil {
		return nil, err
	}
	return &Applied{Correction: *cr, Done: true}, nil
}
