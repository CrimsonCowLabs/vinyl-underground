// Package rip fetches the audio of a web page with yt-dlp into the Inbox, as a Rip.
package rip

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/scanner"
)

var (
	// ErrInvalidURL is returned for anything but an http(s) URL.
	ErrInvalidURL = errors.New("enter the http(s) address of a web page with a video")
	// ErrNoInbox is returned when there is no Inbox to rip into.
	ErrNoInbox = errors.New("the Inbox is not set up, so there is nowhere to put a Rip")
)

// errPlaylist is the job's error for a playlist URL: only single videos can be ripped for now.
const errPlaylist = "this is a playlist; for now only one video can be ripped at a time, so paste the link to a single video"

// workDirName is the hidden Inbox folder each job downloads into, so the scanner never sees a
// half-written File (it skips hidden folders) and the finished File is moved into place on the
// same disk.
const workDirName = ".rips"

// Fetcher gets a web page's metadata and audio. *YtDlp implements it.
type Fetcher interface {
	Probe(ctx context.Context, url string) (*Info, error)
	Download(ctx context.Context, url, dir string) (string, error)
}

// FolderScanner scans library folders. model.Scanner implements it.
type FolderScanner interface {
	ScanFolders(ctx context.Context, fullScan bool, targets []model.ScanTarget) ([]string, error)
}

type Options struct {
	// Timeout bounds one job, from looking at the page to the File landing in the Inbox.
	Timeout time.Duration
	// Tagger writes the Rip's tags into the File. Defaults to WriteTags.
	Tagger func(path string, info *Info) error
	// ScanRetryDelay and ScanAttempts control how long a finished Rip waits for another scan
	// (often the watcher's, set off by the Rip itself) before its own Inbox scan runs.
	ScanRetryDelay time.Duration
	ScanAttempts   int
	// Updater updates yt-dlp, after a failed Rip and on demand. *Binary implements it. Nil turns
	// updates off.
	Updater Updater
}

// Updater keeps yt-dlp up to date. *Binary implements it.
type Updater interface {
	Update(ctx context.Context) (UpdateResult, error)
	Version(ctx context.Context) (string, error)
}

// Scheduler runs a function on a crontab schedule. scheduler.Scheduler implements it.
type Scheduler interface {
	Add(crontab string, cmd func()) (int, error)
}

var (
	// ErrRipRunning is returned by UpdateYtDlp, when it isn't to wait, while a Rip is running.
	ErrRipRunning = errors.New("a Rip is running; update yt-dlp once it has finished")
	// ErrNoUpdater is returned when yt-dlp can't be updated (there is no Updater).
	ErrNoUpdater = errors.New("yt-dlp can't be updated here")
)

// Ripper runs Rip jobs in the background, one at a time. Jobs live in the database, so they keep
// going when the admin closes the tab, and a job the app stopped in the middle of is started
// again from scratch on the next start.
type Ripper struct {
	ds      model.DataStore
	fetcher Fetcher
	scanner FolderScanner
	opts    Options
	wake    chan struct{}
	// ytDlp is held while yt-dlp runs a Rip or is being updated, so an update never swaps the
	// binary under a running Rip.
	ytDlp chan struct{}
}

func New(ds model.DataStore, fetcher Fetcher, scanner FolderScanner, opts Options) *Ripper {
	opts.Timeout = cmp.Or(opts.Timeout, 30*time.Minute)
	opts.ScanRetryDelay = cmp.Or(opts.ScanRetryDelay, 10*time.Second)
	opts.ScanAttempts = cmp.Or(opts.ScanAttempts, 180)
	if opts.Tagger == nil {
		opts.Tagger = WriteTags
	}
	return &Ripper{
		ds: ds, fetcher: fetcher, scanner: scanner, opts: opts,
		wake: make(chan struct{}, 1), ytDlp: make(chan struct{}, 1),
	}
}

