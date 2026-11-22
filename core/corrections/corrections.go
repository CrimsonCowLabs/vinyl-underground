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
	// would merge them, which is not supported yet.
	ErrArtistExists = fmt.Errorf("%w: an Artist with that name already exists", model.ErrAlreadyExists)
	// ErrRenamedAway is returned when renaming an Artist to a name a Correction renames away:
	// undoing that Correction is the way to get it back.
	ErrRenamedAway = fmt.Errorf("%w: a Correction renames that name away; undo it instead", model.ErrValidation)
)

// Applied is the outcome of saving or undoing a Correction.
type Applied struct {
	Correction model.Correction `json:"correction"`
	// ArtistID is the ID the Artist has once its folders are rescanned.
	ArtistID string `json:"artistId,omitempty"`
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
	// replacing an earlier choice, and refreshes the album's artwork. The Correction is keyed by
	// the album's folder (see model.AlbumFolderKey). The image must already be in the
	// chosen-cover store (see artwork.ChosenCoverStore).
	ChooseAlbumCover(ctx context.Context, albumID string, cover AlbumCover) (*Applied, error)
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
	case ar.Name == consts.UnknownArtist || ar.Name == consts.VariousArtists:
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
	if _, err := c.ds.Correction(ctx).GetByMatchKey(model.CorrectionArtistRename, model.ArtistNameKey(name)); err == nil {
		return nil, ErrRenamedAway
	} else if !errors.Is(err, model.ErrNotFound) {
		return nil, err
	}

	targets, err := c.refresher.ScanTargets(ctx, model.KindArtistArtwork, ar.ID)
	if err != nil {
		return nil, err
	}
	cr := model.Correction{Kind: model.CorrectionArtistRename, MatchKey: model.ArtistNameKey(ar.Name), From: ar.Name, To: name}
	if err := c.ds.Correction(ctx).Add(&cr); err != nil {
		return nil, err
	}
	log.Info(ctx, "Corrections: renaming Artist", "from", ar.Name, "to", name, "folders", len(targets))
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
		return repo.Add(&cr)
	})
	if err != nil {
		return nil, err
	}
	log.Info(ctx, "Corrections: choosing an album cover", "album", al.Name, "source", cover.Source, "hash", cover.Hash)
	// A cover is no tag: nothing to rescan, only the album's artwork to resolve again. The choice
	// is saved either way, and wins whenever the album is next resolved.
	if err := artwork.Refresh(ctx, c.ds, model.KindAlbumArtwork, al.ID); err != nil {
		log.Warn(ctx, "Corrections: could not refresh the album's cover", "album", al.Name, err)
	}
	return &Applied{Correction: cr, Done: true}, nil
}

// albumLabel names an album for the Corrections page: "Band – Record".
func albumLabel(al model.Album) string {
	if al.AlbumArtist == "" {
		return al.Name
	}
	return al.AlbumArtist + " – " + al.Name
}

// undoAlbumCover deletes a cover choice and refreshes the albums in its folder, which go back to
// the default cover order.
func (c *corrections) undoAlbumCover(ctx context.Context, cr *model.Correction) (*Applied, error) {
	if err := c.ds.Correction(ctx).Delete(cr.ID); err != nil {
		return nil, err
	}
	albums, err := c.ds.Album(ctx).GetAll(model.QueryOptions{
		Filters: squirrel.Expr("exists (select 1 from json_each(album.folder_ids) af where af.value = ?)", cr.MatchKey),
	})
	if err != nil {
		return nil, err
	}
	log.Info(ctx, "Corrections: undoing an album cover choice", "album", cr.From, "source", cr.To, "albums", len(albums))
	for _, al := range albums {
		if err := artwork.Refresh(ctx, c.ds, model.KindAlbumArtwork, al.ID); err != nil {
			log.Warn(ctx, "Corrections: could not refresh the album's cover", "album", al.Name, err)
		}
	}
	return &Applied{Correction: *cr, Done: true}, nil
}

func (c *corrections) Undo(ctx context.Context, id string) (*Applied, error) {
	repo := c.ds.Correction(ctx)
	cr, err := repo.Get(id)
	if err != nil {
		return nil, err
	}
	if cr.Kind == model.CorrectionAlbumCover {
		return c.undoAlbumCover(ctx, cr)
	}
	folderIDs, err := repo.FolderIDs(id)
	if err != nil {
		return nil, err
	}
	targets, err := refresh.FolderTargets(ctx, c.ds, folderIDs)
	if err != nil {
		return nil, err
	}
	// Artist renames are the only kind so far: undoing one restores, and refreshes, the old name
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
