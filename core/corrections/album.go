package corrections

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/core/refresh"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// ErrSharedFolder is returned when correcting an album whose folders hold another album's Tracks
// too: an album Correction is keyed by folder, so it would correct that album as well.
var ErrSharedFolder = fmt.Errorf("%w: another album shares this album's folder", model.ErrValidation)

// AlbumFix is an album's title, album artist and year as the admin corrects them, and the
// MusicBrainz release picked for it with Find on MusicBrainz, if any.
type AlbumFix struct {
	Name        string `json:"name"`
	AlbumArtist string `json:"albumArtist"`
	Year        int    `json:"year"`
	// MbzAlbumID and MbzReleaseGroupID are the picked release's MusicBrainz IDs: empty keeps the
	// album's (as tagged, or as an earlier Correction set them). MbzRelease names the release
	// for the Corrections page: "Record (GB, CD, 1999)".
	MbzAlbumID        string `json:"mbzAlbumId"`
	MbzReleaseGroupID string `json:"mbzReleaseGroupId"`
	MbzRelease        string `json:"mbzRelease"`
}

// mbzLabel joins the picked release to an album Correction's To label.
const mbzLabel = " · MusicBrainz: "

// maxReleaseLabel caps how much of the release's name the label keeps.
const maxReleaseLabel = 200

func (c *corrections) CorrectAlbum(ctx context.Context, albumID string, fix AlbumFix) (*Applied, error) {
	fix.Name = strings.TrimSpace(fix.Name)
	fix.AlbumArtist = strings.Join(strings.Fields(fix.AlbumArtist), " ")
	fix.MbzAlbumID = strings.ToLower(strings.TrimSpace(fix.MbzAlbumID))
	fix.MbzReleaseGroupID = strings.ToLower(strings.TrimSpace(fix.MbzReleaseGroupID))
	switch {
	case fix.Name == "":
		return nil, fmt.Errorf("%w: the title is empty", model.ErrValidation)
	case fix.AlbumArtist == "":
		return nil, fmt.Errorf("%w: the album artist is empty", model.ErrValidation)
	case fix.Year != 0 && (fix.Year < 1000 || fix.Year > 9999):
		return nil, fmt.Errorf("%w: %d is no year", model.ErrValidation, fix.Year)
	case fix.MbzAlbumID != "" && !isMbzID(fix.MbzAlbumID),
		fix.MbzReleaseGroupID != "" && !isMbzID(fix.MbzReleaseGroupID):
		return nil, fmt.Errorf("%w: not a MusicBrainz ID", model.ErrValidation)
	case fix.MbzAlbumID == "" && fix.MbzReleaseGroupID != "":
		return nil, fmt.Errorf("%w: a release group without its release", model.ErrValidation)
	}
	al, err := c.ds.Album(ctx).Get(albumID)
	if err != nil {
		return nil, err
	}
	key := model.AlbumFolderKey(*al)
	if key == "" {
		return nil, fmt.Errorf("%w: the album has no folder", model.ErrValidation)
	}

	// Only what differs from the album now is corrected: an album artist left alone still follows
	// later Artist renames. What an earlier Correction corrected stays corrected.
	cr := model.Correction{Kind: model.CorrectionAlbum, MatchKey: key,
		From: albumInfoLabel(al.AlbumArtist, al.Name, albumYear(*al)),
		To:   albumInfoLabel(fix.AlbumArtist, fix.Name, cmp.Or(fix.Year, albumYear(*al)))}
	old, err := model.AlbumCorrection(c.ds.Correction(ctx), model.CorrectionAlbum, *al)
	if err == nil {
		cr.From, cr.AlbumName, cr.AlbumArtist, cr.Year = old.From, old.AlbumName, old.AlbumArtist, old.Year
		cr.MbzAlbumID, cr.MbzReleaseGroupID = old.MbzAlbumID, old.MbzReleaseGroupID
	} else if !errors.Is(err, model.ErrNotFound) {
		return nil, err
	}
	changed := false
	if fix.Name != al.Name {
		cr.AlbumName, changed = fix.Name, true
	}
	if fix.AlbumArtist != al.AlbumArtist {
		cr.AlbumArtist, changed = fix.AlbumArtist, true
		if _, err := c.artistCorrection(ctx, model.ArtistNameKey(fix.AlbumArtist)); err == nil {
			return nil, ErrRenamedAway
		} else if !errors.Is(err, model.ErrNotFound) {
			return nil, err
		}
	}
	if fix.Year != 0 && fix.Year != albumYear(*al) {
		cr.Year, changed = fix.Year, true
	}
	// A picked release replaces the album's release and release group together: the tagged
	// release group belongs to the tagged release
	if fix.MbzAlbumID != "" && (fix.MbzAlbumID != al.MbzAlbumID || fix.MbzReleaseGroupID != al.MbzReleaseGroupID) {
		cr.MbzAlbumID, cr.MbzReleaseGroupID, changed = fix.MbzAlbumID, fix.MbzReleaseGroupID, true
		cr.To += mbzLabel + releaseLabel(fix)
	} else if old != nil && cr.MbzAlbumID != "" {
		if i := strings.Index(old.To, mbzLabel); i >= 0 {
			cr.To += old.To[i:]
		}
	}
	if !changed {
		return nil, fmt.Errorf("%w: nothing changed", model.ErrValidation)
	}
	if shared, err := c.ds.MediaFile(ctx).CountAll(model.QueryOptions{Filters: squirrel.And{
		squirrel.Eq{"folder_id": al.FolderIDs, "missing": false}, squirrel.NotEq{"album_id": al.ID},
	}}); err != nil {
		return nil, err
	} else if shared > 0 {
		return nil, ErrSharedFolder
	}

	targets, err := c.refresher.ScanTargets(ctx, model.KindAlbumArtwork, al.ID)
	if err != nil {
		return nil, err
	}
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
	log.Info(ctx, "Corrections: correcting an album", "from", cr.From, "to", cr.To, "folders", len(targets))
	res := &Applied{Correction: cr, AlbumID: al.ID}
	res.Done = c.rescan(ctx, targets, refresh.Item{Kind: model.KindAlbumArtwork, ID: al.ID}, nil)
	if res.Done {
		res.AlbumID = c.albumIn(ctx, al.FolderIDs, al.ID)
	}
	return res, nil
}

