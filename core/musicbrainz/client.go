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
)

// shared is the Limiter every Client uses unless given its own, so the app as a whole keeps to
// MusicBrainz's limit however many lookups run at once.
var shared = NewLimiter(Interval, nil)

// ErrBusy is returned when MusicBrainz still answers 503 after every retry.
var ErrBusy = errors.New("MusicBrainz is busy; try again later")

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
	// HTTP sends the requests. Defaults to a client with a 30s timeout.
	HTTP *http.Client
	// Limiter spaces the requests. Defaults to the one all Clients share.
	Limiter *Limiter
}

// Client asks MusicBrainz's web service. It is safe for concurrent use.
type Client struct {
	base    string
	http    *http.Client
	limiter *Limiter
	cache   cache.SimpleCache[string, []byte]
}

func New(opts Options) *Client {
	return &Client{
		base:    cmp.Or(opts.BaseURL, DefaultBaseURL),
		http:    cmp.Or(opts.HTTP, &http.Client{Timeout: 30 * time.Second}),
		limiter: cmp.Or(opts.Limiter, shared),
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

func (c *Client) get(ctx context.Context, path string, params url.Values, into any) error {
	params.Set("fmt", "json")
	u := c.base + path + "?" + params.Encode()
	body, err := c.cache.GetWithLoader(u, func(u string) ([]byte, time.Duration, error) {
		b, err := c.fetch(ctx, u)
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
