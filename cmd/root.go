package cmd

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/corrections"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/rip"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/resources"
	"github.com/navidrome/navidrome/scanner"
	"github.com/navidrome/navidrome/scheduler"
	"github.com/navidrome/navidrome/server/backgrounds"
	"github.com/navidrome/navidrome/server/events"
	"github.com/navidrome/navidrome/server/ripapi"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/sync/errgroup"

	// Import adapters to register them
	_ "github.com/navidrome/navidrome/adapters/deezer"
	_ "github.com/navidrome/navidrome/adapters/gotaglib"
	_ "github.com/navidrome/navidrome/adapters/lastfm"
	_ "github.com/navidrome/navidrome/adapters/listenbrainz"
)

var (
	cfgFile  string
	noBanner bool

	rootCmd = &cobra.Command{
		Use:   "navidrome",
		Short: "Navidrome is a self-hosted music server and streamer",
		Long: `Navidrome is a self-hosted music server and streamer.
Complete documentation is available at https://www.navidrome.org/docs`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			preRun()
		},
		Run: func(cmd *cobra.Command, args []string) {
			runNavidrome(cmd.Context())
		},
		PostRun: func(cmd *cobra.Command, args []string) {
			postRun()
		},
		Version: consts.Version,
	}
)

// Execute runs the root cobra command, which will start the Navidrome server by calling the runNavidrome function.
func Execute() {
	ctx, cancel := mainContext(context.Background())
	defer cancel()

	rootCmd.SetVersionTemplate(`{{println .Version}}`)
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		log.Fatal(err)
	}
}

func preRun() {
	if !noBanner {
		println(resources.Banner())
	}
	conf.Load(noBanner)
}

func postRun() {
	log.Info("Navidrome stopped, bye.")
}

// runNavidrome is the main entry point for the Navidrome server. It starts all the services and blocks.
// If any of the services returns an error, it will log it and exit. If the process receives a signal to exit,
// it will cancel the context and exit gracefully.
func runNavidrome(ctx context.Context) {
	defer db.Init(ctx)()

	// Before the watcher and the initial scan start, so both pick up the Inbox.
	inboxChanged, err := core.EnsureInboxLibrary(ctx, CreateDataStore(), conf.Server.Inbox.Path)
	if err != nil {
		log.Error(ctx, "Could not set up the Inbox library", err)
	}
	checkInbox(ctx)
	checkPromoteFolder(ctx)
	checkLibraryEditing(ctx)
	settleEdits(ctx)

	g, ctx := errgroup.WithContext(ctx)
	ripper := startRipper(ctx, g)
	editQueue := startEditJobs(ctx, g)
	g.Go(startServer(ctx, ripper, editQueue))
	g.Go(startSignaller(ctx))
	g.Go(startScheduler(ctx))
	g.Go(startPlaybackServer(ctx))
	g.Go(startJellyfinDiscovery(ctx))
	g.Go(schedulePeriodicBackup(ctx))
	g.Go(startInsightsCollector(ctx))
	g.Go(scheduleDBAnalyzer(ctx))
	g.Go(startPluginManager(ctx))
	artworkWorker := CreateArtworkWorker()
	g.Go(startArtworkWorker(ctx, artworkWorker))
	g.Go(scheduleArtworkHousekeeping(ctx, artworkWorker))
	g.Go(runInitialScan(ctx, inboxChanged))
	if conf.Server.Scanner.Enabled {
		g.Go(startScanWatcher(ctx))
		g.Go(schedulePeriodicScan(ctx))
	} else {
		log.Warn(ctx, "Automatic Scanning is DISABLED")
	}

	if err := g.Wait(); err != nil {
		log.Error("Fatal error in Navidrome. Aborting", err)
	}
}

// mainContext returns a context that is cancelled when the process receives a signal to exit.
func mainContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(ctx,
		os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGTERM,
		syscall.SIGABRT,
	)
}

