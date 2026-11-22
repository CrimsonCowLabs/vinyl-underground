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
	all, err := c.ds.Correction(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	for _, o := range all {
		if o.Kind == model.CorrectionArtistRename && o.MatchKey == model.ArtistNameKey(name) {
			return nil, ErrRenamedAway
		}
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

func (c *corrections) Undo(ctx context.Context, id string) (*Applied, error) {
	repo := c.ds.Correction(ctx)
	cr, err := repo.Get(id)
	if err != nil {
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
	res := &Applied{Correction: *cr, ArtistID: metadata.ArtistID(cr.From), Done: true}
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
