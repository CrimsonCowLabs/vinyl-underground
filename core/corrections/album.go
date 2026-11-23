package corrections

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/musicbrainz"
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
	fix.MbzAlbumID = strings.ToLower(strings.TrimSpace(fix.MbzAlbumID))
	fix.MbzReleaseGroupID = strings.ToLower(strings.TrimSpace(fix.MbzReleaseGroupID))
	// A release and its release group are picked together, so the tagged release group never
	// stays next to another release
	if !isMbzID(fix.MbzAlbumID) || !isMbzID(fix.MbzReleaseGroupID) {
		return nil, fmt.Errorf("%w: a release and its release group need both their MusicBrainz IDs", model.ErrValidation)
	}
	al, err := c.ds.Album(ctx).Get(albumID)
	if err != nil {
		return nil, err
	}
	key := model.AlbumFolderKey(*al)
	if key == "" {
		return nil, fmt.Errorf("%w: the album has no folder", model.ErrValidation)
	}

	// Only the release is saved: the album's title, album artist and year go to its Files. What
	// an earlier album Correction (not yet migrated, #165) corrects stays as it is.
	label := albumInfoLabel(al.AlbumArtist, al.Name, albumYear(*al))
	cr := model.Correction{Kind: model.CorrectionAlbum, MatchKey: key, From: label, To: label}
	old, err := model.AlbumCorrection(c.ds.Correction(ctx), model.CorrectionAlbum, *al)
	if err == nil {
		cr.From, cr.To, cr.AlbumName, cr.AlbumArtist, cr.Year = old.From, old.To, old.AlbumName, old.AlbumArtist, old.Year
		cr.MbzAlbumID, cr.MbzReleaseGroupID = old.MbzAlbumID, old.MbzReleaseGroupID
		cr.ImageHash, cr.ImageMime = old.ImageHash, old.ImageMime
		if i := strings.Index(cr.To, mbzLabel); i >= 0 {
			cr.To = cr.To[:i]
		}
	} else if !errors.Is(err, model.ErrNotFound) {
		return nil, err
	}
	// Picking the release the album has already still asks for its cover, if it has none (the
	// archive failed, or had none then).
	changed := fix.MbzAlbumID != cr.MbzAlbumID || fix.MbzReleaseGroupID != cr.MbzReleaseGroupID
	if changed {
		cr.MbzAlbumID, cr.MbzReleaseGroupID = fix.MbzAlbumID, fix.MbzReleaseGroupID
		cr.ImageHash, cr.ImageMime = "", ""
	}
	cr.To += mbzLabel + pickedReleaseLabel(fix)
	if old != nil && !changed {
		cr.To = old.To
	}
	fetchCover := cr.ImageHash == "" && c.opts.PickedCover != nil
	if !changed && !fetchCover {
		return nil, ErrNothingToWrite
	}
	if err := c.checkSharedFolder(ctx, *al); err != nil {
		return nil, err
	}
	// Whatever the archive answers, the pick is saved
	var pickCover PickCover
	if fetchCover {
		pickCover = c.fetchPickedCover(ctx, &cr)
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
	log.Info(ctx, "Corrections: picking a MusicBrainz release for an album", "album", cr.From, "release", cr.MbzAlbumID)
	// No tag changes: only the covers, which follow the pick's.
	if err := c.refreshFolderCovers(ctx, key); err != nil {
		log.Warn(ctx, "Corrections: could not refresh the covers", "album", al.Name, err)
	}
	return &Applied{Correction: cr, AlbumID: al.ID, PickCover: pickCover, Done: true}, nil
}

// checkSharedFolder returns ErrSharedFolder if the album's folders hold another album's Tracks.
func (c *corrections) checkSharedFolder(ctx context.Context, al model.Album) error {
	shared, err := c.ds.MediaFile(ctx).CountAll(model.QueryOptions{Filters: squirrel.And{
		squirrel.Eq{"folder_id": al.FolderIDs, "missing": false}, squirrel.NotEq{"album_id": al.ID},
	}})
	if err != nil {
		return err
	}
	if shared > 0 {
		return ErrSharedFolder
	}
	return nil
}

// albumYear is the year clients show for the album: its original year when it has one.
func albumYear(al model.Album) int {
	return cmp.Or(al.MaxOriginalYear, al.MaxYear)
}

// PickCover is what the Cover Art Archive answered for a picked release (see Applied.PickCover).
type PickCover string

const (
	// PickCoverFound: the release's front cover is kept with the pick, and is the album's cover.
	PickCoverFound PickCover = "found"
	// PickCoverNone: the archive has no front cover for it; the album keeps its usual cover order.
	PickCoverNone PickCover = "none"
	// PickCoverFailed: the archive could not be asked; picking the release again tries it again.
	PickCoverFailed PickCover = "failed"
	// PickCoverDisabled: EnableExternalServices is off, so the archive was not asked.
	PickCoverDisabled PickCover = "disabled"
)

// pickedCoverTimeout is how long saving waits for the archive before saving the pick without it.
const pickedCoverTimeout = 20 * time.Second

// fetchPickedCover asks the archive for the picked release's front cover and keeps it with the
// Correction. A failure is only logged: the pick is saved without it.
func (c *corrections) fetchPickedCover(ctx context.Context, cr *model.Correction) PickCover {
	ctx, cancel := context.WithTimeout(ctx, pickedCoverTimeout)
	defer cancel()
	stored, err := c.opts.PickedCover(ctx, cr.MbzAlbumID, cr.MbzReleaseGroupID)
	switch {
	case errors.Is(err, artwork.ErrExternalServicesOff):
		return PickCoverDisabled
	case errors.Is(err, musicbrainz.ErrNoCover):
		log.Info(ctx, "Corrections: the Cover Art Archive has no cover for the picked release", "release", cr.MbzAlbumID)
		return PickCoverNone
	case err != nil:
		log.Warn(ctx, "Corrections: could not fetch the picked release's cover", "release", cr.MbzAlbumID, err)
		return PickCoverFailed
	}
	cr.ImageHash, cr.ImageMime = stored.Hash, stored.Mime
	return PickCoverFound
}

// isMbzID says whether s is a MusicBrainz ID (a UUID).
func isMbzID(s string) bool {
	return mbzIDPattern.MatchString(s)
}

var mbzIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// pickedReleaseLabel names the picked release for the Corrections page, by the name the client gave it
// (folded onto one line and capped), else by its ID.
func pickedReleaseLabel(fix AlbumFix) string {
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
