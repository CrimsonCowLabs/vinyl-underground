// Package corrections lets admins fix wrong tag values without touching the (read-only) Files.
// A Correction is kept in the database and applied by the scanner whenever it reads tags (see
// metadata.Corrections), so saving or undoing one rescans the folders it affects and refreshes
// them (see refresh.Refresher).
package corrections

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/refresh"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/metadata"
)

var (
	// ErrArtistExists is returned when renaming an Artist to the name of another Artist: that
	// merges them, which MergeArtist does.
	ErrArtistExists = fmt.Errorf("%w: an Artist with that name already exists", model.ErrAlreadyExists)
	// ErrRenamedAway is returned when renaming an Artist to a name a Correction renames away:
	// undoing that Correction is the way to get it back.
	ErrRenamedAway = fmt.Errorf("%w: a Correction renames that name away; undo it instead", model.ErrValidation)
	// ErrUndoLaterFirst is returned when undoing an artist rename or merge that a later Correction
	// builds on: one renaming its new name onward (A→B, then B→C), or merging another Artist into
	// its new name. Undoing that one first keeps the Artists' stars and play counts apart.
	ErrUndoLaterFirst = fmt.Errorf("%w: a later Correction builds on this one; undo it first", model.ErrValidation)
	// ErrCoverChanged is returned when marking an album's cover wrong, but the album no longer
	// shows the image the admin saw (or shows none).
	ErrCoverChanged = fmt.Errorf("%w: the album's cover has changed", model.ErrValidation)
)

// Applied is the outcome of saving or undoing a Correction.
type Applied struct {
	Correction model.Correction `json:"correction"`
	// ArtistID is the ID the Artist has once its folders are rescanned.
	ArtistID string `json:"artistId,omitempty"`
	// AlbumID is the ID a corrected album has once its folders are rescanned (if Done).
	AlbumID string `json:"albumId,omitempty"`
	// Done is false when the rescan outlasted Options.Wait and carries on in the background.
	Done bool `json:"done"`
}

type Corrections interface {
	// GetAll returns every Correction, newest first, with how many Tracks each applies to now.
	GetAll(ctx context.Context) (model.Corrections, error)
	// RenameArtist saves a Correction renaming the Artist everywhere in the Library, then rescans
	// the folders of the albums it is album artist or Track artist on, and refreshes it.
	RenameArtist(ctx context.Context, artistID, name string) (*Applied, error)
	// ChooseAlbumCover makes the image the album's cover, over every source in CoverArtPriority,
	// replacing an earlier choice; it is the album's disc art and its tracks' art too. The
	// Correction is keyed by the album's folder (see model.AlbumFolderKey), and saving it refreshes
	// the artwork of the albums in that folder and of their tracks. The image must already be in the
	// chosen-cover store (see artwork.ChosenCoverStore). Choosing an image blocked for the album
	// lifts the block.
	ChooseAlbumCover(ctx context.Context, albumID string, cover AlbumCover) (*Applied, error)
	// BlockAlbumCover marks the image the album shows wrong for it, by its content hash (the one
	// the client saw, ErrCoverChanged if the album shows another now). The album, its disc art and
	// its tracks' own art skip it from then on, falling back to the next source in the cover
	// order. Marking the chosen cover wrong also removes the choice.
	BlockAlbumCover(ctx context.Context, albumID, imageHash string) (*Applied, error)
	// MergeArtist saves a Correction renaming the Artist to the name of the Artist intoID, merging
	// them: their stars, ratings and play counts are joined (see
	// model.CorrectionRepository.MergeArtistAnnotations). It rescans like RenameArtist.
	MergeArtist(ctx context.Context, artistID, intoID string) (*Applied, error)
	// CorrectAlbum saves a Correction of the album's title, album artist and year, keyed by its
	// folder (see model.AlbumFolderKey) and replacing an earlier one, then rescans the album's
	// folders and refreshes it. Only what differs from the album now is corrected.
	CorrectAlbum(ctx context.Context, albumID string, fix AlbumFix) (*Applied, error)
	// Undo stops applying the Correction, rescans the folders of the Tracks it applied to, and
	// then deletes it. If the rescan fails, it stays marked as undoing, and Undo can be retried.
	Undo(ctx context.Context, id string) (*Applied, error)
}

// AlbumCover is a stored image to make an album's cover, and where it came from, as shown to the
// admin ("Cover Art Archive: Record (GB, CD, 1999)").
type AlbumCover struct {
	artwork.StoredCover
	Source string
}

