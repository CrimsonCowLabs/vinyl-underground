package rip

import (
	"net/http"
	"path/filepath"
	"runtime"
	"time"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/model"
)

// NewFromConfig sets up the Ripper from the Ripping.* settings: yt-dlp is kept in
// <DataFolder>/bin (/data/bin in the Docker image) and downloaded there on first use.
func NewFromConfig(ds model.DataStore, scanner FolderScanner) *Ripper {
	asset, _ := AssetFor(runtime.GOOS, runtime.GOARCH, IsMusl()) // empty: Binary.Path reports it
	bin := &Binary{
		Dir:        filepath.Join(conf.Server.DataFolder.String(), "bin"),
		ReleaseURL: conf.Server.Ripping.YtDlpReleaseURL,
		Asset:      asset,
		Client:     &http.Client{Timeout: 10 * time.Minute},
	}
	yt := &YtDlp{
		Binary:     bin,
		FFmpegPath: conf.Server.FFmpegPath,
		CacheDir:   filepath.Join(conf.Server.CacheFolder.String(), "yt-dlp"),
		TempDir:    filepath.Join(bin.Dir, "tmp"),
	}
	return New(ds, yt, scanner, Options{Timeout: conf.Server.Ripping.Timeout})
}