// Enqueue queues a Rip of the page at rawURL for the user in ctx.
func (r *Ripper) Enqueue(ctx context.Context, rawURL string) (*model.RipJob, error) {
	rawURL = strings.TrimSpace(rawURL)
	if !validURL(rawURL) {
		return nil, ErrInvalidURL
	}
	inbox, err := core.FindInboxLibrary(ctx, r.ds)
	if err != nil {
		return nil, err
	}
	if inbox == nil {
		return nil, ErrNoInbox
	}
	user, _ := request.UserFrom(ctx)
	now := time.Now()
	job := &model.RipJob{UserID: user.ID, URL: rawURL, Status: model.RipQueued, CreatedAt: now, UpdatedAt: now}
	if err := r.ds.RipJob(ctx).Put(job); err != nil {
		return nil, err
	}
	log.Info(ctx, "Rip queued", "id", job.ID, "url", rawURL)
	select {
	case r.wake <- struct{}{}:
	default:
	}
	return job, nil
}

// Jobs returns every job, newest first.
func (r *Ripper) Jobs(ctx context.Context) (model.RipJobs, error) {
	return r.ds.RipJob(ctx).GetAll()
}

func validURL(s string) bool {
	if len(s) > 2048 {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// Run works through the queue until ctx ends. Jobs that were downloading when the app last
// stopped go back in the queue first.
func (r *Ripper) Run(ctx context.Context) {
	if n, err := r.ds.RipJob(ctx).RequeueInterrupted(); err != nil {
		log.Error(ctx, "Could not re-queue interrupted Rips", err)
	} else if n > 0 {
		log.Info(ctx, "Re-queued Rips interrupted by the last stop", "count", n)
	}
	for ctx.Err() == nil {
		job, err := r.ds.RipJob(ctx).NextQueued()
		switch {
		case err == nil:
			r.process(ctx, job)
			continue
		case !errors.Is(err, model.ErrNotFound):
			log.Error(ctx, "Could not read the Rip queue", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-r.wake:
		case <-time.After(time.Minute):
		}
	}
}

func (r *Ripper) process(ctx context.Context, job *model.RipJob) {
	if ctx.Err() != nil {
		return
	}
	job.Status = model.RipDownloading
	r.save(ctx, job)
	log.Info(ctx, "Ripping", "id", job.ID, "url", job.URL)

	inbox, err := core.FindInboxLibrary(ctx, r.ds)
	if err == nil && inbox == nil {
		err = ErrNoInbox
	}
	if err == nil && !inInbox(inbox, job) {
		err = r.ripWithRetry(ctx, inbox.Path, job)
	}
	if ctx.Err() != nil {
		// The app is stopping. The job stays "downloading", so the next start runs it again (or,
		// if its File is already in the Inbox, only scans it).
		log.Info(ctx, "Rip interrupted; it will run again on the next start", "id", job.ID)
		return
	}
	if err != nil {
		job.Status, job.Error = model.RipFailed, err.Error()
		log.Warn(ctx, "Rip failed", "id", job.ID, "url", job.URL, "error", job.Error)
		r.save(ctx, job)
		return
	}

	// The File is in the Inbox now, so the scan is not bound by the job's timeout.
	if err := r.scanInbox(ctx, inbox.ID); err != nil {
		if ctx.Err() != nil {
			log.Info(ctx, "Rip interrupted before its Inbox scan; it is scanned on the next start", "id", job.ID)
			return
		}
		job.Error = fmt.Sprintf("saved, but the Inbox scan failed (%s); the Rip shows up after the next scan", err)
		log.Warn(ctx, "Rip saved, but the Inbox scan failed", "id", job.ID, "path", job.Path, err)
	} else {
		log.Info(ctx, "Rip ready for review", "id", job.ID, "path", job.Path)
	}
	job.Status = model.RipReady
	r.save(ctx, job)
}

// inInbox reports whether the job's File already landed in the Inbox, in a run the app stopped
// before the Inbox scan finished.
func inInbox(inbox *model.Library, job *model.RipJob) bool {
	if job.Path == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(inbox.Path, job.Path))
	return err == nil
}

// ripWithRetry rips the job, with no yt-dlp update allowed meanwhile. When yt-dlp fails in a way
// a newer yt-dlp might fix (see UpdateMightFix), it updates yt-dlp and tries the job once more,
// even if the update fails or finds yt-dlp already current, as the failure may have been
// passing. Each try gets the full timeout.
func (r *Ripper) ripWithRetry(ctx context.Context, inboxPath string, job *model.RipJob) error {
	if err := r.lockYtDlp(ctx, true); err != nil {
		return err
	}
	defer r.unlockYtDlp()
	err := r.ripTimed(ctx, inboxPath, job)
	if err == nil || r.opts.Updater == nil || ctx.Err() != nil || !UpdateMightFix(err) {
		return err
	}
	log.Warn(ctx, "Rip failed; updating yt-dlp and trying again", "id", job.ID, "error", err)
	res, updErr := r.opts.Updater.Update(ctx)
	if updErr != nil {
		log.Warn(ctx, "Could not update yt-dlp; trying the Rip again with the one installed", "id", job.ID, updErr)
	} else {
		log.Info(ctx, "yt-dlp checked before trying the Rip again", "id", job.ID, "version", res.Version, "updated", res.Updated)
	}
	if ctx.Err() != nil {
		return err
	}
	err = r.ripTimed(ctx, inboxPath, job)
	if err != nil && updErr != nil {
		err = fmt.Errorf("%w (updating yt-dlp failed too: %v)", err, updErr)
	}
	return err
}

// ripTimed is rip, bounded by the Rip's timeout.
func (r *Ripper) ripTimed(ctx context.Context, inboxPath string, job *model.RipJob) error {
	jobCtx, cancel := context.WithTimeout(ctx, r.opts.Timeout)
	defer cancel()
	err := r.rip(jobCtx, inboxPath, job)
	if err != nil && errors.Is(jobCtx.Err(), context.DeadlineExceeded) {
		err = fmt.Errorf("the Rip took longer than %s and was stopped", r.opts.Timeout)
	}
	return err
}

// UpdateYtDlp updates yt-dlp to its latest release. It waits for a running Rip to finish first,
// or, if wait is false, returns ErrRipRunning straight away.
func (r *Ripper) UpdateYtDlp(ctx context.Context, wait bool) (UpdateResult, error) {
	if r.opts.Updater == nil {
		return UpdateResult{}, ErrNoUpdater
	}
	if err := r.lockYtDlp(ctx, wait); err != nil {
		return UpdateResult{}, err
	}
	defer r.unlockYtDlp()
	return r.opts.Updater.Update(ctx)
}

// YtDlpVersion returns the installed yt-dlp's version, or "" before it is installed.
func (r *Ripper) YtDlpVersion(ctx context.Context) (string, error) {
	if r.opts.Updater == nil {
		return "", ErrNoUpdater
	}
	return r.opts.Updater.Version(ctx)
}

// ScheduleYtDlpUpdates updates yt-dlp on the crontab schedule, waiting for a running Rip each
// time. An empty schedule schedules nothing.
func (r *Ripper) ScheduleYtDlpUpdates(ctx context.Context, s Scheduler, schedule string) error {
	if schedule == "" || r.opts.Updater == nil {
		log.Info(ctx, "Scheduled yt-dlp updates are DISABLED")
		return nil
	}
	log.Info(ctx, "Scheduling yt-dlp updates", "schedule", schedule)
	_, err := s.Add(schedule, func() {
		res, err := r.UpdateYtDlp(ctx, true)
		switch {
		case err != nil:
			log.Error(ctx, "Scheduled yt-dlp update failed; keeping the one installed", "version", res.Version, err)
		case res.Updated:
			log.Info(ctx, "Updated yt-dlp", "from", res.Previous, "to", res.Version)
		default:
			log.Debug(ctx, "yt-dlp is up to date", "version", res.Version)
		}
	})
	return err
}

// lockYtDlp takes the yt-dlp lock, waiting for it if wait is true.
func (r *Ripper) lockYtDlp(ctx context.Context, wait bool) error {
	if !wait {
		select {
		case r.ytDlp <- struct{}{}:
			return nil
		default:
			return ErrRipRunning
		}
	}
	select {
	case r.ytDlp <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Ripper) unlockYtDlp() { <-r.ytDlp }

// rip fetches the job's audio into the Inbox folder at inboxPath, and records the File's path in
// the job as soon as it is there.
func (r *Ripper) rip(ctx context.Context, inboxPath string, job *model.RipJob) error {
	work := filepath.Join(inboxPath, workDirName, job.ID)
	if err := os.RemoveAll(work); err != nil {
		return err
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	defer func() {
		_ = os.RemoveAll(work)
		_ = os.Remove(filepath.Dir(work)) // only if no other work folder is left
	}()

	info, err := r.fetcher.Probe(ctx, job.URL)
	if err != nil {
		return err
	}
	if info.IsPlaylist {
		return errors.New(errPlaylist)
	}
	job.Title, job.Source = info.Title, info.Source
	r.save(ctx, job)

	file, err := r.fetcher.Download(ctx, job.URL, work)
	if err != nil {
		return err
	}
	if err := r.opts.Tagger(file, info); err != nil {
		return err
	}
	dest, err := moveIntoInbox(file, inboxPath, fileName(info, filepath.Ext(file)))
	if err != nil {
		return err
	}
	job.Path = filepath.Base(dest)
	r.save(context.WithoutCancel(ctx), job)
	return nil
}

// scanInbox scans the Inbox library, waiting while another scan runs (often the watcher's, set
// off by the Rip itself). Any other scan error is returned straight away.
func (r *Ripper) scanInbox(ctx context.Context, libID int) error {
	var err error
	for range r.opts.ScanAttempts {
		_, err = r.scanner.ScanFolders(ctx, false, []model.ScanTarget{{LibraryID: libID}})
		if !errors.Is(err, scanner.ErrAlreadyScanning) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(r.opts.ScanRetryDelay):
		}
	}
	return err
}

func (r *Ripper) save(ctx context.Context, job *model.RipJob) {
	job.UpdatedAt = time.Now()
	if err := r.ds.RipJob(ctx).Put(job); err != nil {
		log.Error(ctx, "Could not save the Rip job", "id", job.ID, err)
	}
}

// moveIntoInbox moves the finished File into the Inbox under name, adding " (2)", " (3)"... to
// the name rather than replacing a File already there.
func moveIntoInbox(file, inbox, name string) (string, error) {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; i < 1000; i++ {
		candidate := name
		if i > 1 {
			candidate = fmt.Sprintf("%s (%d)%s", base, i, ext)
		}
		dest := filepath.Join(inbox, candidate)
		// A hard link fails if dest exists, so an existing File is never replaced.
		if err := os.Link(file, dest); err == nil {
			_ = os.Remove(file)
			return dest, nil
		} else if errors.Is(err, os.ErrExist) {
			continue
		}
		// No hard links on this disk: fall back to a rename, after checking dest is free.
		if _, err := os.Lstat(dest); err == nil {
			continue
		}
		if err := os.Rename(file, dest); err != nil {
			return "", fmt.Errorf("moving the Rip into the Inbox: %w", err)
		}
		return dest, nil
	}
	return "", fmt.Errorf("too many Files named %q in the Inbox", name)
}

// fileName names the File "<Artist> - <Title> [<id>].<ext>", safe for any disk.
func fileName(info *Info, ext string) string {
	name := sanitize(info.Title)
	if a := sanitize(info.Artist); a != "" {
		name = a + " - " + name
	}
	if len(name) > 160 {
		name = strings.ToValidUTF8(name[:160], "")
	}
	name = strings.TrimRight(name, " .")
	if id := sanitize(info.ID); id != "" {
		name += " [" + id + "]"
	}
	name = strings.TrimLeft(strings.TrimSpace(name), ". -")
	return cmp.Or(name, "Rip") + strings.ToLower(ext)
}

func sanitize(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r):
			return -1
		case strings.ContainsRune(`/\:*?"<>|`, r):
			return '_'
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}