// undoAlbum stops applying an album Correction, rescans the album's folders (those its Tracks are
// in, and those it was saved for, in case its own rescan has not reached them), then deletes it.
func (c *corrections) undoAlbum(ctx context.Context, cr *model.Correction) (*Applied, error) {
	repo := c.ds.Correction(ctx)
	folderIDs, err := repo.FolderIDs(cr.ID)
	if err != nil {
		return nil, err
	}
	albumFolders, err := repo.AlbumFolderIDs(cr.MatchKey)
	if err != nil {
		return nil, err
	}
	folderIDs = append(folderIDs, albumFolders...)
	targets, err := refresh.FolderTargets(ctx, c.ds, folderIDs)
	if err != nil {
		return nil, err
	}
	res := &Applied{Correction: *cr, Done: true}
	if len(targets) == 0 {
		log.Info(ctx, "Corrections: deleting an album Correction whose folders are gone", "from", cr.From, "to", cr.To)
		return res, repo.Delete(cr.ID)
	}
	if err := repo.SetUndoing(cr.ID); err != nil {
		return nil, err
	}
	res.AlbumID = c.albumIn(ctx, folderIDs, "")
	log.Info(ctx, "Corrections: undoing an album Correction", "from", cr.From, "to", cr.To, "folders", len(targets))
	res.Done = c.rescan(ctx, targets, refresh.Item{Kind: model.KindAlbumArtwork, ID: res.AlbumID}, func(ctx context.Context) error {
		return c.ds.Correction(ctx).Delete(cr.ID)
	})
	if res.Done {
		res.AlbumID = c.albumIn(ctx, folderIDs, res.AlbumID)
	}
	return res, nil
}

// albumIn returns the album of a present Track in the folders, or def if there is none: once an
// album Correction is applied (or undone) that is the album with its new ID.
func (c *corrections) albumIn(ctx context.Context, folderIDs []string, def string) string {
	mfs, err := c.ds.MediaFile(ctx).GetAll(model.QueryOptions{Max: 1,
		Filters: squirrel.Eq{"media_file.folder_id": folderIDs, "media_file.missing": false}})
	if err != nil || len(mfs) == 0 {
		if err != nil {
			log.Warn(ctx, "Corrections: could not find the album's new ID", err)
		}
		return def
	}
	return mfs[0].AlbumID
}

// albumYear is the year clients show for the album: its original year when it has one.
func albumYear(al model.Album) int {
	return cmp.Or(al.MaxOriginalYear, al.MaxYear)
}

// isMbzID says whether s is a MusicBrainz ID (a UUID).
func isMbzID(s string) bool {
	return mbzIDPattern.MatchString(s)
}

var mbzIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// releaseLabel names the picked release for the Corrections page, by the name the client gave it
// (folded onto one line and capped), else by its ID.
func releaseLabel(fix AlbumFix) string {
	label := strings.Join(strings.Fields(fix.MbzRelease), " ")
	if r := []rune(label); len(r) > maxReleaseLabel {
		label = string(r[:maxReleaseLabel]) + "…"
	}
	return cmp.Or(label, fix.MbzAlbumID)
}

// albumInfoLabel names an album for the Corrections page: "Band – Record (1999)".
func albumInfoLabel(albumArtist, name string, year int) string {
	label := albumLabel(model.Album{AlbumArtist: albumArtist, Name: name})
	if year > 0 {
		label += fmt.Sprintf(" (%d)", year)
	}
	return label
}
