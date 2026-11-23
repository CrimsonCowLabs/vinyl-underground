// Package acoustid identifies a File by its audio: Chromaprint's fpcalc fingerprints it, and
// AcoustID's web service (https://acoustid.org/webservice) names the MusicBrainz recordings with
// that fingerprint. It needs an application key, which the owner registers at acoustid.org.
package acoustid

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/navidrome/navidrome/core/musicbrainz"
)

const (
	// DefaultURL is AcoustID's lookup.
	DefaultURL = "https://api.acoustid.org/v2/lookup"
	// Interval keeps to AcoustID's limit of three requests a second.
	Interval = 340 * time.Millisecond
	// fpLength is how many seconds of audio fpcalc reads (its default): only the start of a File
	// is read, which keeps a slow share's reads short.
	fpLength = 120
	maxBody  = 2 << 20
)

// ErrNoFpcalc is a host without Chromaprint's fpcalc.
var ErrNoFpcalc = errors.New("fpcalc (Chromaprint) isn't installed")

// Fingerprint is what fpcalc makes of a File.
type Fingerprint struct {
	Duration    float64 `json:"duration"`
	Fingerprint string  `json:"fingerprint"`
}

// Recording is a MusicBrainz recording AcoustID knows the fingerprint for.
type Recording struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Artists []struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		JoinPhrase string `json:"joinphrase"`
	} `json:"artists"`
	ReleaseGroups []struct {
		ID             string   `json:"id"`
		Title          string   `json:"title"`
		Type           string   `json:"type"`
		SecondaryTypes []string `json:"secondarytypes"`
	} `json:"releasegroups"`
}

// Artist is the recording's artist credit as printed: "Band & Friend".
func (r Recording) Artist() string {
	var b strings.Builder
	for _, a := range r.Artists {
		b.WriteString(a.Name + a.JoinPhrase)
	}
	return strings.TrimSpace(b.String())
}

// Result is one fingerprint AcoustID matched, with its score (0 to 1) and recordings.
type Result struct {
	Score      float64     `json:"score"`
	Recordings []Recording `json:"recordings"`
}

// Options set up a Client. Key is required.
type Options struct {
	Key string
	// URL is the lookup's URL. Defaults to DefaultURL.
	URL  string
	HTTP *http.Client
	// Limiter spaces the requests. Defaults to one at Interval.
	Limiter *musicbrainz.Limiter
	// Store keeps each answer, by File, so a run started again doesn't fingerprint it again.
	Store musicbrainz.Store
	// Fpcalc runs fpcalc on a File. Defaults to running the fpcalc on the PATH.
	Fpcalc func(ctx context.Context, path string) ([]byte, error)
}

// Client fingerprints Files and looks them up.
type Client struct {
	opts Options
}

func New(opts Options) *Client {
	opts.URL = cmp.Or(opts.URL, DefaultURL)
	opts.HTTP = cmp.Or(opts.HTTP, &http.Client{Timeout: 30 * time.Second})
	opts.Limiter = cmp.Or(opts.Limiter, musicbrainz.NewLimiter(Interval, nil))
	if opts.Fpcalc == nil {
		opts.Fpcalc = runFpcalc
	}
	return &Client{opts: opts}
}

// Available reports whether fpcalc is installed.
func Available() bool {
	_, err := exec.LookPath("fpcalc")
	return err == nil
}

func runFpcalc(ctx context.Context, path string) ([]byte, error) {
	bin, err := exec.LookPath("fpcalc")
	if err != nil {
		return nil, ErrNoFpcalc
	}
	out, err := exec.CommandContext(ctx, bin, "-json", "-length", strconv.Itoa(fpLength), path).Output() //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("fingerprinting %s: %w", path, err)
	}
	return out, nil
}

// Lookup fingerprints the File at path and returns AcoustID's matches, best first. version
// tells this File apart from an earlier one at the same path (its size and modification time),
// for the Store.
func (c *Client) Lookup(ctx context.Context, path, version string) ([]Result, error) {
	key := "acoustid:" + path + "@" + version
	body, ok := []byte(nil), false
	if c.opts.Store != nil {
		body, ok = c.opts.Store.Get(key)
	}
	fresh := !ok
	if fresh {
		out, err := c.opts.Fpcalc(ctx, path)
		if err != nil {
			return nil, err
		}
		var fp Fingerprint
		if err := json.Unmarshal(out, &fp); err != nil {
			return nil, fmt.Errorf("reading fpcalc's fingerprint of %s: %w", path, err)
		}
		if fp.Fingerprint == "" {
			return nil, fmt.Errorf("fpcalc made no fingerprint of %s", path)
		}
		if body, err = c.fetch(ctx, fp); err != nil {
			return nil, err
		}
	}
	var res struct {
		Status string `json:"status"`
		Error  struct {
			Message string `json:"message"`
		} `json:"error"`
		Results []Result `json:"results"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("reading AcoustID's answer: %w", err)
	}
	if res.Status != "ok" {
		return nil, fmt.Errorf("AcoustID: %s", cmp.Or(res.Error.Message, res.Status))
	}
	if fresh && c.opts.Store != nil {
		c.opts.Store.Set(key, body)
	}
	return res.Results, nil
}

func (c *Client) fetch(ctx context.Context, fp Fingerprint) ([]byte, error) {
	if err := c.opts.Limiter.Wait(ctx); err != nil {
		return nil, err
	}
	form := url.Values{
		"client":      {c.opts.Key},
		"duration":    {strconv.Itoa(int(fp.Duration))},
		"fingerprint": {fp.Fingerprint},
		"meta":        {"recordings releasegroups compress"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.opts.URL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", musicbrainz.UserAgent())
	resp, err := c.opts.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("asking AcoustID: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("reading AcoustID's answer: %w", err)
	}
	// AcoustID answers errors (a bad key...) as JSON with a 400, which Lookup reports.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadRequest {
		return nil, fmt.Errorf("AcoustID answered %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	return body, nil
}
