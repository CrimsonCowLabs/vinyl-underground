package rip

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/navidrome/navidrome/log"
	"golang.org/x/sync/singleflight"
)

// ytDlpName is the binary's name inside Binary.Dir.
const ytDlpName = "yt-dlp"

// sumsName is the release asset listing the SHA-256 of every other asset.
const sumsName = "SHA2-256SUMS"

const (
	// latestKeptFor is how long Binary.Latest remembers the latest release's version. GitHub
	// allows only 60 lookups an hour without a login.
	latestKeptFor = time.Hour
	// failedLatestKeptFor is how long Binary.Latest remembers that the lookup failed, so a page
	// loaded again and again while GitHub is down doesn't wait on it each time.
	failedLatestKeptFor = 5 * time.Minute
	// latestTimeout bounds one lookup, which the Rips page waits on.
	latestTimeout = 10 * time.Second
)

// AssetFor returns the name of the standalone yt-dlp release asset that runs on the platform.
// musl picks the build for Alpine, which the Docker image uses.
func AssetFor(goos, goarch string, musl bool) (string, error) {
	if goos != "linux" {
		return "", fmt.Errorf("no standalone yt-dlp build for %s", goos)
	}
	name := "yt-dlp_linux"
	if musl {
		name = "yt-dlp_musllinux"
	}
	switch goarch {
	case "amd64":
		return name, nil
	case "arm64":
		return name + "_aarch64", nil
	default:
		return "", fmt.Errorf("no standalone yt-dlp build for linux/%s", goarch)
	}
}

// IsMusl reports whether this system's C library is musl (as on Alpine).
func IsMusl() bool {
	matches, _ := filepath.Glob("/lib/ld-musl-*")
	return len(matches) > 0
}

// Binary is the yt-dlp executable, downloaded into Dir the first time it is needed, because the
// container's image is read-only, and replaced there by Update.
type Binary struct {
	// Dir is where the binary is kept (/data/bin in the Docker image).
	Dir string
	// ReleaseURL is the folder the release assets and their SHA2-256SUMS are downloaded from.
	// When it redirects to a release's own folder, as GitHub's .../releases/latest/download does,
	// everything is fetched from that one release, and its tag is the version.
	ReleaseURL string
	// Asset is the release asset to download (see AssetFor).
	Asset  string
	Client *http.Client
	// TempDir is the TMPDIR for `yt-dlp --version` (see YtDlp.TempDir). It is kept apart from the
	// Rips' own, which each run of YtDlp empties.
	TempDir string

	mu sync.Mutex
	// version is the installed binary's version, as of its modification time modTime.
	version string
	modTime time.Time
	// lastVersion is version, readable without mu.
	lastVersion atomic.Value

	// latestMu guards seen, the latest release as last looked up (see Latest). It is apart from
	// mu so the lookup doesn't wait for an update, and it is never held across a lookup, so an
	// update doesn't wait for GitHub either.
	latestMu sync.Mutex
	seen     latestSeen
	// lookups makes the pages that find seen out of date at the same time share one lookup.
	lookups singleflight.Group
	// now is the clock (time.Now when nil), set in tests.
	now func() time.Time
}

// latestSeen is the latest release's version as of at, or the error looking it up gave.
type latestSeen struct {
	tag string
	err error
	at  time.Time
}

// fresh reports whether s is recent enough to use instead of looking the release up again.
func (s latestSeen) fresh(now time.Time) bool {
	if s.at.IsZero() {
		return false
	}
	keep := latestKeptFor
	if s.err != nil {
		keep = failedLatestKeptFor
	}
	return now.Sub(s.at) < keep
}

// UpdateResult is what Binary.Update did.
type UpdateResult struct {
	// Previous is the version that was installed before ("" for none), and Version the one
	// installed now.
	Previous string `json:"previous"`
	Version  string `json:"version"`
	// Updated is false when the installed version already was the latest.
	Updated bool `json:"updated"`
}

func (b *Binary) path() string { return filepath.Join(b.Dir, ytDlpName) }

// Path returns the binary's path, downloading it first if it isn't there yet. The download is
// checked against the release's SHA2-256SUMS before it is put in place.
func (b *Binary) Path(ctx context.Context) (string, error) {
	p := b.path()
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	if _, _, err := b.install(ctx, ""); err != nil {
		return "", err
	}
	return p, nil
}

// Version returns the installed binary's version, as `yt-dlp --version` prints it, or "" when
// yt-dlp isn't installed yet. While an update or the first download runs, it returns the version
// last seen rather than waiting.
func (b *Binary) Version(ctx context.Context) (string, error) {
	if !b.mu.TryLock() {
		v, _ := b.lastVersion.Load().(string)
		return v, nil
	}
	defer b.mu.Unlock()
	return b.installedVersion(ctx)
}

