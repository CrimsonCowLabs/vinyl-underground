package artwork

import (
	"context"
	"errors"
	"io"
	"net/url"
	"sync"

	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/core/agents"
	"github.com/navidrome/navidrome/core/musicbrainz"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// coverArtArchiveCandidate is the CoverArtPriority token for the Cover Art Archive. It is a token of
// its own, not an agent: it is an album-cover-only source keyed by MusicBrainz release and release
// group IDs, which the agents' name-based image interface cannot carry.
const coverArtArchiveCandidate = consts.CoverArtArchivePriority

// frontCoverSource is the part of the Cover Art Archive client the album chain uses.
type frontCoverSource interface {
	FrontCover(ctx context.Context, releaseID, releaseGroupID string) (musicbrainz.Cover, error)
}

// coverArtArchive is shared, so its rate limit holds across every resolver, and built on first use,
// so importing the package starts nothing. Tests swap it.
var coverArtArchive = sync.OnceValue(func() frontCoverSource {
	return musicbrainz.NewCoverArt(musicbrainz.CoverArtOptions{})
})

// fetchCoverArtArchive asks the archive for the album's front cover: the exact release's, else its
// release group's. The error is non-nil only for a transient failure worth retrying.
func fetchCoverArtArchive(ctx context.Context, gate gateFunc, al model.Album) (io.ReadCloser, error) {
	if al.MbzAlbumID == "" && al.MbzReleaseGroupID == "" {
		traceFrom(ctx).add(TraceStep{Candidate: ExternalPrefix + coverArtArchiveCandidate, Outcome: OutcomeSkipped,
			Detail: "album has no MusicBrainz ID"})
		return nil, nil
	}
	reader, path, err := gate(coverArtArchiveCandidate, func() (io.ReadCloser, string, error) {
		cover, err := coverArtArchive().FrontCover(ctx, al.MbzAlbumID, al.MbzReleaseGroupID)
		if err != nil {
			return nil, "", agentError(err)
		}
		u, err := url.Parse(cover.URL)
		if err != nil {
			return nil, "", agents.ErrNotFound
		}
		return fromURLAs(ctx, u, musicbrainz.CoverArtUserAgent())
	})
	recordAgent(ctx, coverArtArchiveCandidate, reader, path, err)
	if reader != nil {
		return reader, nil
	}
	if isTransientExternal(err) {
		log.Debug(ctx, "Artwork: Cover Art Archive lookup failed", "album", al.Name, "mbzAlbumId", al.MbzAlbumID, err)
		return nil, err
	}
	return nil, nil
}

// agentError translates the archive's answers into the agents' vocabulary, which the breaker and
// the retry policy understand.
func agentError(err error) error {
	if errors.Is(err, musicbrainz.ErrNoCover) {
		return agents.ErrNotFound
	}
	if busy, ok := errors.AsType[*musicbrainz.BusyError](err); ok {
		return &agents.RetryLaterError{RetryIn: busy.RetryIn}
	}
	return err
}
