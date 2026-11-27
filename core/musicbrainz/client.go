// Package musicbrainz is a small client for MusicBrainz's web service (https://musicbrainz.org/doc/MusicBrainz_API),
// as used for tag proposals (#34). It keeps to MusicBrainz's rules for clients: at most one request
// a second from the whole app (all Clients share one Limiter by default), a User-Agent naming the
// app, its version and its repo, and backing off when the server answers 503. Answers are cached
// for a day.
package musicbrainz

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/utils/cache"
)

const (
	// DefaultBaseURL is MusicBrainz's web service.
	DefaultBaseURL = "https://musicbrainz.org/ws/2/"
	// RepoURL is the app's repo, named in the User-Agent so MusicBrainz knows who is asking.
	RepoURL = "https://github.com/CrimsonCowLabs/vinyl-underground"
	// Interval is the time between two requests: MusicBrainz allows one a second on average, and
	// the extra tenth keeps the app clear of its limit.
	Interval = 1100 * time.Millisecond

	cacheTTL    = 24 * time.Hour
	cacheSize   = 2000
	maxBody     = 4 << 20
	maxRetries  = 4
	backoffBase = 2 * time.Second
	maxBackoff  = time.Minute
	// timeout is how long one request may take. MusicBrainz, when overloaded, can take 20-30s
	// to answer a release lookup with its recordings, so this leaves room for that.
	timeout = 60 * time.Second
)

// shared is the Limiter every Client uses unless given its own, so the app as a whole keeps to
// MusicBrainz's limit however many lookups run at once.
var shared = NewLimiter(Interval, nil)

// ErrBusy is returned when MusicBrainz still answers 503 after every retry, or doesn't answer
// a request in time (it is slow, not unreachable, when overloaded).
var ErrBusy = errors.New("MusicBrainz is busy; try again later")

// ErrNotFound is returned when MusicBrainz has nothing at the request's URL (404), e.g. a release
// merged away.
var ErrNotFound = errors.New("MusicBrainz has no such entity")

// UserAgent names the app, its version and its repo, as MusicBrainz asks, with no personal
// contact details.
func UserAgent() string {
	return fmt.Sprintf("VinylUnderground/%s ( %s )", appVersion(), RepoURL)
}

// appVersion is the app's version without its build details, "dev" when unknown.
func appVersion() string {
	if f := strings.Fields(consts.Version); len(f) > 0 {
		return f[0]
	}
	return "dev"
}

// Options set up a Client. Every field is optional.
type Options struct {
	// BaseURL is the web service's root, ending in "/". Defaults to DefaultBaseURL.
	BaseURL string
	// HTTP sends the requests. Defaults to a client with a 60s timeout.
	HTTP *http.Client
	// Limiter spaces the requests. Defaults to the one all Clients share.
	Limiter *Limiter
	// Store, if set, keeps every answer for good, so a long job that is stopped and run again
	// doesn't ask MusicBrainz again (the in-memory cache only lasts a day, and a run).
	Store Store
}

// Store keeps answers by request URL.
type Store interface {
	Get(key string) ([]byte, bool)
	Set(key string, body []byte)
}

// Client asks MusicBrainz's web service. It is safe for concurrent use.
type Client struct {
	base    string
	http    *http.Client
	limiter *Limiter
	store   Store
	cache   cache.SimpleCache[string, []byte]
}

func New(opts Options) *Client {
	return &Client{
		base:    cmp.Or(opts.BaseURL, DefaultBaseURL),
		http:    cmp.Or(opts.HTTP, &http.Client{Timeout: timeout}),
		limiter: cmp.Or(opts.Limiter, shared),
		store:   opts.Store,
		cache:   cache.NewSimpleCache[string, []byte](cache.Options{SizeLimit: cacheSize, DefaultTTL: cacheTTL}),
	}
}

// SearchRecordings runs a recording search (a Lucene query, see
// https://musicbrainz.org/doc/MusicBrainz_API/Search) and returns up to 25 recordings, best first,
// each with the releases MusicBrainz lists for it.
func (c *Client) SearchRecordings(ctx context.Context, query string) ([]Recording, error) {
	var res struct {
		Recordings []Recording `json:"recordings"`
	}
	err := c.get(ctx, "recording", url.Values{"query": {query}, "limit": {"25"}}, &res)
	return res.Recordings, err
}

// ReleaseGroupReleases browses the releases (editions) of a release group: up to 100, with their
// media and whether the Cover Art Archive holds a front cover for each.
func (c *Client) ReleaseGroupReleases(ctx context.Context, releaseGroupID string) ([]Release, error) {
	var res struct {
		Releases []Release `json:"releases"`
	}
	params := url.Values{"release-group": {releaseGroupID}, "inc": {"media"}, "limit": {"100"}}
	err := c.get(ctx, "release", params, &res)
	return res.Releases, err
}

