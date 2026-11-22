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

	"github.com/navidrome/navidrome/consts"
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
	// ErrUndoLaterFirst is returned when undoing an artist rename that a later Correction builds on:
	// one renaming its new name onward (A→B, then B→C), or, for a rename, merging another Artist
	// into its new name. Undoing that one first keeps the Artists' stars and play counts apart.
	ErrUndoLaterFirst = fmt.Errorf("%w: a later Correction builds on this one; undo it first", model.ErrValidation)
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
	// MergeArtist saves a Correction renaming the Artist to the name of the Artist intoID, merging
	// them: their stars, ratings and play counts are joined (see
	// model.CorrectionRepository.MergeArtistAnnotations). It rescans like RenameArtist.
	MergeArtist(ctx context.Context, artistID, intoID string) (*Applied, error)
	// Undo stops applying the Correction, rescans the folders of the Tracks it applied to, and
	// then deletes it. If the rescan fails, it stays marked as undoing, and Undo can be retried.
	Undo(ctx context.Context, id string) (*Applied, error)
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
// cr's new name onward, or (cr being a plain rename, whose undo moves all of the new name's stars
// back) it merges another Artist into cr's new name.
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
		if o.ID == cr.ID || o.Undoing || !o.Kind.IsArtistRename() {
			continue
		}
		if o.MatchKey == to || (cr.Kind == model.CorrectionArtistRename && model.ArtistNameKey(o.To) == to) {
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
