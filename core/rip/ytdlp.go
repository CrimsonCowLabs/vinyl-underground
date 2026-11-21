package rip

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Info is what a Rip needs to know about a web page before fetching it.
type Info struct {
	ID     string
	Title  string
	Artist string
	// Source is the site's display name (e.g. "YouTube"), and SourceURL the page's canonical URL.
	Source    string
	SourceURL string
	// IsPlaylist is true when the URL is a playlist (or album, or channel) rather than one video.
	IsPlaylist bool
	// Count is how many items the page has: 1 for a video, yt-dlp's total for a playlist (which
	// can be more than len(Entries), as only the first MaxItems+1 are listed).
	Count int
	// Entries are a playlist's items, in order, as listed by --flat-playlist: no item has been
	// looked at on its own yet, so the title and artist are only a first guess.
	Entries []PlaylistEntry
}

// PlaylistEntry is one item of a playlist.
type PlaylistEntry struct {
	URL    string
	Title  string
	Artist string
}

// albumPrefix is how YouTube Music titles an album's playlist ("Album - <name>").
const albumPrefix = "Album - "

// BinaryPath finds the yt-dlp executable, downloading it if needed. *Binary implements it.
type BinaryPath interface {
	Path(ctx context.Context) (string, error)
}

// YtDlp runs yt-dlp. The URL is always passed as a single argument after "--", never through a
// shell, so it can't be read as an option, and yt-dlp never reads the user's config files.
type YtDlp struct {
	Binary BinaryPath
	// FFmpegPath is the ffmpeg yt-dlp uses to remux; empty leaves yt-dlp to find it in the PATH.
	FFmpegPath string
	// CacheDir is yt-dlp's cache; empty turns its cache off.
	CacheDir string
	// TempDir is yt-dlp's TMPDIR. The standalone build unpacks itself there on every run, so it
	// must allow running programs, which the container's /tmp (a noexec tmpfs) does not.
	TempDir string
	// DenoPath is the JavaScript runtime yt-dlp needs to read YouTube pages fully (deno is in the
	// Docker image). Empty leaves yt-dlp to look for one itself.
	DenoPath string
	// DenoDir is deno's cache folder (DENO_DIR), as its default under $HOME isn't writable in
	// the read-only container.
	DenoDir string

	// mu runs one yt-dlp at a time: each run clears TempDir first, and a Rip being queued (which
	// probes the page) can overlap the worker's download.
	mu sync.Mutex
}

// audioFormat keeps the best Opus stream, else the best AAC one, from audio-only formats first.
// Together with --audio-format best this only ever remuxes, so the audio is never re-encoded.
const audioFormat = "bestaudio[acodec^=opus]/bestaudio[acodec^=mp4a]/best[acodec^=opus]/best[acodec^=mp4a]"

func (y *YtDlp) commonArgs() []string {
	args := []string{"--ignore-config", "--no-playlist", "--no-color"}
	if y.CacheDir != "" {
		args = append(args, "--cache-dir", y.CacheDir)
	} else {
		args = append(args, "--no-cache-dir")
	}
	if y.DenoPath != "" {
		args = append(args, "--js-runtimes", "deno:"+y.DenoPath)
	}
	return args
}