// SearchReleases searches releases (editions of an album) by album artist and title, and returns
// up to 25, best first, each with its release group, country, date and media. An empty artist
// searches by title alone.
func (c *Client) SearchReleases(ctx context.Context, artist, title string) ([]Release, error) {
	query := `release:"` + luceneEscape(strings.TrimSpace(title)) + `"`
	if artist = strings.TrimSpace(artist); artist != "" {
		query += ` AND artist:"` + luceneEscape(artist) + `"`
	}
	return c.searchReleases(ctx, query)
}

// SearchReleasesByBarcode searches releases by their barcode (its digits), as SearchReleases
// does, but by any album artist: the barcode alone names the release.
func (c *Client) SearchReleasesByBarcode(ctx context.Context, barcode string) ([]Release, error) {
	return c.searchReleases(ctx, `barcode:"`+luceneEscape(strings.TrimSpace(barcode))+`"`)
}

// SearchReleasesByCatalogNumber searches releases by their label's catalog number, as
// SearchReleases does, but by any album artist: the number all but names the release.
func (c *Client) SearchReleasesByCatalogNumber(ctx context.Context, catno string) ([]Release, error) {
	return c.searchReleases(ctx, `catno:"`+luceneEscape(strings.TrimSpace(catno))+`"`)
}

// searchReleases runs a release search (a Lucene query) and returns up to 25 releases, best first.
func (c *Client) searchReleases(ctx context.Context, query string) ([]Release, error) {
	var res struct {
		Releases []Release `json:"releases"`
	}
	err := c.get(ctx, "release", url.Values{"query": {query}, "limit": {"25"}}, &res)
	return res.Releases, err
}

// Release looks up one release with its media, their tracks and recordings, the artists
// credited, its release group, and the genres of both.
func (c *Client) Release(ctx context.Context, id string) (*Release, error) {
	var res Release
	params := url.Values{"inc": {"recordings artist-credits release-groups genres"}}
	if err := c.get(ctx, "release/"+url.PathEscape(id), params, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// luceneEscape escapes a phrase for a quoted Lucene term.
func luceneEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}

func (c *Client) get(ctx context.Context, path string, params url.Values, into any) error {
	params.Set("fmt", "json")
	u := c.base + path + "?" + params.Encode()
	body, err := c.cache.GetWithLoader(u, func(u string) ([]byte, time.Duration, error) {
		if c.store != nil {
			if b, ok := c.store.Get(u); ok {
				return b, cacheTTL, nil
			}
		}
		b, err := c.fetch(ctx, u)
		if err == nil && c.store != nil {
			c.store.Set(u, b)
		}
		return b, cacheTTL, err
	})
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("reading MusicBrainz's answer: %w", err)
	}
	return nil
}

// fetch sends one GET, in its turn, backing off and trying again while the server answers 503
// (or 429). Every Client sharing the Limiter backs off with it.
func (c *Client) fetch(ctx context.Context, u string) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", UserAgent())
		req.Header.Set("Accept", "application/json")
		resp, err := c.http.Do(req)
		if err != nil {
			// The client's own timeout, not the caller giving up: MusicBrainz is too slow right now.
			if ne, ok := errors.AsType[net.Error](err); ok && ne.Timeout() && ctx.Err() == nil {
				log.Warn(ctx, "MusicBrainz didn't answer in time", "url", u, err)
				return nil, ErrBusy
			}
			return nil, fmt.Errorf("asking MusicBrainz: %w", err)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
		_ = resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusTooManyRequests:
			if attempt == maxRetries {
				return nil, ErrBusy
			}
			wait := retryAfter(resp.Header.Get("Retry-After"), backoffBase<<attempt)
			log.Debug(ctx, "MusicBrainz is busy; backing off", "wait", wait, "attempt", attempt+1)
			c.limiter.Backoff(wait)
		case resp.StatusCode == http.StatusNotFound:
			return nil, ErrNotFound
		case resp.StatusCode != http.StatusOK:
			return nil, fmt.Errorf("MusicBrainz answered %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
		case err != nil:
			return nil, fmt.Errorf("reading MusicBrainz's answer: %w", err)
		default:
			return body, nil
		}
	}
}

// retryAfter is the server's Retry-After (in seconds), if it gave one, else def, at most maxBackoff.
func retryAfter(header string, def time.Duration) time.Duration {
	if s, err := strconv.Atoi(strings.TrimSpace(header)); err == nil && s > 0 {
		def = time.Duration(s) * time.Second
	}
	return min(def, maxBackoff)
}
