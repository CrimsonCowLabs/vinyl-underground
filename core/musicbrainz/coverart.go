package musicbrainz

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/utils/cache"
)

// DefaultCoverArtBaseURL is the Cover Art Archive (https://musicbrainz.org/doc/Cover_Art_Archive/API),
// MusicBrainz's store of release artwork, keyed by MusicBrainz release and release group IDs.
const DefaultCoverArtBaseURL = "https://coverartarchive.org/"

// coverArtRetries is how often a busy answer is retried. Fewer than the web service gets: an artwork
// lookup holds a worker slot while it waits, and the artwork worker retries the album later anyway.
const coverArtRetries = 2

// ErrNoCover is returned when the archive holds no front cover for what was asked.
var ErrNoCover = errors.New("no cover in the Cover Art Archive")

// BusyError is returned when the archive still answers 503 or 429 after every retry. RetryIn is
// the wait it asked for, zero when it named none.
type BusyError struct{ RetryIn time.Duration }

func (e *BusyError) Error() string {
	return fmt.Sprintf("the Cover Art Archive is busy; try again in %s", e.RetryIn)
}

// CoverArtUserAgent names the app and its version, and nothing else. Image downloads from the
// archive send it too.
func CoverArtUserAgent() string {
	return "VinylUnderground/" + appVersion()
}

// Cover is one image the archive holds for a release.
type Cover struct {
	// ReleaseID is the release the image belongs to. For a release group lookup it is the release
	// the archive chose to represent the group.
	ReleaseID string
	// FromReleaseGroup is set when the cover came from a release group lookup, not the exact release.
	FromReleaseGroup bool
	// Front is set for the image the archive marks as the release's front cover.
	Front bool
	// URL is the image at a size fit for album art: the 1200px thumbnail, else the 500px one, else
	// the original upload (which can be a very large scan).
	URL string
	// ThumbnailURL is a small preview (250px) for listing covers, falling back to URL.
	ThumbnailURL string
}

// CoverArtOptions set up a CoverArt client. Every field is optional.
type CoverArtOptions struct {
	// BaseURL is the archive's root, ending in "/". Defaults to DefaultCoverArtBaseURL.
	BaseURL string
	// HTTP sends the requests. Defaults to a client with a 15s timeout.
	HTTP *http.Client
	// Limiter spaces the requests. Defaults to one shared by every CoverArt client, at
	// MusicBrainz's one request a second.
	Limiter *Limiter
}

// coverArtShared spaces every CoverArt client's requests, apart from the web service's own Limiter,
// so artwork lookups and tag proposals do not hold each other up.
var coverArtShared = NewLimiter(Interval, nil)

// CoverArt asks the Cover Art Archive for release artwork. It is safe for concurrent use.
type CoverArt struct {
	base    string
	http    *http.Client
	limiter *Limiter
	cache   cache.SimpleCache[string, []Cover]
}

func NewCoverArt(opts CoverArtOptions) *CoverArt {
	return &CoverArt{
		base:    cmp.Or(opts.BaseURL, DefaultCoverArtBaseURL),
		http:    cmp.Or(opts.HTTP, &http.Client{Timeout: 15 * time.Second}),
		limiter: cmp.Or(opts.Limiter, coverArtShared),
		cache:   cache.NewSimpleCache[string, []Cover](cache.Options{SizeLimit: cacheSize, DefaultTTL: cacheTTL}),
	}
}

// FrontCover is the front cover of the release, else the front cover of its release group. Either ID
// may be empty. ErrNoCover means neither has one; any other error leaves that open, so the release
// group is only asked once the release has definitely answered "none".
func (c *CoverArt) FrontCover(ctx context.Context, releaseID, releaseGroupID string) (Cover, error) {
	if releaseID != "" {
		cover, err := front(c.ReleaseCovers(ctx, releaseID))
		if !errors.Is(err, ErrNoCover) {
			return cover, err
		}
	}
	if releaseGroupID != "" {
		return front(c.ReleaseGroupCovers(ctx, releaseGroupID))
	}
	return Cover{}, ErrNoCover
}

// ReleaseCovers lists every image of the release, its front cover first. ErrNoCover means it has none.
func (c *CoverArt) ReleaseCovers(ctx context.Context, releaseID string) ([]Cover, error) {
	return c.covers(ctx, "release/"+releaseID, false)
}