// Update installs the latest release, unless it is the version already installed. When anything
// goes wrong the binary that was there stays in place.
func (b *Binary) Update(ctx context.Context) (UpdateResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	prev, err := b.installedVersion(ctx)
	if err != nil {
		log.Warn(ctx, "The installed yt-dlp doesn't run; replacing it", err)
		prev = ""
	}
	version, updated, err := b.install(ctx, prev)
	if err != nil {
		return UpdateResult{Previous: prev, Version: prev}, err
	}
	return UpdateResult{Previous: prev, Version: version, Updated: updated}, nil
}

// Latest returns the latest release's version, or "" when ReleaseURL doesn't say (a release's own
// folder, as for a mirror) or there is no standalone build for this platform, which Update
// couldn't install. It looks it up at most once an hour, and an update tells it too. It always
// returns "" with an error.
//
// The lookup carries on when ctx ends (the page waiting on it was closed), so the next page
// gets its answer rather than a failure remembered for five minutes.
func (b *Binary) Latest(ctx context.Context) (string, error) {
	if b.Asset == "" {
		return "", nil
	}
	b.latestMu.Lock()
	seen := b.seen
	b.latestMu.Unlock()
	if seen.fresh(b.clock()) {
		return seen.tag, seen.err
	}
	lookup := b.lookups.DoChan("latest", func() (any, error) {
		at := b.clock()
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), latestTimeout)
		defer cancel()
		rel, err := b.latest(ctx)
		b.setLatest(latestSeen{tag: rel.tag, err: err, at: at})
		return rel.tag, err
	})
	select {
	case res := <-lookup:
		if res.Err != nil {
			return "", res.Err
		}
		return res.Val.(string), nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// setLatest keeps the latest release as an update or a lookup found it, unless something newer
// has been kept since (an update that finished while a slower lookup was waiting on GitHub).
func (b *Binary) setLatest(s latestSeen) {
	b.latestMu.Lock()
	defer b.latestMu.Unlock()
	if s.at.Before(b.seen.at) {
		return
	}
	b.seen = s
}

func (b *Binary) clock() time.Time {
	if b.now == nil {
		return time.Now()
	}
	return b.now()
}

func (b *Binary) installedVersion(ctx context.Context) (string, error) {
	info, err := os.Stat(b.path())
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if b.version != "" && info.ModTime().Equal(b.modTime) {
		return b.version, nil
	}
	v, err := b.runVersion(ctx, b.path())
	if err != nil {
		return "", err
	}
	b.setVersion(v, info.ModTime())
	return v, nil
}

func (b *Binary) setVersion(v string, modTime time.Time) {
	b.version, b.modTime = v, modTime
	b.lastVersion.Store(v)
}

// runVersion runs `yt-dlp --version` on the binary at p.
func (b *Binary) runVersion(ctx context.Context, p string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, p, "--version")
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if b.TempDir != "" {
		// A killed run leaves its unpacked copy behind; runs never overlap (b.mu is held).
		_ = os.RemoveAll(b.TempDir)
		if err := os.MkdirAll(b.TempDir, 0o755); err != nil {
			return "", err
		}
		cmd.Env = append(os.Environ(), "TMPDIR="+b.TempDir)
	}
	killProcessGroup(cmd)
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("yt-dlp doesn't run: %v", ytDlpError(stderr.String(), err))
	}
	v := lastLine(stdout.Bytes())
	if v == "" {
		return "", errors.New("yt-dlp doesn't run: it printed no version")
	}
	return v, nil
}

// install downloads the latest release into place and returns its version. It downloads nothing
// when that release is the current version (updated is false then).
func (b *Binary) install(ctx context.Context, current string) (version string, updated bool, err error) {
	if b.Asset == "" {
		return "", false, fmt.Errorf("no standalone yt-dlp build for this platform; put a yt-dlp binary at %s", b.path())
	}
	rel, err := b.latest(ctx)
	if err != nil {
		return "", false, err
	}
	b.setLatest(latestSeen{tag: rel.tag, at: b.clock()})
	if current != "" && rel.tag == current {
		return current, false, nil
	}
	sums := rel.sums
	if sums == nil {
		var buf bytes.Buffer
		if err := b.fetch(ctx, rel.folder+"/"+sumsName, &buf); err != nil {
			return "", false, err
		}
		sums = buf.Bytes()
	}
	want, err := sumFor(sums, b.Asset)
	if err != nil {
		return "", false, err
	}
	if current != "" && fileSum(b.path()) == want {
		// The release's tag isn't its version (or it has no tag), but its file is already here.
		return current, false, nil
	}

	if err := os.MkdirAll(b.Dir, 0o755); err != nil {
		return "", false, fmt.Errorf("creating %s: %w", b.Dir, err)
	}
	log.Info(ctx, "Downloading yt-dlp", "asset", b.Asset, "release", cmp.Or(rel.tag, rel.folder), "to", b.path())
	tmp, err := os.CreateTemp(b.Dir, "."+ytDlpName+"-*.part")
	if err != nil {
		return "", false, err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	h := sha256.New()
	err = b.fetch(ctx, rel.folder+"/"+b.Asset, io.MultiWriter(tmp, h))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", false, err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return "", false, fmt.Errorf("yt-dlp download %s failed its checksum: got %s, want %s", b.Asset, got, want)
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return "", false, err
	}
	version, err = b.runVersion(ctx, tmp.Name())
	if err != nil {
		return "", false, fmt.Errorf("the downloaded %s: %w", b.Asset, err)
	}
	if err := os.Rename(tmp.Name(), b.path()); err != nil {
		return "", false, err
	}
	if info, err := os.Stat(b.path()); err == nil {
		b.setVersion(version, info.ModTime())
	}
	log.Info(ctx, "yt-dlp installed", "version", version, "previous", current)
	return version, true, nil
}

