package rip

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/navidrome/navidrome/log"
)

// ytDlpName is the binary's name inside Binary.Dir.
const ytDlpName = "yt-dlp"

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
// container's image is read-only.
type Binary struct {
	// Dir is where the binary is kept (/data/bin in the Docker image).
	Dir string
	// ReleaseURL is the folder the release assets and their SHA2-256SUMS are downloaded from.
	ReleaseURL string
	// Asset is the release asset to download (see AssetFor).
	Asset  string
	Client *http.Client
}

// Path returns the binary's path, downloading it first if it isn't there yet. The download is
// checked against the release's SHA2-256SUMS before it is put in place.
func (b *Binary) Path(ctx context.Context) (string, error) {
	path := filepath.Join(b.Dir, ytDlpName)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if b.Asset == "" {
		return "", fmt.Errorf("no standalone yt-dlp build for this platform; put a yt-dlp binary at %s", path)
	}
	if err := os.MkdirAll(b.Dir, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", b.Dir, err)
	}
	log.Info(ctx, "Downloading yt-dlp", "asset", b.Asset, "from", b.ReleaseURL, "to", path)

	want, err := b.expectedSum(ctx)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(b.Dir, "."+ytDlpName+"-*.part")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	h := sha256.New()
	err = b.fetch(ctx, b.Asset, io.MultiWriter(tmp, h))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return "", fmt.Errorf("yt-dlp download %s failed its checksum: got %s, want %s", b.Asset, got, want)
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", err
	}
	return path, nil
}

// expectedSum reads the asset's SHA-256 from the release's SHA2-256SUMS.
func (b *Binary) expectedSum(ctx context.Context) (string, error) {
	var sums strings.Builder
	if err := b.fetch(ctx, "SHA2-256SUMS", &sums); err != nil {
		return "", err
	}
	sc := bufio.NewScanner(strings.NewReader(sums.String()))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == b.Asset {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("the yt-dlp release's SHA2-256SUMS has no entry for %s", b.Asset)
}

func (b *Binary) fetch(ctx context.Context, name string, w io.Writer) error {
	url := strings.TrimSuffix(b.ReleaseURL, "/") + "/" + name
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := b.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
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