// startServer starts the Navidrome web server, adding all the necessary routers.
func startServer(ctx context.Context, ripper *rip.Ripper, editQueue *editjob.Queue) func() error {
	return func() error {
		a := CreateServer()
		nativeAPI := CreateNativeAPIRouter(ctx)
		nativeAPI.SetEditQueue(editQueue)
		a.MountRouter("Native API", consts.URLPathNativeAPI, nativeAPI)
		if ripper != nil {
			a.MountRouter("Rip API", consts.URLPathNativeAPI+"/rip", ripapi.New(CreateDataStore(), ripper))
		}
		a.MountRouter("Subsonic API", consts.URLPathSubsonicAPI, CreateSubsonicAPIRouter(ctx))
		a.MountRouter("Public Endpoints", consts.URLPathPublic, CreatePublicRouter())
		// No "LastFM Auth" router: Last.fm is used for metadata only, so Listeners can't link
		// their accounts and nothing is scrobbled to it.
		if conf.Server.ListenBrainz.Enabled {
			a.MountRouter("ListenBrainz Auth", consts.URLPathNativeAPI+"/listenbrainz", CreateListenBrainzRouter())
		}
		if conf.Server.Jellyfin.Enabled {
			a.MountRouter("Jellyfin API", consts.URLPathJellyfinAPI, CreateJellyfinAPIRouter(ctx))
		}
		if conf.Server.Prometheus.Enabled {
			p := CreatePrometheus()
			// blocking call because takes <100ms but useful if fails
			p.WriteInitialMetrics(ctx)
			a.MountRouter("Prometheus metrics", conf.Server.Prometheus.MetricsPath, p.GetHandler())
		}
		if conf.Server.DevEnableProfiler {
			a.MountRouter("Profiling", "/debug", profilerHandler())
		}
		if strings.HasPrefix(conf.Server.UILoginBackgroundURL, "/") {
			a.MountRouter("Background images", conf.Server.UILoginBackgroundURL, backgrounds.NewHandler())
		}
		return a.Run(ctx, conf.Server.Address, conf.Server.Port, conf.Server.TLSCert, conf.Server.TLSKey)
	}
}

// startRipper starts the Inbox's background workers: the tag proposals and the dropped Files
// (#35) whenever there is an Inbox, and the one that runs Rips one at a time if ripping is on.
// It returns nil when there is neither, and then no Rip API is mounted.
func startRipper(ctx context.Context, g *errgroup.Group) *rip.Ripper {
	if !conf.Server.Ripping.Enabled && conf.Server.Inbox.Path == "" {
		return nil
	}
	ripper := rip.NewFromConfig(CreateDataStore(), CreateScanner(ctx))
	// Each scan may have found Files dropped into the Inbox.
	scanner.AfterScan(ripper.InboxScanned)
	resumed := make(chan struct{})
	g.Go(func() error {
		// Before any new Rip or Promote: finish or undo the moves the last stop interrupted.
		ripper.ResumePromotes(ctx)
		close(resumed)
		ripper.RunInbox(ctx)
		return nil
	})
	if !conf.Server.Ripping.Enabled {
		log.Info(ctx, "Ripping is disabled; uploads and Files dropped into the Inbox still get review cards")
		return ripper
	}
	g.Go(func() error {
		select {
		case <-resumed:
		case <-ctx.Done():
			return nil
		}
		ripper.Run(ctx)
		return nil
	})
	if err := ripper.ScheduleYtDlpUpdates(ctx, scheduler.GetInstance(), conf.Server.Ripping.YtDlpUpdateSchedule); err != nil {
		log.Error(ctx, "Could not schedule yt-dlp updates", err)
	}
	log.Info(ctx, "Ripping is enabled")
	return ripper
}