// release is the one release the binary and its SHA2-256SUMS are both fetched from.
type release struct {
	folder string
	// tag is the release's tag, which is also its version; "" when ReleaseURL doesn't say.
	tag string
	// sums is the release's SHA2-256SUMS, when finding the release already fetched it.
	sums []byte
}

// latest pins ReleaseURL to one release. GitHub's .../releases/latest/download/<asset> redirects
// to .../releases/download/<tag>/<asset>, so the redirect names the tag, and fetching both files
// from that tag means a release published in between can't mix two releases' files.
func (b *Binary) latest(ctx context.Context) (release, error) {
	base := strings.TrimSuffix(b.ReleaseURL, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/"+sumsName, nil)
	if err != nil {
		return release{}, err
	}
	noFollow := *b.client()
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := noFollow.Do(req)
	if err != nil {
		return release{}, fmt.Errorf("looking up the latest yt-dlp: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
		// Not a redirect, so ReleaseURL is a release's own folder (a pinned release or a mirror).
		sums, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return release{}, fmt.Errorf("downloading %s: %w", req.URL, err)
		}
		return release{folder: base, sums: sums}, nil
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		loc, err := resp.Location()
		if err != nil {
			return release{}, fmt.Errorf("looking up the latest yt-dlp: %w", err)
		}
		if path.Base(loc.Path) != sumsName {
			// Redirected somewhere that doesn't name a release: fetch from ReleaseURL as it is.
			return release{folder: base}, nil
		}
		folder := *loc
		folder.Path = path.Dir(loc.Path)
		folder.RawPath, folder.RawQuery, folder.Fragment = "", "", ""
		tag, _ := url.PathUnescape(path.Base(folder.Path))
		return release{folder: folder.String(), tag: tag}, nil
	default:
		return release{}, fmt.Errorf("looking up the latest yt-dlp at %s: %s", req.URL, resp.Status)
	}
}

// NewerVersion reports whether the latest release is newer than the installed version (true when
// nothing is installed yet, false when the latest isn't known). yt-dlp's versions are dates
// (2026.11.01), sometimes with a further number (2026.11.01.1, or a nightly's 2026.11.01.232857),
// compared number by number, so a nightly newer than the latest stable release isn't behind.
// When either can't be read that way, any difference counts as newer.
func NewerVersion(latest, installed string) bool {
	if latest == "" || latest == installed {
		return false
	}
	l, lok := versionParts(latest)
	i, iok := versionParts(installed)
	if !lok || !iok {
		return true
	}
	for n := range min(len(l), len(i)) {
		if l[n] != i[n] {
			return l[n] > i[n]
		}
	}
	return len(l) > len(i)
}

// versionParts splits a dotted version into its numbers, or reports false when it isn't one.
func versionParts(v string) ([]uint64, bool) {
	fields := strings.Split(v, ".")
	parts := make([]uint64, len(fields))
	for n, f := range fields {
		p, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			return nil, false
		}
		parts[n] = p
	}
	return parts, true
}

// sumFor reads the asset's SHA-256 from a release's SHA2-256SUMS.
func sumFor(sums []byte, asset string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == asset {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("the yt-dlp release's SHA2-256SUMS has no entry for %s", asset)
}

// fileSum returns the SHA-256 of the file at p, or "" if it can't be read.
func fileSum(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (b *Binary) client() *http.Client {
	if b.Client == nil {
		return http.DefaultClient
	}
	return b.Client
}

func (b *Binary) fetch(ctx context.Context, url string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := b.client().Do(req)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading %s: %w", url, errors.New(resp.Status))
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		return fmt.Errorf("downloading %s: %w", url, err)
	}
	return nil
}