// ReleaseGroupCovers lists the images of the release the archive picked to represent the group,
// its front cover first. ErrNoCover means it has none.
func (c *CoverArt) ReleaseGroupCovers(ctx context.Context, releaseGroupID string) ([]Cover, error) {
	return c.covers(ctx, "release-group/"+releaseGroupID, true)
}

// ReleaseFront names the front cover of a release the caller already knows has one (see
// Release.CoverArtArchive), through the archive's front-image redirects, without a request.
func (c *CoverArt) ReleaseFront(releaseID string) Cover {
	u := c.base + "release/" + url.PathEscape(releaseID) + "/front"
	return Cover{ReleaseID: releaseID, Front: true, URL: u + "-1200", ThumbnailURL: u + "-250"}
}

func front(covers []Cover, err error) (Cover, error) {
	if err != nil {
		return Cover{}, err
	}
	if len(covers) == 0 || !covers[0].Front {
		return Cover{}, ErrNoCover
	}
	return covers[0], nil
}

// coverArtListing is the archive's answer for a release or release group.
type coverArtListing struct {
	// Release is the release's MusicBrainz URL, ending in its ID.
	Release string `json:"release"`
	Images  []struct {
		Front      bool              `json:"front"`
		Image      string            `json:"image"`
		Thumbnails map[string]string `json:"thumbnails"`
	} `json:"images"`
}

func (c *CoverArt) covers(ctx context.Context, path string, group bool) ([]Cover, error) {
	u := c.base + path
	return c.cache.GetWithLoader(u, func(u string) ([]Cover, time.Duration, error) {
		body, err := c.fetch(ctx, u)
		if err != nil {
			return nil, 0, err
		}
		var listing coverArtListing
		if err := json.Unmarshal(body, &listing); err != nil {
			return nil, 0, fmt.Errorf("reading the Cover Art Archive's answer: %w", err)
		}
		covers := listing.covers(group)
		if len(covers) == 0 {
			return nil, 0, ErrNoCover
		}
		return covers, cacheTTL, nil
	})
}

func (l coverArtListing) covers(group bool) []Cover {
	releaseID := releaseIDFromURL(l.Release)
	covers := make([]Cover, 0, len(l.Images))
	for _, img := range l.Images {
		url := cmp.Or(img.Thumbnails["1200"], img.Thumbnails["large"], img.Thumbnails["500"], img.Image)
		if url == "" {
			continue
		}
		covers = append(covers, Cover{
			ReleaseID:        releaseID,
			FromReleaseGroup: group,
			Front:            img.Front,
			URL:              url,
			ThumbnailURL:     cmp.Or(img.Thumbnails["250"], img.Thumbnails["small"], url),
		})
	}
	slices.SortStableFunc(covers, func(a, b Cover) int {
		switch {
		case a.Front == b.Front:
			return 0
		case a.Front:
			return -1
		default:
			return 1
		}
	})
	return covers
}

// releaseIDFromURL takes the ID off the end of a MusicBrainz release URL.
func releaseIDFromURL(u string) string {
	if u == "" {
		return ""
	}
	return path.Base(strings.TrimRight(u, "/"))
}

// fetch sends one GET, in its turn. A 404 (nothing archived) or 400 (not an MBID) is ErrNoCover; a
// busy answer backs every client sharing the Limiter off, and is retried a few times.
func (c *CoverArt) fetch(ctx context.Context, u string) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", CoverArtUserAgent())
		req.Header.Set("Accept", "application/json")
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("asking the Cover Art Archive: %w", err)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
		_ = resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusBadRequest:
			return nil, ErrNoCover
		case resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusTooManyRequests:
			wait := retryAfter(resp.Header.Get("Retry-After"), backoffBase<<attempt)
			if attempt == coverArtRetries {
				return nil, &BusyError{RetryIn: wait}
			}
			log.Debug(ctx, "The Cover Art Archive is busy; backing off", "wait", wait, "attempt", attempt+1)
			c.limiter.Backoff(wait)
		case resp.StatusCode != http.StatusOK:
			return nil, fmt.Errorf("the Cover Art Archive answered %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
		case err != nil:
			return nil, fmt.Errorf("reading the Cover Art Archive's answer: %w", err)
		default:
			return body, nil
		}
	}
}