// checkPromoteFolder checks the promote folder (Inbox.PromotePath) once at startup. When it is
// unusable (not mounted read-write, or not inside the Library), Promote is turned off for this
// run, so neither its endpoint nor its button shows.
func checkPromoteFolder(ctx context.Context) {
	path, err := rip.CheckPromoteFolder(ctx, CreateDataStore(), conf.Server.Inbox.PromotePath)
	switch {
	case err != nil:
		log.Error(ctx, "Promote is off: the promote folder is not usable", "path", conf.Server.Inbox.PromotePath, err)
		conf.Server.Inbox.PromotePath = ""
	case path == "":
		log.Debug(ctx, "Promote is off (no Inbox.PromotePath set)")
	default:
		conf.Server.Inbox.PromotePath = path
		log.Info(ctx, "Promote is enabled", "path", path)
	}
}

// checkLibraryEditing decides once at startup whether editing the Library's Files is on, by
// trying a write in each library's folder (ADR-0011). With no writable folder, editing is off.
func checkLibraryEditing(ctx context.Context) {
	editing, err := core.DetectLibraryEditing(ctx, CreateDataStore())
	core.SetLibraryEditing(editing)
	switch {
	case err != nil:
		log.Error(ctx, "Editing the Library is off: could not check its folders", err)
	case editing.Enabled:
		log.Info(ctx, "Editing the Library is on", "libraries", editing.Libraries)
	default:
		log.Info(ctx, "Editing the Library is off: no Library folder is writable")
	}
}

// settleEdits finishes or undoes the writes to Files the last stop interrupted, before the
// server takes a new one. With editing off they are left as they are: the Library may just not be
// mounted read-write this time. The folder of a File it finished is rescanned, so its Track has
// the new tags (and the next edit's guard sees the File as scanned).
func settleEdits(ctx context.Context) {
	if !core.CurrentLibraryEditing().Enabled {
		return
	}
	tagedit.New(CreateDataStore(), CreateScanner(ctx)).Settle(ctx)
}

// startEditJobs starts the edit job queue (#162), which writes the admin's edits to Files in the
// background, one at a time. It runs after settleEdits, so a job the last stop interrupted
// carries on from a settled File. With editing off it doesn't run (and there is none): queued
// jobs wait for a start with the Library writable.
func startEditJobs(ctx context.Context, g *errgroup.Group) *editjob.Queue {
	if !core.CurrentLibraryEditing().Enabled {
		return nil
	}
	ds := CreateDataStore()
	queue := editjob.New(ds, events.GetBroker(), editjob.Kinds(ds, tagedit.New(ds, CreateScanner(ctx))))
	// The album Edit dialog, Track order and artist rename and merge write to the Files (#163).
	for kind, k := range corrections.JobKinds(ds, CreateScanner(ctx)) {
		queue.Handle(kind, k)
	}
	g.Go(func() error {
		queue.Run(ctx)
		return nil
	})
	return queue
}

// checkInbox turns the Inbox off for this run (Inbox.Path cleared) when the Inbox library isn't
// at Inbox.Path (none could be set up, or the folder wasn't usable and an older one is still
// recorded), so neither uploads nor the Inbox's review show, and no File is adopted from it.
func checkInbox(ctx context.Context) {
	if conf.Server.Inbox.Path == "" {
		return
	}
	inbox, err := core.FindInboxLibrary(ctx, CreateDataStore())
	if err == nil && (inbox == nil || !core.SameFolder(inbox.Path, conf.Server.Inbox.Path)) {
		err = errors.New("no Inbox library is set up at this path")
	}
	if err != nil {
		log.Warn(ctx, "The Inbox is off", "path", conf.Server.Inbox.Path, err)
		conf.Server.Inbox.Path = ""
	}
}

// profilerHandler returns the pprof handler. net/http/pprof resolves the profile
// name from the raw request path, so the BasePath has to come off first.
func profilerHandler() http.Handler {
	// A trailing or root slash would make StripPrefix drop the leading slash chi needs.
	basePath := strings.TrimRight(conf.Server.BasePath, "/")
	return http.StripPrefix(basePath, middleware.Profiler())
}

