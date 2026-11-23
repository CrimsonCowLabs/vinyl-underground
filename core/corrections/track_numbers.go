package corrections

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/core/refresh"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// TrackNumber is a Track's disc and track number as the admin corrects them.
type TrackNumber struct {
	ID          string `json:"id"`
	DiscNumber  int    `json:"discNumber"`
	TrackNumber int    `json:"trackNumber"`
}

// The highest numbers accepted (also in ui/src/album/trackNumbers.js)
const (
	maxDiscNumber  = 999
	maxTrackNumber = 9999
)

func (c *corrections) CorrectTrackNumbers(ctx context.Context, albumID string, tracks []TrackNumber) (*Applied, error) {
	if len(tracks) == 0 {
		return nil, fmt.Errorf("%w: no Tracks", model.ErrValidation)
	}
	al, err := c.ds.Album(ctx).Get(albumID)
	if err != nil {
		return nil, err
	}
	key := model.AlbumFolderKey(*al)
	if key == "" {
		return nil, fmt.Errorf("%w: the album has no folder", model.ErrValidation)
	}
	ids := make([]string, 0, len(tracks))
	for _, t := range tracks {
		ids = append(ids, t.ID)
	}
	mfs, err := c.ds.MediaFile(ctx).GetAll(model.QueryOptions{Filters: squirrel.Eq{
		"media_file.id": ids, "media_file.album_id": al.ID, "media_file.missing": false}})
	if err != nil {
		return nil, err
	}
	byID := make(map[string]model.MediaFile, len(mfs))
	for _, mf := range mfs {
		byID[mf.ID] = mf
	}

	// What an earlier Correction renumbered stays renumbered, with the numbers as tagged it found
	old, err := model.AlbumCorrection(c.ds.Correction(ctx), model.CorrectionTrackNumbers, *al)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return nil, err
	}
	var fixes model.TrackNumberFixes
	if old != nil {
		fixes = append(fixes, old.TrackNumbers...)
	}
	changed, seen := false, map[string]bool{}
	for _, t := range tracks {
		mf, ok := byID[t.ID]
		switch {
		case !ok:
			return nil, fmt.Errorf("%w: %s is no Track of the album", model.ErrValidation, t.ID)
		case seen[t.ID]:
			return nil, fmt.Errorf("%w: %s is listed twice", model.ErrValidation, t.ID)
		}
		seen[t.ID] = true
		if t.DiscNumber == mf.DiscNumber && t.TrackNumber == mf.TrackNumber {
			continue
		}
		changed = true
		at := fixIndex(fixes, mf)
		fix := model.TrackNumberFix{LibraryID: mf.LibraryID, Path: mf.Path, Title: mf.Title,
			FromDisc: mf.DiscNumber, FromTrack: mf.TrackNumber}
		if at >= 0 {
			fix.FromDisc, fix.FromTrack = fixes[at].FromDisc, fixes[at].FromTrack
		}
		// A number may stay as tagged (a Track with no disc number keeps none), else it must be one
		if (t.DiscNumber != fix.FromDisc && (t.DiscNumber < 1 || t.DiscNumber > maxDiscNumber)) ||
			(t.TrackNumber != fix.FromTrack && (t.TrackNumber < 1 || t.TrackNumber > maxTrackNumber)) {
			return nil, fmt.Errorf("%w: %d-%d is no track number", model.ErrValidation, t.DiscNumber, t.TrackNumber)
		}
		fix.Disc, fix.Track = t.DiscNumber, t.TrackNumber
		switch {
		case fix.Disc == fix.FromDisc && fix.Track == fix.FromTrack && at >= 0:
			// Set back to the numbers as tagged: nothing left to correct
			fixes = append(fixes[:at], fixes[at+1:]...)
		case at >= 0:
			fixes[at] = fix
		default:
			fixes = append(fixes, fix)
		}
	}
	if !changed {
		return nil, fmt.Errorf("%w: nothing changed", model.ErrValidation)
	}
	if err := c.checkSharedFolder(ctx, *al); err != nil {
		return nil, err
	}

	if len(fixes) == 0 {
		// Every Track is back to its tagged numbers: that is undoing the earlier Correction, which
		// stays (marked undoing) until the rescan has read them again
		return c.undoAlbum(ctx, old)
	}

	targets, err := c.refresher.ScanTargets(ctx, model.KindAlbumArtwork, al.ID)
	if err != nil {
		return nil, err
	}
	cr := model.Correction{Kind: model.CorrectionTrackNumbers, MatchKey: key, From: albumLabel(*al),
		To: trackNumbersLabel(fixes), TrackNumbers: fixes}
	err = c.ds.WithTx(func(tx model.DataStore) error {
		repo := tx.Correction(ctx)
		if old != nil {
			if err := repo.Delete(old.ID); err != nil {
				return err
			}
		}
		return repo.Add(&cr)
	})
	if err != nil {
		return nil, err
	}
	log.Info(ctx, "Corrections: correcting track numbers", "album", cr.From, "tracks", len(fixes), "folders", len(targets))
	res := &Applied{Correction: cr, AlbumID: al.ID}
	res.Done = c.rescan(ctx, targets, refresh.Item{Kind: model.KindAlbumArtwork, ID: al.ID}, nil)
	return res, nil
}

// fixIndex returns where the Track's File is in the fixes, or -1.
func fixIndex(fixes model.TrackNumberFixes, mf model.MediaFile) int {
	for i, f := range fixes {
		if f.LibraryID == mf.LibraryID && f.Path == mf.Path {
			return i
		}
	}
	return -1
}

// trackNumbersLabel lists the renumbered Tracks for the Corrections page, as numbered now:
// "1-1 One, 1-2 Two" (just "2 Two" for a Track with no disc number).
func trackNumbersLabel(fixes model.TrackNumberFixes) string {
	sorted := slices.SortedStableFunc(slices.Values(fixes), func(a, b model.TrackNumberFix) int {
		return cmp.Or(cmp.Compare(a.Disc, b.Disc), cmp.Compare(a.Track, b.Track))
	})
	labels := make([]string, 0, len(fixes))
	for _, f := range sorted {
		n := fmt.Sprint(f.Track)
		if f.Disc > 0 {
			n = fmt.Sprintf("%d-%d", f.Disc, f.Track)
		}
		labels = append(labels, n+" "+f.Title)
	}
	return strings.Join(labels, ", ")
}
