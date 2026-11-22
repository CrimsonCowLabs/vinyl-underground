package artwork

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/core/musicbrainz"
	"github.com/navidrome/navidrome/model"
)

// ErrExternalServicesOff is returned when a lookup is not made because EnableExternalServices is off.
var ErrExternalServicesOff = errors.New("external services are turned off")

// FetchPickedCover fetches the Cover Art Archive front cover of a MusicBrainz release picked for an
// album (the release's own, else its release group's) and keeps it in the ChosenCoverStore, so the
// album, its disc art and its Tracks' own art can all show it without a request (see PickSource).
// musicbrainz.ErrNoCover means the archive has none, and ErrExternalServicesOff that
// EnableExternalServices is off, so nothing was asked. It goes through the shared archive client, so
// its rate limit holds, and the download through the guarded client that refuses private addresses.
func FetchPickedCover(ctx context.Context, releaseID, releaseGroupID string) (StoredCover, error) {
	if !conf.Server.EnableExternalServices {
		return StoredCover{}, ErrExternalServicesOff
	}
	cover, err := coverArtArchive().FrontCover(ctx, releaseID, releaseGroupID)
	if err != nil {
		return StoredCover{}, err
	}
	u, err := url.Parse(cover.URL)
	if err != nil {
		return StoredCover{}, fmt.Errorf("the archive named no usable image: %w", err)
	}
	r, _, err := fromURLAs(ctx, u, musicbrainz.CoverArtUserAgent())
	if err != nil {
		return StoredCover{}, err
	}
	defer r.Close()
	limit := maxImageBytes()
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return StoredCover{}, err
	}
	if int64(len(data)) > limit {
		return StoredCover{}, fmt.Errorf("%w: the image exceeds the size cap %d", model.ErrValidation, limit)
	}
	return StoreChosenCover(data)
}