// Probe reads the page's metadata without downloading anything.
// A playlist's items are only listed (--flat-playlist), and only the first MaxItems+1 of them,
// which is enough to tell a playlist that is too big to rip.
func (y *YtDlp) Probe(ctx context.Context, url string) (*Info, error) {
	args := append(y.commonArgs(), "--flat-playlist", "--playlist-items", fmt.Sprintf("1:%d", MaxItems+1),
		"--dump-single-json", "--", url)
	out, err := y.run(ctx, args)
	if err != nil {
		return nil, err
	}
	var meta struct {
		Type         string   `json:"_type"`
		ID           string   `json:"id"`
		Title        string   `json:"title"`
		Track        string   `json:"track"`
		Artists      []string `json:"artists"`
		Artist       string   `json:"artist"`
		Creator      string   `json:"creator"`
		Uploader     string   `json:"uploader"`
		Channel      string   `json:"channel"`
		ExtractorKey string   `json:"extractor_key"`
		WebpageURL   string   `json:"webpage_url"`
		// Only in a playlist:
		PlaylistCount int `json:"playlist_count"`
		Entries       []struct {
			URL        string `json:"url"`
			WebpageURL string `json:"webpage_url"`
			Title      string `json:"title"`
			Uploader   string `json:"uploader"`
			Channel    string `json:"channel"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(out, &meta); err != nil {
		return nil, fmt.Errorf("reading yt-dlp's metadata: %w", err)
	}
	artist := meta.Artist
	if len(meta.Artists) > 0 {
		artist = strings.Join(meta.Artists, ", ")
	}
	info := &Info{
		ID:         meta.ID,
		Title:      firstNonEmpty(meta.Track, meta.Title),
		Artist:     firstNonEmpty(artist, meta.Creator, meta.Uploader, meta.Channel),
		Source:     SourceName(meta.ExtractorKey),
		SourceURL:  firstNonEmpty(meta.WebpageURL, url),
		IsPlaylist: meta.Type == "playlist" || meta.Type == "multi_video",
		Count:      1,
	}
	if info.IsPlaylist {
		info.Title = strings.TrimPrefix(info.Title, albumPrefix)
		info.Count = max(meta.PlaylistCount, len(meta.Entries))
		for _, e := range meta.Entries {
			info.Entries = append(info.Entries, PlaylistEntry{
				URL:   firstNonEmpty(e.WebpageURL, e.URL),
				Title: e.Title,
				// YouTube Music lists an album's Tracks as uploaded by "<Artist> - Topic".
				Artist: strings.TrimSuffix(firstNonEmpty(e.Uploader, e.Channel), " - Topic"),
			})
		}
	}
	return info, nil
}

// Download fetches the page's audio into dir and returns the File's path, ending in .opus or .m4a.
func (y *YtDlp) Download(ctx context.Context, url, dir string) (string, error) {
	args := y.commonArgs()
	args = append(args,
		"--no-progress", "--no-mtime", "--no-part",
		"-f", audioFormat,
		"--extract-audio", "--audio-format", "best",
		"-P", dir, "-o", "%(id)s.%(ext)s",
		"--print", "after_move:filepath", "--no-simulate",
	)
	if y.FFmpegPath != "" {
		args = append(args, "--ffmpeg-location", y.FFmpegPath)
	}
	args = append(args, "--", url)
	out, err := y.run(ctx, args)
	if err != nil {
		if strings.Contains(err.Error(), "Requested format is not available") {
			return "", fmt.Errorf("the page has no Opus or AAC audio to keep without re-encoding: %w", err)
		}
		return "", err
	}
	path := lastLine(out)
	if path == "" {
		return "", errors.New("yt-dlp finished without saying where it saved the File")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	if ext := strings.ToLower(filepath.Ext(path)); ext != ".opus" && ext != ".m4a" {
		return "", fmt.Errorf("yt-dlp saved a %s File; only .opus and .m4a are kept", ext)
	}
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("yt-dlp's File is missing: %w", err)
	}
	return path, nil
}

func (y *YtDlp) run(ctx context.Context, args []string) ([]byte, error) {
	y.mu.Lock()
	defer y.mu.Unlock()
	bin, err := y.Binary.Path(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting yt-dlp: %w", err)
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = os.Environ()
	if y.DenoDir != "" {
		cmd.Env = append(cmd.Env, "DENO_DIR="+y.DenoDir)
	}
	if y.TempDir != "" {
		// A yt-dlp that was killed leaves its unpacked copy behind. Only one runs at a time (see mu),
		// so whatever is there is left over.
		_ = os.RemoveAll(y.TempDir)
		if err := os.MkdirAll(y.TempDir, 0o755); err != nil {
			return nil, err
		}
		cmd.Env = append(cmd.Env, "TMPDIR="+y.TempDir)
	}
	killProcessGroup(cmd)
	cmd.WaitDelay = 5 * time.Second
	err = cmd.Run()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("yt-dlp stopped: %w", ctx.Err())
	}
	if err != nil {
		return nil, ytDlpError(stderr.String(), err)
	}
	return stdout.Bytes(), nil
}

// ytDlpError turns a failed run into yt-dlp's own error message.
func ytDlpError(stderr string, runErr error) error {
	var errs []string
	for _, l := range strings.Split(stderr, "\n") {
		if l = strings.TrimSpace(l); strings.HasPrefix(l, "ERROR:") {
			errs = append(errs, l)
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "\n"))
	}
	if l := lastLine([]byte(stderr)); l != "" {
		return errors.New(l)
	}
	return fmt.Errorf("yt-dlp failed: %w", runErr)
}

// sourceNames maps yt-dlp's extractor keys (lowercased, without a "tab" suffix) to display names.
var sourceNames = map[string]string{
	"youtube":    "YouTube",
	"soundcloud": "SoundCloud",
	"bandcamp":   "Bandcamp",
	"mixcloud":   "Mixcloud",
	"archiveorg": "Internet Archive",
}

// SourceName turns yt-dlp's extractor key into the site's display name, for the SOURCE tag.
func SourceName(extractorKey string) string {
	k := strings.ToLower(extractorKey)
	if name, ok := sourceNames[k]; ok {
		return name
	}
	if name, ok := sourceNames[strings.TrimSuffix(k, "tab")]; ok {
		return name
	}
	return extractorKey
}

func lastLine(b []byte) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
