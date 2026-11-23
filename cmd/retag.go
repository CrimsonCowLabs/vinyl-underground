package cmd

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"syscall"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/musicbrainz"
	"github.com/navidrome/navidrome/core/retag"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/id"
	"github.com/spf13/cobra"
)

var (
	retagApply         bool
	retagPath          string
	retagNoMusicBrainz bool
)

func init() {
	retagCmd.Flags().BoolVar(&retagApply, "apply", false, "write the changes (without it, only report them)")
	retagCmd.Flags().StringVar(&retagPath, "path", "", "only retag the Files under this folder (relative to the library)")
	retagCmd.Flags().BoolVar(&retagNoMusicBrainz, "no-musicbrainz", false, "don't ask MusicBrainz: local fixes only")
	rootCmd.AddCommand(retagCmd)
}

var retagCmd = &cobra.Command{
	Use:   "retag",
	Short: "Repair the tags of the Library's Files",
	Long: `Repair the tags of every File in the Library (not the Inbox, not Quarantine): titles holding
their track number ("10 - Bones"), "Track 06" and number-only titles, file extensions and the
artist's name in titles, unknown artists, and empty track numbers, disc numbers and years.

Local evidence (the title, the File's name and folder) fixes what it can settle. MusicBrainz,
matched an album at a time, repairs the rest; a value that is set and looks right is never
overwritten. What nothing settles is left as
it is, and listed in the report.

Without --apply nothing is written: the run reports what it would change. With --apply, every
write goes through the Edit journal (one job per run, which the Edit history can undo) and is
checked before it replaces the File.
Run it with the server stopped:

  docker compose stop vinyl-underground
  docker compose run --rm vinyl-underground retag --apply
  docker compose start vinyl-underground

Answers from MusicBrainz are kept in the cache folder, so a run that is stopped
(Ctrl-C stops it between Files) and run again picks up where it was, and a second run changes
nothing it already fixed.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		if err := runRetag(cmd.Context()); err != nil {
			log.Fatal(cmd.Context(), "Retag failed", err)
		}
	},
}

func runRetag(ctx context.Context) error {
	requireExistingDB()
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	if retagApply && serverRunning() {
		return fmt.Errorf("the server is running on port %d: stop it first, as two processes would scan and write the same Files", conf.Server.Port)
	}
	ds, ctx := getAdminContext(ctx)

	editing, err := core.DetectLibraryEditing(ctx, ds)
	if err != nil {
		return err
	}
	core.SetLibraryEditing(editing)
	var libs []retag.Library
	for _, l := range editing.Libraries {
		if !l.Writable && retagApply {
			log.Warn(ctx, "Library is read-only: it is left out", "library", l.Name)
			continue
		}
		libID := l.ID
		libs = append(libs, retag.Library{ID: libID, Files: func(ctx context.Context) (model.MediaFiles, error) {
			return ds.MediaFile(ctx).GetAll(model.QueryOptions{
				Filters: squirrel.And{squirrel.Eq{"media_file.library_id": libID}, squirrel.Eq{"missing": false}},
				Sort:    "path",
			})
		}})
	}
	if len(libs) == 0 {
		return fmt.Errorf("no Library folder to retag (with --apply, it must be writable: VU_LIBRARY_MODE=rw)")
	}

	editor := tagedit.New(ds, CreateScanner(ctx))
	if retagApply {
		// The server is stopped, so a write it left half-done is finished or undone first.
		editor.Settle(ctx)
	}
	cache := filepath.Join(conf.Server.CacheFolder.String(), "retag")
	jobID := id.NewRandom()
	opts := retag.Options{Writer: editor, Apply: retagApply, JobID: jobID, PathPrefix: retagPath, Progress: os.Stderr}
	if !retagNoMusicBrainz {
		opts.MusicBrainz = musicbrainz.New(musicbrainz.Options{Store: retag.DirStore{Dir: filepath.Join(cache, "musicbrainz")}})
	}

	reportPath := filepath.Join(conf.Server.DataFolder.String(), "retag-"+time.Now().Format("20060102-150405")+".tsv")
	report, err := os.Create(reportPath)
	if err != nil {
		return fmt.Errorf("creating the report: %w", err)
	}
	defer report.Close()
	opts.Report = report

	sum, err := retag.Run(ctx, libs, opts)
	printRetagSummary(sum, jobID, reportPath)
	if ctx.Err() != nil {
		fmt.Println("Stopped. Run it again to carry on.")
		return nil
	}
	return err
}

// serverRunning reports whether a server answers on this host's port.
func serverRunning() bool {
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get("http://127.0.0.1:" + strconv.Itoa(conf.Server.Port) + "/ping")
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}

func printRetagSummary(s retag.Summary, jobID, reportPath string) {
	verb := "Would change"
	if retagApply {
		verb = "Changed"
	}
	w := newTabWriter(os.Stdout)
	_, _ = fmt.Fprintf(w, "Files\t%d (in %d folders)\n", s.Files, s.Folders)
	_, _ = fmt.Fprintf(w, "%s\t%d (%d from MusicBrainz)\n", verb, s.Changed, s.FromMusicBrainz)
	_, _ = fmt.Fprintf(w, "Unchanged\t%d\n", s.Unchanged)
	_, _ = fmt.Fprintf(w, "Left flagged\t%d\n", s.Flagged)
	_, _ = fmt.Fprintf(w, "Skipped\t%d\n", s.Skipped)
	_, _ = fmt.Fprintf(w, "Failed\t%d\n", s.Failed)
	for _, fix := range slices.Sorted(maps.Keys(s.Fixes)) {
		_, _ = fmt.Fprintf(w, "  %s\t%d\n", fix, s.Fixes[fix])
	}
	_ = w.Flush()
	fmt.Println("Report:", reportPath)
	if retagApply && s.Changed > 0 {
		fmt.Println("Edit journal job:", jobID)
	}
}
