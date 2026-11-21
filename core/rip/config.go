package rip

import (
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/core/rip/match"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// NewFromConfig sets up the Ripper from the Ripping.* settings: yt-dlp is kept in
// <DataFolder>/bin (/data/bin in the Docker image), downloaded there on first use and updated
// there (see Ripper.UpdateYtDlp and Ripper.ScheduleYtDlpUpdates).
func NewFromConfig(ds model.DataStore, scanner FolderScanner) *Ripper {
	asset, _ := AssetFor(runtime.GOOS, runtime.GOARCH, IsMusl()) // empty: Binary.Path reports it
	bin := &Binary{
		Dir:        filepath.Join(conf.Server.DataFolder.String(), "bin"),
		ReleaseURL: conf.Server.Ripping.YtDlpReleaseURL,
		Asset:      asset,
		Client:     &http.Client{Timeout: 10 * time.Minute},
		TempDir:    filepath.Join(conf.Server.DataFolder.String(), "bin", "tmp-version"),
	}
	yt := &YtDlp{
		Binary:     bin,
		FFmpegPath: conf.Server.FFmpegPath,
		CacheDir:   filepath.Join(conf.Server.CacheFolder.String(), "yt-dlp"),
		TempDir:    filepath.Join(bin.Dir, "tmp"),
		DenoDir:    filepath.Join(conf.Server.CacheFolder.String(), "deno"),
	}
	opts := Options{Timeout: conf.Server.Ripping.Timeout, Matcher: match.New(ds)}
	if asset != "" {
		opts.Updater = bin // no standalone build to update to otherwise
	}
	if deno, err := exec.LookPath("deno"); err == nil {
		yt.DenoPath = deno
	} else {
		log.Warn("Ripping: deno is not installed, so yt-dlp may miss formats or fail on YouTube")
	}
	return New(ds, yt, scanner, opts)
}