// schedulePeriodicScan schedules a periodic scan of the music library, if configured.
func schedulePeriodicScan(ctx context.Context) func() error {
	return func() error {
		schedule := conf.Server.Scanner.Schedule
		if schedule == "" {
			log.Info(ctx, "Periodic scan is DISABLED")
			return nil
		}

		s := CreateScanner(ctx)
		schedulerInstance := scheduler.GetInstance()

		log.Info("Scheduling periodic scan", "schedule", schedule)
		_, err := schedulerInstance.Add(schedule, func() {
			_, err := s.ScanAll(ctx, false)
			if err != nil {
				log.Error(ctx, "Error executing periodic scan", err)
			}
		})
		if err != nil {
			log.Error(ctx, "Error scheduling periodic scan", err)
		}
		return nil
	}
}

func pidHashChanged(ds model.DataStore) (bool, error) {
	pidAlbum, err := ds.Property(context.Background()).DefaultGet(consts.PIDAlbumKey, "")
	if err != nil {
		return false, err
	}
	pidTrack, err := ds.Property(context.Background()).DefaultGet(consts.PIDTrackKey, "")
	if err != nil {
		return false, err
	}
	return !strings.EqualFold(pidAlbum, conf.Server.PID.Album) || !strings.EqualFold(pidTrack, conf.Server.PID.Track), nil
}

// runInitialScan runs an initial scan of the music library if needed. A new or moved Inbox
// library is always scanned, even with ScanOnStartup off.
func runInitialScan(ctx context.Context, inboxChanged bool) func() error {
	return func() error {
		ds := CreateDataStore()
		fullScanRequired, err := ds.Property(ctx).DefaultGet(consts.FullScanAfterMigrationFlagKey, "0")
		if err != nil {
			return err
		}
		inProgress, err := ds.Library(ctx).ScanInProgress()
		if err != nil {
			return err
		}
		pidHasChanged, err := pidHashChanged(ds)
		if err != nil {
			return err
		}
		scanOnStartup := conf.Server.Scanner.Enabled && conf.Server.Scanner.ScanOnStartup
		inboxScan := conf.Server.Scanner.Enabled && inboxChanged
		scanNeeded := scanOnStartup || inboxScan || inProgress || fullScanRequired == "1" || pidHasChanged
		time.Sleep(2 * time.Second) // Wait 2 seconds before the initial scan
		if scanNeeded {
			s := CreateScanner(ctx)
			switch {
			case fullScanRequired == "1":
				log.Warn(ctx, "Full scan required after migration")
				_ = ds.Property(ctx).Delete(consts.FullScanAfterMigrationFlagKey)
			case pidHasChanged:
				log.Warn(ctx, "PID config changed, performing full scan")
				fullScanRequired = "1"
			case inProgress:
				log.Warn(ctx, "Resuming interrupted scan")
			default:
				log.Info("Executing initial scan")
			}

			_, err = s.ScanAll(ctx, fullScanRequired == "1")
			if err != nil {
				log.Error(ctx, "Scan failed", err)
			} else {
				log.Info(ctx, "Scan completed")
			}
		} else {
			log.Debug(ctx, "Initial scan not needed")
		}
		return nil
	}
}

func startScanWatcher(ctx context.Context) func() error {
	return func() error {
		if conf.Server.Scanner.WatcherWait == 0 {
			log.Debug("Folder watcher is DISABLED")
			return nil
		}
		w := CreateScanWatcher(ctx)
		err := w.Run(ctx)
		if err != nil {
			log.Error("Error starting watcher", err)
		}
		return nil
	}
}