// Options: Wait is how long saving or undoing waits for its rescan before returning and letting
// it finish in the background.
type Options struct {
	Wait time.Duration
}

func New(ds model.DataStore, refresher refresh.Refresher) Corrections {
	return NewWithOptions(ds, refresher, Options{})
}

func NewWithOptions(ds model.DataStore, refresher refresh.Refresher, opts Options) Corrections {
	opts.Wait = cmp.Or(opts.Wait, 5*time.Second)
	return &corrections{ds: ds, refresher: refresher, opts: opts}
}

type corrections struct {
	ds        model.DataStore
	refresher refresh.Refresher
	opts      Options
}

func (c *corrections) GetAll(ctx context.Context) (model.Corrections, error) {
	return c.ds.Correction(ctx).GetAll()
}

func (c *corrections) RenameArtist(ctx context.Context, artistID, name string) (*Applied, error) {
	name = strings.Join(strings.Fields(name), " ")
	ar, err := c.ds.Artist(ctx).Get(artistID)
	if err != nil {
		return nil, err
	}
	switch {
	case name == "":
		return nil, fmt.Errorf("%w: the new name is empty", model.ErrValidation)
	case name == ar.Name:
		return nil, fmt.Errorf("%w: the name is unchanged", model.ErrValidation)
	case isPlaceholder(ar.Name):
		return nil, fmt.Errorf("%w: %s can't be renamed", model.ErrValidation, ar.Name)
	}
	newID := metadata.ArtistID(name)
	if newID != ar.ID {
		if _, err := c.ds.Artist(ctx).Get(newID); err == nil {
			return nil, ErrArtistExists
		} else if !errors.Is(err, model.ErrNotFound) {
			return nil, err
		}
	}
	return c.saveArtistRename(ctx, ar, model.CorrectionArtistRename, name)
}

func (c *corrections) MergeArtist(ctx context.Context, artistID, intoID string) (*Applied, error) {
	ar, err := c.ds.Artist(ctx).Get(artistID)
	if err != nil {
		return nil, err
	}
	into, err := c.ds.Artist(ctx).Get(intoID)
	if err != nil {
		return nil, err
	}
	switch {
	case ar.ID == into.ID:
		return nil, fmt.Errorf("%w: an Artist can't be merged into itself", model.ErrValidation)
	case isPlaceholder(ar.Name) || isPlaceholder(into.Name):
		return nil, fmt.Errorf("%w: %s and %s can't be merged", model.ErrValidation, ar.Name, into.Name)
	}
	return c.saveArtistRename(ctx, ar, model.CorrectionArtistMerge, into.Name)
}

// saveArtistRename saves the Correction renaming (or merging) the Artist to name, and rescans the
// Artist's folders.
func (c *corrections) saveArtistRename(ctx context.Context, ar *model.Artist, kind model.CorrectionKind, name string) (*Applied, error) {
	if _, err := c.artistCorrection(ctx, model.ArtistNameKey(name)); err == nil {
		return nil, ErrRenamedAway
	} else if !errors.Is(err, model.ErrNotFound) {
		return nil, err
	}
	// The repository only refuses a second Correction of the same kind
	if _, err := c.artistCorrection(ctx, model.ArtistNameKey(ar.Name)); err == nil {
		return nil, model.ErrAlreadyExists
	} else if !errors.Is(err, model.ErrNotFound) {
		return nil, err
	}

	targets, err := c.refresher.ScanTargets(ctx, model.KindArtistArtwork, ar.ID)
	if err != nil {
		return nil, err
	}
	cr := model.Correction{Kind: kind, MatchKey: model.ArtistNameKey(ar.Name), From: ar.Name, To: name}
	if err := c.ds.Correction(ctx).Add(&cr); err != nil {
		return nil, err
	}
	newID := metadata.ArtistID(name)
	log.Info(ctx, "Corrections: renaming Artist", "kind", kind, "from", ar.Name, "to", name, "folders", len(targets))
	done := c.rescan(ctx, targets, refresh.Item{Kind: model.KindArtistArtwork, ID: newID}, nil)
	return &Applied{Correction: cr, ArtistID: newID, Done: done}, nil
}

