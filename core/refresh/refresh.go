// Package refresh brings albums and Artists up to date with their files and external info: it
// scans only the folders holding them, then refreshes their artwork and external info.
package refresh

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/external"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/persistence"
	"github.com/navidrome/navidrome/scanner"
)

// Scanner is the part of the scanner a refresh needs.
type Scanner interface {
	ScanFolders(ctx context.Context, fullScan bool, targets []model.ScanTarget) ([]string, error)
}

// InfoRefresher re-fetches external info for one item (external.Provider is one).
type InfoRefresher interface {
	RefreshInfo(ctx context.Context, kind model.Kind, id string) error
}

// Item is one thing to refresh once its folders are scanned.
type Item struct {
	Kind model.Kind
	ID   string
}

type Refresher interface {
	// Refresh rescans the folders holding the item (see ScanTargets), then refreshes its artwork
	// and external info, as ScanThenRefresh does. It returns model.ErrNotFound for an unknown item.
	Refresh(ctx context.Context, kind model.Kind, id string) error
	// ScanTargets returns the folders holding the item: an album's folders, or the folders of all
	// the albums an Artist is album artist or Track artist on (the albums its page lists). Other
	// kinds have none.
	ScanTargets(ctx context.Context, kind model.Kind, id string) ([]model.ScanTarget, error)
	// ScanThenRefresh fully rescans only the given folders, so every File in them is read again
	// even if unchanged on disk, then refreshes the items' artwork and external info. An album or
	// Artist whose ID the scan changed (its name was changed) is replaced by the albums, or their
	// album artists, now in those folders.
	//
	// It waits for a scan already running (returning scanner.ErrAlreadyScanning if that outlasts
	// Options), and returns once its own scan is done and the artwork re-queued; external info is
	// fetched in the background. The library root is never scanned, and no targets means no
	// scan: a refresh never rescans a whole library.
	ScanThenRefresh(ctx context.Context, targets []model.ScanTarget, items ...Item) error
}

// Options control how long a refresh waits for a scan already running before giving up with
// scanner.ErrAlreadyScanning: ScanAttempts tries, ScanRetryDelay apart.
type Options struct {
	ScanRetryDelay time.Duration
	ScanAttempts   int
}

func New(ds model.DataStore, scanner Scanner, info InfoRefresher) Refresher {
	return NewWithOptions(ds, scanner, info, Options{})
}

func NewWithOptions(ds model.DataStore, scanner Scanner, info InfoRefresher, opts Options) Refresher {
	opts.ScanRetryDelay = cmp.Or(opts.ScanRetryDelay, 2*time.Second)
	opts.ScanAttempts = cmp.Or(opts.ScanAttempts, 30)
	return &refresher{ds: ds, scanner: scanner, info: info, opts: opts}
}

type refresher struct {
	ds      model.DataStore
	scanner Scanner
	info    InfoRefresher
	opts    Options
}

func (r *refresher) Refresh(ctx context.Context, kind model.Kind, id string) error {
	if _, err := artwork.ItemName(ctx, r.ds, kind, id); err != nil {
		return err
	}
	targets, err := r.ScanTargets(ctx, kind, id)
	if err != nil {
		return err
	}
	return r.ScanThenRefresh(ctx, targets, Item{Kind: kind, ID: id})
}

func (r *refresher) ScanThenRefresh(ctx context.Context, targets []model.ScanTarget, items ...Item) error {
	scanned := slices.DeleteFunc(slices.Clone(targets), isLibraryRoot)
	if len(scanned) < len(targets) {
		log.Warn(ctx, "Refresh: not rescanning the library root, as that would rescan the whole library")
	}
	if len(scanned) > 0 {
		if err := r.scan(ctx, scanned); err != nil {
			return fmt.Errorf("scanning folders: %w", err)
		}
	}
	items, err := r.current(ctx, scanned, items)
	if err != nil {
		return err
	}
	for _, it := range items {
		if err := artwork.Refresh(ctx, r.ds, it.Kind, it.ID); err != nil {
			return err
		}
	}
	// Detached: the lookups are slow, and the caller (often an HTTP request) returns before them.
	bg := context.WithoutCancel(ctx)
	go func() {
		for _, it := range items {
			if !external.HasInfo(it.Kind) {
				continue
			}
			if err := r.info.RefreshInfo(bg, it.Kind, it.ID); err != nil {
				log.Error(bg, "Error refreshing external info", "kind", it.Kind, "id", it.ID, err)
			}
		}
	}()
	return nil
}