func schedulePeriodicBackup(ctx context.Context) func() error {
	return func() error {
		schedule := conf.Server.Backup.Schedule
		if schedule == "" {
			log.Info(ctx, "Periodic backup is DISABLED")
			return nil
		}

		schedulerInstance := scheduler.GetInstance()

		log.Info("Scheduling periodic backup", "schedule", schedule)
		_, err := schedulerInstance.Add(schedule, func() {
			start := time.Now()
			path, err := db.Backup(ctx)
			elapsed := time.Since(start)
			if err != nil {
				log.Error(ctx, "Error backing up database", "elapsed", elapsed, err)
				return
			}
			log.Info(ctx, "Backup complete", "elapsed", elapsed, "path", path)

			count, err := db.Prune(ctx)
			if err != nil {
				log.Error(ctx, "Error pruning database", "error", err)
			} else if count > 0 {
				log.Info(ctx, "Successfully pruned old files", "count", count)
			} else {
				log.Info(ctx, "No backups pruned")
			}
		})

		return err
	}
}

func scheduleDBAnalyzer(ctx context.Context) func() error {
	return func() error {
		if !conf.Server.EnableScheduledDBAnalyze {
			log.Info(ctx, "Scheduled DB analysis is DISABLED")
			return nil
		}
		log.Info(ctx, "Scheduling DB analysis check", "schedule", consts.DBAnalyzeCheckSchedule)
		schedulerInstance := scheduler.GetInstance()
		_, err := schedulerInstance.Add(consts.DBAnalyzeCheckSchedule, func() {
			release, ok := scanner.LockForMaintenance()
			if !ok {
				log.Debug(ctx, "Skipping DB analysis check because a scan is in progress")
				return
			}
			defer release()
			if _, err := db.OptimizeIfNeeded(ctx); err != nil {
				log.Error(ctx, "Error analyzing DB", err)
			}
		})
		return err
	}
}

// startScheduler starts the Navidrome scheduler, which is used to run periodic tasks.
func startScheduler(ctx context.Context) func() error {
	return func() error {
		log.Info(ctx, "Starting scheduler")
		schedulerInstance := scheduler.GetInstance()
		schedulerInstance.Run(ctx)
		return nil
	}
}

// startInsightsCollector starts the Navidrome Insight Collector, if configured.
func startInsightsCollector(ctx context.Context) func() error {
	return func() error {
		if !conf.Server.EnableInsightsCollector {
			log.Info(ctx, "Insight Collector is DISABLED")
			return nil
		}
		log.Info(ctx, "Starting Insight Collector")
		select {
		case <-time.After(conf.Server.DevInsightsInitialDelay):
		case <-ctx.Done():
			return nil
		}
		ic := CreateInsights()
		ic.Run(ctx)
		return nil
	}
}

// startJellyfinDiscovery never returns an error: a discovery failure must not stop the server.
func startJellyfinDiscovery(ctx context.Context) func() error {
	return func() error {
		if !conf.Server.Jellyfin.Enabled || !conf.Server.Jellyfin.AutoDiscovery {
			log.Debug("Jellyfin auto-discovery is DISABLED")
			return nil
		}
		CreateJellyfinDiscovery().Serve(ctx)
		return nil
	}
}

// startPlaybackServer starts the Navidrome playback server, if configured.
// It is responsible for the Jukebox functionality
func startPlaybackServer(ctx context.Context) func() error {
	return func() error {
		if !conf.Server.Jukebox.Enabled {
			log.Debug("Jukebox is DISABLED")
			return nil
		}
		log.Info(ctx, "Starting Jukebox service")
		playbackInstance := GetPlaybackServer()
		return playbackInstance.Run(ctx)
	}
}

// startArtworkWorker starts the background artwork acquisition worker. It always
// runs; the queue is simply empty until something enqueues work into it.
func startArtworkWorker(ctx context.Context, worker *artwork.Worker) func() error {
	return func() error {
		log.Info(ctx, "Starting artwork worker")
		// The scanner writes to the DB for its whole run; competing for the write lock makes both fail.
		worker.PauseWhile(scanner.IsScanning)
		return worker.Run(ctx)
	}
}