func (c *corrections) ChooseAlbumCover(ctx context.Context, albumID string, cover AlbumCover) (*Applied, error) {
	if cover.Hash == "" || cover.Mime == "" || cover.Source == "" {
		return nil, fmt.Errorf("%w: no cover image", model.ErrValidation)
	}
	al, err := c.ds.Album(ctx).Get(albumID)
	if err != nil {
		return nil, err
	}
	key := model.AlbumFolderKey(*al)
	if key == "" {
		return nil, fmt.Errorf("%w: the album has no folder", model.ErrValidation)
	}
	cr := model.Correction{Kind: model.CorrectionAlbumCover, MatchKey: key, From: albumLabel(*al), To: cover.Source,
		ImageHash: cover.Hash, ImageMime: cover.Mime}
	err = c.ds.WithTx(func(tx model.DataStore) error {
		repo := tx.Correction(ctx)
		if old, err := model.AlbumCorrection(repo, model.CorrectionAlbumCover, *al); err == nil {
			if err := repo.Delete(old.ID); err != nil {
				return err
			}
		} else if !errors.Is(err, model.ErrNotFound) {
			return err
		}
		// Choosing an image the admin had marked wrong takes that back
		if err := deleteAlbumCorrections(repo, model.CorrectionAlbumCoverBlock, *al, cover.Hash); err != nil {
			return err
		}
		return repo.Add(&cr)
	})
	if err != nil {
		return nil, err
	}
	log.Info(ctx, "Corrections: choosing an album cover", "album", al.Name, "source", cover.Source, "hash", cover.Hash)
	// A cover is no tag: nothing to rescan, only the artwork of every album the choice applies to,
	// and of their tracks (which show it too), to resolve again
	if err := c.refreshFolderCovers(ctx, key); err != nil {
		log.Warn(ctx, "Corrections: could not refresh the covers", "album", al.Name, err)
	}
	return &Applied{Correction: cr, Done: true}, nil
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
	err = c.ds.WithTx(func(tx model.DataStore) error {
		repo := tx.Correction(ctx)
		// A wrong choice is no choice: the album falls back to the cover order, past this image
		if choice, err := model.AlbumCorrection(repo, model.CorrectionAlbumCover, *al); err == nil {
			if choice.ImageHash == shown.Hash {
				cr.To = choice.To
				if err := repo.Delete(choice.ID); err != nil {
					return err
				}
			}
		} else if !errors.Is(err, model.ErrNotFound) {
			return err
		}
		return repo.Add(&cr)
	})
	if err != nil {
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

// deleteAlbumCorrections deletes the album's Corrections of the kind for the image.
func deleteAlbumCorrections(repo model.CorrectionRepository, kind model.CorrectionKind, al model.Album, imageHash string) error {
	all, err := model.AlbumCorrections(repo, kind, al)
	if err != nil {
		return err
	}
	for _, c := range all {
		if c.ImageHash != imageHash {
			continue
		}
		if err := repo.Delete(c.ID); err != nil {
			return err
		}
	}
	return nil
}

// albumLabel names an album for the Corrections page: "Band – Record".
func albumLabel(al model.Album) string {
	if al.AlbumArtist == "" {
		return al.Name
	}
	return al.AlbumArtist + " – " + al.Name
}

// undoAlbumCover deletes a cover choice or block and refreshes the albums in its folder, which go
// back to the default cover order, and their tracks' art.
func (c *corrections) undoAlbumCover(ctx context.Context, cr *model.Correction) (*Applied, error) {
	if err := c.ds.Correction(ctx).Delete(cr.ID); err != nil {
		return nil, err
	}
	log.Info(ctx, "Corrections: undoing an album cover Correction", "kind", cr.Kind, "album", cr.From, "image", cr.To)
	if err := c.refreshFolderCovers(ctx, cr.MatchKey); err != nil {
		return nil, err
	}
	return &Applied{Correction: *cr, Done: true}, nil
}

// refreshFolderCovers refreshes the covers of every album an album cover Correction keyed by the
// folder applies to (albums sharing a folder share its Corrections), and their tracks' art.
func (c *corrections) refreshFolderCovers(ctx context.Context, folderKey string) error {
	albums, err := c.ds.Album(ctx).GetAll(model.QueryOptions{
		Filters: squirrel.Expr("exists (select 1 from json_each(album.folder_ids) af where af.value = ?)", folderKey),
	})
	if err != nil {
		return err
	}
	for _, al := range albums {
		c.refreshCover(ctx, al)
	}
	return nil
}

// refreshCover resolves the album's cover again, and its tracks' own art, which follow its choice
// and blocks. A failure is only logged: the Correction is saved either way, and applies whenever
// they are next resolved.
func (c *corrections) refreshCover(ctx context.Context, al model.Album) {
	if err := artwork.RefreshAlbumCover(ctx, c.ds, al.ID); err != nil {
		log.Warn(ctx, "Corrections: could not refresh the album's cover", "album", al.Name, err)
	}
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

// checkUndoOrder returns ErrUndoLaterFirst if another artist Correction builds on cr: it renames
// cr's new name onward, or it merges another Artist into cr's new name later. A plain rename's
// undo moves all of the new name's stars back, and a merge's undo the star the target took from
// it, which a later merge may rely on. An album Correction setting a rename's new name as its album
// artist builds on it too: undone first, the rename would leave that album alone under the new name.
func (c *corrections) checkUndoOrder(ctx context.Context, cr *model.Correction) error {
	if !cr.Kind.IsArtistRename() {
		return nil
	}
	all, err := c.ds.Correction(ctx).GetAll()
	if err != nil {
		return err
	}
	to := model.ArtistNameKey(cr.To)
	for _, o := range all {
		if cr.Kind == model.CorrectionArtistRename && o.Kind == model.CorrectionAlbum && !o.Undoing &&
			o.AlbumArtist != "" && model.ArtistNameKey(o.AlbumArtist) == to {
			return ErrUndoLaterFirst
		}
		if o.ID == cr.ID || o.Undoing || !o.Kind.IsArtistRename() {
			continue
		}
		if o.MatchKey == to || (model.ArtistNameKey(o.To) == to && o.CreatedAt.After(cr.CreatedAt)) {
			return ErrUndoLaterFirst
		}
	}
	return nil
}

func (c *corrections) Undo(ctx context.Context, id string) (*Applied, error) {
	repo := c.ds.Correction(ctx)
	cr, err := repo.Get(id)
	if err != nil {
		return nil, err
	}
	if cr.Kind.IsAlbumCover() {
		return c.undoAlbumCover(ctx, cr)
	}
	if cr.Kind == model.CorrectionAlbum {
		return c.undoAlbum(ctx, cr)
	}
	if err := c.checkUndoOrder(ctx, cr); err != nil {
		return nil, err
	}
	folderIDs, err := repo.FolderIDs(id)
	if err != nil {
		return nil, err
	}
	targets, err := refresh.FolderTargets(ctx, c.ds, folderIDs)
	if err != nil {
		return nil, err
	}
	// Artist renames and merges are the only kinds so far: undoing one restores, and refreshes,
	// the old name
	res := &Applied{Correction: *cr, ArtistID: metadata.ArtistID(cr.From), Done: true}
	// Folders still holding the old name too: a rename whose own rescan is still running, or
	// failed, has not reached its Tracks yet
	notYetRenamed, err := c.refresher.ScanTargets(ctx, model.KindArtistArtwork, res.ArtistID)
	if err != nil {
		return nil, err
	}
	for _, t := range notYetRenamed {
		if !slices.Contains(targets, t) {
			targets = append(targets, t)
		}
	}
	if len(targets) == 0 {
		log.Info(ctx, "Corrections: deleting a Correction that applies to nothing", "kind", cr.Kind, "from", cr.From, "to", cr.To)
		return res, repo.Delete(id)
	}
	if err := repo.SetUndoing(id); err != nil {
		return nil, err
	}
	log.Info(ctx, "Corrections: undoing", "kind", cr.Kind, "from", cr.From, "to", cr.To, "folders", len(targets))
	res.Done = c.rescan(ctx, targets, refresh.Item{Kind: model.KindArtistArtwork, ID: res.ArtistID}, func(ctx context.Context) error {
		return c.ds.Correction(ctx).Delete(id)
	})
	return res, nil
}

// rescan rescans and refreshes the targets, then runs after (if the rescan worked), in the
// background: a large Library can take minutes, longer than the request that asked for it. It
// reports whether that was all done within Options.Wait.
func (c *corrections) rescan(ctx context.Context, targets []model.ScanTarget, item refresh.Item, after func(context.Context) error) bool {
	bg := context.WithoutCancel(ctx)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		if err := c.refresher.ScanThenRefresh(bg, targets, item); err != nil {
			log.Error(bg, "Corrections: rescanning the affected folders failed", "folders", len(targets), err)
			return
		}
		if after != nil {
			if err := after(bg); err != nil {
				log.Error(bg, "Corrections: finishing after the rescan failed", err)
			}
		}
	}()
	select {
	case <-finished:
		return true
	case <-time.After(c.opts.Wait):
		return false
	}
}