// scan fully scans the targets, so every File is read again whatever its time, waiting for any
// other scan to finish first.
func (r *refresher) scan(ctx context.Context, targets []model.ScanTarget) error {
	var err error
	for attempt := range r.opts.ScanAttempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(r.opts.ScanRetryDelay):
			}
		}
		_, err = r.scanner.ScanFolders(ctx, true, targets)
		if !errors.Is(err, scanner.ErrAlreadyScanning) {
			return err
		}
	}
	return err
}

// current returns the items as they are after the scan. Album and Artist IDs come from names,
// so one the scan read a new name for is gone: it is replaced by the albums in the scanned
// folders, or by their album artists.
func (r *refresher) current(ctx context.Context, scanned []model.ScanTarget, items []Item) ([]Item, error) {
	var res []Item
	var albums model.Albums // in the scanned folders, loaded on first need
	for _, it := range items {
		_, err := artwork.ItemName(ctx, r.ds, it.Kind, it.ID)
		if err == nil || !errors.Is(err, model.ErrNotFound) ||
			(it.Kind != model.KindAlbumArtwork && it.Kind != model.KindArtistArtwork) {
			res = append(res, it)
			continue
		}
		if albums == nil {
			if albums, err = r.albumsIn(ctx, scanned); err != nil {
				return nil, err
			}
		}
		log.Debug(ctx, "Refresh: item renamed by the scan, refreshing what its folders now hold", "kind", it.Kind, "id", it.ID)
		for _, al := range albums {
			if it.Kind == model.KindAlbumArtwork {
				res = append(res, Item{Kind: model.KindAlbumArtwork, ID: al.ID})
				continue
			}
			for _, p := range al.Participants[model.RoleAlbumArtist] {
				res = append(res, Item{Kind: model.KindArtistArtwork, ID: p.ID})
			}
		}
	}
	slices.SortFunc(res, func(a, b Item) int {
		return cmp.Or(cmp.Compare(a.Kind.Prefix(), b.Kind.Prefix()), cmp.Compare(a.ID, b.ID))
	})
	return slices.Compact(res), nil
}

// albumsIn returns the albums with Tracks in the given folders.
func (r *refresher) albumsIn(ctx context.Context, targets []model.ScanTarget) (model.Albums, error) {
	if len(targets) == 0 {
		return model.Albums{}, nil
	}
	folderIDs := make([]string, 0, len(targets))
	for _, t := range targets {
		folderIDs = append(folderIDs, model.FolderID(model.Library{ID: t.LibraryID}, t.FolderPath))
	}
	return r.ds.Album(ctx).GetAll(model.QueryOptions{
		Filters: persistence.Exists("json_each(album.folder_ids) je", squirrel.Eq{"je.value": folderIDs}),
	})
}

// isLibraryRoot reports whether scanning the target would rescan its whole library: the scanner
// reads an empty path, or the root itself, as the entire library, subfolders included.
func isLibraryRoot(t model.ScanTarget) bool {
	return path.Clean("/"+t.FolderPath) == "/"
}

func (r *refresher) ScanTargets(ctx context.Context, kind model.Kind, id string) ([]model.ScanTarget, error) {
	var albums model.Albums
	switch kind {
	case model.KindAlbumArtwork:
		al, err := r.ds.Album(ctx).Get(id)
		if err != nil {
			return nil, err
		}
		albums = model.Albums{*al}
	case model.KindArtistArtwork:
		var err error
		albums, err = r.ds.Album(ctx).GetAll(model.QueryOptions{
			Filters: persistence.ParticipantIDFilter("album", id, model.RoleAlbumArtist, model.RoleArtist),
		})
		if err != nil {
			return nil, err
		}
	}
	var folderIDs []string
	for _, al := range albums {
		folderIDs = append(folderIDs, al.FolderIDs...)
	}
	if len(folderIDs) == 0 {
		return nil, nil
	}
	folders, err := r.ds.Folder(ctx).GetAll(model.QueryOptions{
		Filters: squirrel.Eq{"folder.id": slices.Compact(slices.Sorted(slices.Values(folderIDs))), "missing": false},
	})
	if err != nil {
		return nil, err
	}
	targets := make([]model.ScanTarget, 0, len(folders))
	for _, f := range folders {
		targets = append(targets, model.ScanTarget{LibraryID: f.LibraryID, FolderPath: path.Join(f.Path, f.Name)})
	}
	return targets, nil
}