// outsideScan runs a DB maintenance job unless a scan is running, and keeps a scan from starting
// until it ends; both write to the DB, and competing for the lock can make either fail.
func outsideScan(ctx context.Context, job string, run func(context.Context) error) {
	release, ok := scanner.LockForMaintenance()
	if !ok {
		log.Debug(ctx, "Skipping "+job+" because a scan is in progress")
		return
	}
	defer release()
	if err := run(ctx); err != nil {
		log.Error(ctx, "Error running "+job, err)
	}
}

// scheduleArtworkHousekeeping registers the recurring missing-state and prune jobs, and
// reports an artwork config change without acting on it.
func scheduleArtworkHousekeeping(ctx context.Context, worker *artwork.Worker) func() error {
	return func() error {
		schedulerInstance := scheduler.GetInstance()

		if _, err := schedulerInstance.Add(consts.ArtworkEnqueueMissingSchedule, func() {
			outsideScan(ctx, "artwork missing-state recheck", worker.EnqueueMissingAll)
		}); err != nil {
			log.Error(ctx, "Error scheduling artwork missing-state recheck", err)
		}

		if _, err := schedulerInstance.Add(consts.ArtworkPruneSchedule, func() {
			outsideScan(ctx, "artwork prune", worker.RunPrune)
		}); err != nil {
			log.Error(ctx, "Error scheduling artwork prune", err)
		}

		// Also run the missing-row recheck once at startup so a never-scanned entity is picked up
		// immediately, not only on the next hourly tick (e.g. after enabling the feature).
		outsideScan(ctx, "artwork missing-state recheck", worker.EnqueueMissingAll)

		if err := worker.ReconcileConfig(ctx); err != nil {
			log.Error(ctx, "Error checking the artwork config fingerprint", err)
		}
		return nil
	}
}

// startPluginManager starts the plugin manager, if configured.
func startPluginManager(ctx context.Context) func() error {
	return func() error {
		manager := GetPluginManager(ctx)
		if !conf.Server.Plugins.Enabled {
			log.Debug("Plugin system is DISABLED")
			return nil
		}
		log.Info(ctx, "Starting plugin manager")
		return manager.Start(ctx)
	}
}

