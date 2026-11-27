package corrections

import (
	"cmp"
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/musicbrainz"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// AlbumFix is an album's title, album artist and year as the admin corrects them in the album
// Edit dialog.
type AlbumFix struct {
	Name        string `json:"name"`
	AlbumArtist string `json:"albumArtist"`
	Year        int    `json:"year"`
	// Composer are the composers written into every File of the album; none keeps each File's
	// own (an album's Tracks usually have composers of their own).
	Composer []string `json:"composer,omitempty"`
}

// mbzLabel joins the MusicBrainz release an album Correction picked (before #170 wrote picks into
// the Files) to its To label.
const mbzLabel = " · MusicBrainz: "

// albumYear is the year clients show for the album: its original year when it has one.
func albumYear(al model.Album) int {
	return cmp.Or(al.MaxOriginalYear, al.MaxYear)
}

// PickCover is what the Cover Art Archive answered for a picked release's front cover, when the
// admin asked for it to be written as the album's cover.jpg (see Plan.PickCover).
type PickCover string

const (
	// PickCoverFound: the job writes the release's front cover as the album's cover.jpg.
	PickCoverFound PickCover = "found"
	// PickCoverNone: the archive has no front cover for it; the album keeps its cover.
	PickCoverNone PickCover = "none"
	// PickCoverFailed: the archive could not be asked; saving the release again tries it again.
	PickCoverFailed PickCover = "failed"
	// PickCoverDisabled: EnableExternalServices is off, so the archive was not asked.
	PickCoverDisabled PickCover = "disabled"
	// PickCoverNoFolder: the album has no folder of its own for a cover.jpg (its disc folders
	// are in a folder other albums are in too, say), so the archive was not asked.
	PickCoverNoFolder PickCover = "noFolder"
)

// pickedCoverTimeout is how long saving waits for the archive before writing the release without
// its cover.
const pickedCoverTimeout = 20 * time.Second

// fetchPickedCover asks the archive for the picked release's front cover, and keeps it in the
// Correction's ImageHash and ImageMime. A failure is only logged: the release is written without
// it.
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