// TODO: Implement some struct tags to map flags to viper
func init() {
	cobra.OnInitialize(func() {
		conf.InitConfig(cfgFile, true)
	})

	rootCmd.PersistentFlags().StringVarP(&cfgFile, "configfile", "c", "", `config file (default "./navidrome.toml")`)
	rootCmd.PersistentFlags().BoolVarP(&noBanner, "nobanner", "n", false, `don't show banner`)
	rootCmd.PersistentFlags().String("musicfolder", viper.GetString("musicfolder"), "folder where your music is stored")
	rootCmd.PersistentFlags().String("datafolder", viper.GetString("datafolder"), "folder to store application data (DB), needs write access")
	rootCmd.PersistentFlags().String("cachefolder", viper.GetString("cachefolder"), "folder to store cache data (transcoding, images...), needs write access")
	rootCmd.PersistentFlags().StringP("loglevel", "l", viper.GetString("loglevel"), "log level, possible values: error, info, debug, trace")
	rootCmd.PersistentFlags().String("logfile", viper.GetString("logfile"), "log file path, if not set logs will be printed to stderr")

	_ = viper.BindPFlag("musicfolder", rootCmd.PersistentFlags().Lookup("musicfolder"))
	_ = viper.BindPFlag("datafolder", rootCmd.PersistentFlags().Lookup("datafolder"))
	_ = viper.BindPFlag("cachefolder", rootCmd.PersistentFlags().Lookup("cachefolder"))
	_ = viper.BindPFlag("loglevel", rootCmd.PersistentFlags().Lookup("loglevel"))
	_ = viper.BindPFlag("logfile", rootCmd.PersistentFlags().Lookup("logfile"))

	rootCmd.Flags().StringP("address", "a", viper.GetString("address"), "IP address to bind to")
	rootCmd.Flags().IntP("port", "p", viper.GetInt("port"), "HTTP port Navidrome will listen to")
	rootCmd.Flags().String("baseurl", viper.GetString("baseurl"), "base URL to configure Navidrome behind a proxy (ex: /music or http://my.server.com)")
	rootCmd.Flags().String("tlscert", viper.GetString("tlscert"), "optional path to a TLS cert file (enables HTTPS listening)")
	rootCmd.Flags().String("unixsocketperm", viper.GetString("unixsocketperm"), "optional file permission for the unix socket")
	rootCmd.Flags().String("tlskey", viper.GetString("tlskey"), "optional path to a TLS key file (enables HTTPS listening)")

	rootCmd.Flags().Duration("sessiontimeout", viper.GetDuration("sessiontimeout"), "how long Navidrome will wait before closing web ui idle sessions")
	rootCmd.Flags().Duration("scaninterval", viper.GetDuration("scaninterval"), "how frequently to scan for changes in your music library")
	rootCmd.Flags().String("uiloginbackgroundurl", viper.GetString("uiloginbackgroundurl"), "URL to a backaground image used in the Login page")
	rootCmd.Flags().Bool("enabletranscodingconfig", viper.GetBool("enabletranscodingconfig"), "enables transcoding configuration in the UI")
	rootCmd.Flags().Bool("enabletranscodingcancellation", viper.GetBool("enabletranscodingcancellation"), "enables transcoding context cancellation")
	rootCmd.Flags().String("transcodingcachesize", viper.GetString("transcodingcachesize"), "size of transcoding cache")
	rootCmd.Flags().String("imagecachesize", viper.GetString("imagecachesize"), "size of image (art work) cache. set to 0 to disable cache")
	rootCmd.Flags().String("albumplaycountmode", viper.GetString("albumplaycountmode"), "how to compute playcount for albums. absolute (default) or normalized")
	rootCmd.Flags().Bool("autoimportplaylists", viper.GetBool("autoimportplaylists"), "enable/disable .m3u playlist auto-import`")

	rootCmd.Flags().Bool("prometheus.enabled", viper.GetBool("prometheus.enabled"), "enable/disable prometheus metrics endpoint")
	rootCmd.Flags().String("prometheus.metricspath", viper.GetString("prometheus.metricspath"), "http endpoint for prometheus metrics")

	_ = viper.BindPFlag("address", rootCmd.Flags().Lookup("address"))
	_ = viper.BindPFlag("port", rootCmd.Flags().Lookup("port"))
	_ = viper.BindPFlag("tlscert", rootCmd.Flags().Lookup("tlscert"))
	_ = viper.BindPFlag("unixsocketperm", rootCmd.Flags().Lookup("unixsocketperm"))
	_ = viper.BindPFlag("tlskey", rootCmd.Flags().Lookup("tlskey"))
	_ = viper.BindPFlag("baseurl", rootCmd.Flags().Lookup("baseurl"))

	_ = viper.BindPFlag("sessiontimeout", rootCmd.Flags().Lookup("sessiontimeout"))
	_ = viper.BindPFlag("scaninterval", rootCmd.Flags().Lookup("scaninterval"))
	_ = viper.BindPFlag("uiloginbackgroundurl", rootCmd.Flags().Lookup("uiloginbackgroundurl"))

	_ = viper.BindPFlag("prometheus.enabled", rootCmd.Flags().Lookup("prometheus.enabled"))
	_ = viper.BindPFlag("prometheus.metricspath", rootCmd.Flags().Lookup("prometheus.metricspath"))

	_ = viper.BindPFlag("enabletranscodingconfig", rootCmd.Flags().Lookup("enabletranscodingconfig"))
	_ = viper.BindPFlag("enabletranscodingcancellation", rootCmd.Flags().Lookup("enabletranscodingcancellation"))
	_ = viper.BindPFlag("transcodingcachesize", rootCmd.Flags().Lookup("transcodingcachesize"))
	_ = viper.BindPFlag("imagecachesize", rootCmd.Flags().Lookup("imagecachesize"))
}
