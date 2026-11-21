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
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/scanner"
)

const (
	// ConfirmAbove is how many items a playlist can have before the admin must confirm the Rip.
	ConfirmAbove = 50
	// MaxItems is the most items one job can have. A bigger playlist is refused.
	MaxItems = 300
)

var (
	// ErrInvalidURL is returned for anything but an http(s) URL.
	ErrInvalidURL = errors.New("enter the http(s) address of a web page with a video, a playlist or an album")
	// ErrNoInbox is returned when there is no Inbox to rip into.
	ErrNoInbox = errors.New("the Inbox is not set up, so there is nowhere to put a Rip")
)

// Preview is what a page holds, for the admin to confirm before it is ripped: a playlist of
// more than ConfirmAbove items, or videos the Library already seems to have.
type Preview struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Count int    `json:"count"`
	// Titles are the items' "<Artist> - <Title>", as listed by the site.
	Titles []string `json:"titles"`
	// ConfirmCount is true when the playlist has more than ConfirmAbove items.
	ConfirmCount bool `json:"confirmCount"`
	// AlreadyHave are the items the Library or the Inbox already seems to have, in order. It is
	// only a warning: confirming rips them too.
	AlreadyHave []PreviewItem `json:"alreadyHave"`
}

// NeedsConfirmationError is returned by Enqueue, and nothing is queued, until the admin confirms
// a playlist of more than ConfirmAbove items, or videos the Library already seems to have.
type NeedsConfirmationError struct {
	Preview *Preview
}

func (e *NeedsConfirmationError) Error() string {
	p, have := e.Preview, len(e.Preview.AlreadyHave)
	switch {
	case p.ConfirmCount && have > 0:
		return fmt.Sprintf("this playlist has %d items, and the Library already seems to have %d of them; confirm to rip them all", p.Count, have)
	case p.ConfirmCount:
		return fmt.Sprintf("this playlist has %d items; confirm to rip them all", p.Count)
	case p.Count <= 1:
		return "the Library already seems to have this; rip it anyway?"
	default:
		return fmt.Sprintf("the Library already seems to have %d of these %d items; rip them anyway?", have, p.Count)
	}
}

// TooManyItemsError is returned by Enqueue for a playlist of more than MaxItems items.
type TooManyItemsError struct {
	Count int
}

func (e *TooManyItemsError) Error() string {
	if e.Count == MaxItems+1 {
		// Only the first MaxItems+1 are listed, and the site (e.g. a channel) gave no total.
		return fmt.Sprintf("this playlist has more than %d items, and at most %d can be ripped at once", MaxItems, MaxItems)
	}
	return fmt.Sprintf("this playlist has %d items, and at most %d can be ripped at once", e.Count, MaxItems)
}

const (
	// errNestedPlaylist is an item's error when the item is a playlist itself (e.g. in a channel).
	errNestedPlaylist = "this item is a playlist itself; paste its own link to rip it"
	errNoItemURL      = "this item has no web page of its own to rip"
	// errLatePlaylist is the error of a URL queued as one video, as yt-dlp couldn't read it then,
	// that turned out to be a playlist.
	errLatePlaylist  = "this is a playlist, which yt-dlp could only read after an update; paste the link again to rip all of it"
	errEmptyPlaylist = "this playlist is empty"
)

// workDirName is the hidden Inbox folder each item downloads into, so the scanner never sees a
// half-written File (it skips hidden folders) and the finished File is moved into place on the
// same disk.
const workDirName = ".rips"

// Fetcher gets a web page's metadata and audio. *YtDlp implements it.
type Fetcher interface {
	// Probe looks at the page without downloading it. For a playlist, it lists the items.
	Probe(ctx context.Context, url string) (*Info, error)
	Download(ctx context.Context, url, dir string) (string, error)
}

// FolderScanner scans library folders. model.Scanner implements it.
type FolderScanner interface {
	ScanFolders(ctx context.Context, fullScan bool, targets []model.ScanTarget) ([]string, error)
}

type Options struct {
	// Timeout bounds one item, from looking at its page to its File landing in the Inbox.
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
	// Matcher finds the Tracks the Library already seems to have for a video, to warn the admin
	// before it is ripped. Nil warns of nothing.
	Matcher Matcher
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
	// ErrUpdateRunning is returned by UpdateYtDlp, when it isn't to wait, while another update runs.
	ErrUpdateRunning = errors.New("yt-dlp is already being updated")
	// ErrNoUpdater is returned when yt-dlp can't be updated (there is no Updater), as on a platform
	// with no standalone yt-dlp build.
	ErrNoUpdater = errors.New("yt-dlp can't be updated here: there is no standalone yt-dlp build for this platform")
)

// Ripper runs Rip jobs in the background, one at a time, and each job's items one at a time, in
// order. Jobs and items live in the database, so they keep going when the admin closes the tab.
// An item the app stopped in the middle of is ripped again from scratch on the next start, and
// items whose File already landed are not ripped again.
type Ripper struct {
	ds      model.DataStore
	fetcher Fetcher
	scanner FolderScanner
	opts    Options
	wake    chan struct{}
	// ytDlp is held while yt-dlp runs a Rip or is being updated, so an update never swaps the
	// binary under a running Rip.
	ytDlp chan struct{}
	// updating is true while UpdateYtDlp holds ytDlp.
	updating atomic.Bool
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

// Enqueue queues a Rip of the page at rawURL for the user in ctx: one job, with one item for a
// video, or one per video for a playlist or album. It looks at the page first, and matches each
// video against the Library: unless confirmed, a playlist of more than ConfirmAbove items, or
// videos the Library already seems to have, return a *NeedsConfirmationError. A playlist of more
// than MaxItems returns a *TooManyItemsError. Queued items keep their matches. If the page can't be read, the job is saved as failed with
// yt-dlp's error, as it would be if the worker had found it.
func (r *Ripper) Enqueue(ctx context.Context, rawURL string, confirmed bool) (*model.RipJob, error) {
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
	job := &model.RipJob{UserID: user.ID, URL: rawURL, Status: model.RipQueued, CreatedAt: now, UpdatedAt: now,
		Items: model.RipItems{}}

	info, err := r.fetcher.Probe(ctx, rawURL)
	switch {
	case err != nil && r.opts.Updater != nil && UpdateMightFix(err):
		// A newer yt-dlp might read the page. The worker updates it and tries again (see
		// ripWithRetry), so queue the URL as one item, as if it were a single video.
		log.Warn(ctx, "Could not look at the page; queuing it as one video to try again after a yt-dlp update", "url", rawURL, err)
		job.Items = model.RipItems{newItem(1, rawURL, "", "", now)}
	case err != nil:
		job.Status, job.Error = model.RipFailed, err.Error()
	case info.IsPlaylist && info.Count > MaxItems:
		return nil, &TooManyItemsError{Count: info.Count}
	case info.IsPlaylist && len(info.Entries) == 0:
		job.Title, job.Source = info.Title, info.Source
		job.Status, job.Error = model.RipFailed, errEmptyPlaylist
	case info.IsPlaylist:
		job.Title, job.Source, job.Folder = info.Title, info.Source, folderName(info)
		for i, e := range info.Entries {
			job.Items = append(job.Items, newItem(i+1, e.URL, e.Title, e.Artist, now))
		}
	default:
		job.Title, job.Source = info.Title, info.Source
		// The pasted URL, not the page's canonical one: it is what was asked for, and yt-dlp
		// rips just that video from a watch?v=…&list=… link.
		job.Items = model.RipItems{newItem(1, rawURL, info.Title, info.Artist, now)}
	}
	if job.Status != model.RipFailed {
		big := info != nil && info.IsPlaylist && info.Count > ConfirmAbove
		have := r.alreadyHave(ctx, job.Items)
		if !confirmed && (big || len(have) > 0) {
			return nil, &NeedsConfirmationError{Preview: preview(rawURL, info, job.Items, big, have)}
		}
	}

	err = r.ds.WithTx(func(tx model.DataStore) error {
		if err := tx.RipJob(ctx).Put(job); err != nil {
			return err
		}
		for i := range job.Items {
			job.Items[i].JobID = job.ID
			if err := tx.RipItem(ctx).Put(&job.Items[i]); err != nil {
				return err
			}
		}
		return nil
	}, "rip enqueue")
	if err != nil {
		return nil, err
	}
	if job.Status == model.RipFailed {
		log.Warn(ctx, "Rip failed", "id", job.ID, "url", rawURL, "error", job.Error)
		return job, nil
	}
	log.Info(ctx, "Rip queued", "id", job.ID, "url", rawURL, "items", len(job.Items))
	select {
	case r.wake <- struct{}{}:
	default:
	}
	return job, nil
}

func newItem(pos int, itemURL, title, artist string, now time.Time) model.RipItem {
	it := model.RipItem{Position: pos, URL: itemURL, Title: title, Artist: artist,
		Status: model.RipQueued, CreatedAt: now, UpdatedAt: now}
	if !validURL(itemURL) {
		it.Status, it.Error = model.RipFailed, errNoItemURL
	}
	return it
}

func preview(rawURL string, info *Info, items model.RipItems, big bool, have []PreviewItem) *Preview {
	p := &Preview{URL: rawURL, Count: len(items), Titles: []string{}, ConfirmCount: big, AlreadyHave: have}
	if info != nil {
		p.Title, p.Count = info.Title, info.Count
	}
	for _, it := range items {
		p.Titles = append(p.Titles, itemTitle(it))
	}
	return p
}

// Jobs returns every job with its items, newest first.
func (r *Ripper) Jobs(ctx context.Context) (model.RipJobs, error) {
	jobs, err := r.ds.RipJob(ctx).GetAll()
	if err != nil || len(jobs) == 0 {
		return jobs, err
	}
	ids := make([]string, len(jobs))
	byID := make(map[string]*model.RipJob, len(jobs))
	for i := range jobs {
		ids[i] = jobs[i].ID
		jobs[i].Items = model.RipItems{}
		byID[jobs[i].ID] = &jobs[i]
	}
	items, err := r.ds.RipItem(ctx).ForJobs(ids...)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if j := byID[it.JobID]; j != nil {
			j.Items = append(j.Items, it)
		}
	}
	return jobs, nil
}

func validURL(s string) bool {
	if len(s) > 2048 {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// Run works through the queue until ctx ends. Jobs and items that were downloading when the app
// last stopped go back in the queue first.
func (r *Ripper) Run(ctx context.Context) {
	if n, err := r.ds.RipJob(ctx).RequeueInterrupted(); err != nil {
		log.Error(ctx, "Could not re-queue interrupted Rips", err)
	} else if n > 0 {
		log.Info(ctx, "Re-queued Rips interrupted by the last stop", "count", n)
	}
	if _, err := r.ds.RipItem(ctx).RequeueInterrupted(); err != nil {
		log.Error(ctx, "Could not re-queue interrupted Rip items", err)
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

// process rips the job's items that are still to do, one at a time, then scans the Inbox once.
// A failed item doesn't stop the others. The job ends ready if any item landed (its Error says how
// many failed), and failed if none did.
func (r *Ripper) process(ctx context.Context, job *model.RipJob) {
	if ctx.Err() != nil {
		return
	}
	// The job stays queued until its first item gets hold of yt-dlp (see processItem), so it
	// doesn't show as downloading while a yt-dlp update runs.
	log.Info(ctx, "Ripping", "id", job.ID, "url", job.URL)

	items, err := r.ds.RipItem(ctx).ForJobs(job.ID)
	if err != nil {
		log.Error(ctx, "Could not read the Rip's items; it is tried again on the next start", "id", job.ID, err)
		return
	}
	inbox, err := core.FindInboxLibrary(ctx, r.ds)
	if err == nil && inbox == nil {
		err = ErrNoInbox
	}
	if err == nil {
		err = r.claimFolder(ctx, inbox.Path, job, items)
	}
	for i := range items {
		item := &items[i]
		if item.Status != model.RipQueued && item.Status != model.RipDownloading {
			continue
		}
		if err != nil {
			item.Status, item.Error = model.RipFailed, err.Error()
			r.saveItem(ctx, item)
			continue
		}
		r.processItem(ctx, inbox.Path, job, item)
		if ctx.Err() != nil {
			// The app is stopping. The job and item stay "downloading", so the next start rips the
			// item again, and skips the ones already in the Inbox.
			log.Info(ctx, "Rip interrupted; it goes on from this item on the next start", "id", job.ID, "item", item.Position)
			return
		}
	}

	if err != nil {
		job.Status, job.Error = model.RipFailed, err.Error()
		log.Warn(ctx, "Rip failed", "id", job.ID, "url", job.URL, "error", job.Error)
		r.save(ctx, job)
		return
	}

	var ready, failed []model.RipItem
	for _, it := range items {
		switch it.Status {
		case model.RipReady:
			ready = append(ready, it)
		case model.RipFailed:
			failed = append(failed, it)
		}
	}
	switch {
	case len(ready) == 0 && len(items) == 1:
		job.Status, job.Error = model.RipFailed, items[0].Error
	case len(ready) == 0:
		job.Status, job.Error = model.RipFailed, fmt.Sprintf("none of the %d Tracks could be ripped", len(items))
	case len(failed) > 0:
		job.Error = fmt.Sprintf("%d of %d Tracks failed", len(failed), len(items))
	default:
		job.Error = ""
	}
	if job.Status == model.RipFailed {
		log.Warn(ctx, "Rip failed", "id", job.ID, "url", job.URL, "error", job.Error)
		r.save(ctx, job)
		return
	}

	// Every File is in the Inbox now, so the scan is not bound by the items' timeout.
	if err := r.scanInbox(ctx, inbox.ID); err != nil {
		if ctx.Err() != nil {
			log.Info(ctx, "Rip interrupted before its Inbox scan; it is scanned on the next start", "id", job.ID)
			return
		}
		note := fmt.Sprintf("saved, but the Inbox scan failed (%s); the Rip shows up after the next scan", err)
		job.Error = strings.TrimPrefix(job.Error+"; "+note, "; ")
		log.Warn(ctx, "Rip saved, but the Inbox scan failed", "id", job.ID, err)
	} else {
		log.Info(ctx, "Rip ready for review", "id", job.ID, "tracks", len(ready), "failed", len(failed))
	}
	job.Status = model.RipReady
	r.save(ctx, job)
}

// claimFolder gives a playlist job an Inbox folder of its own before its first File lands, adding
// " (2)", " (3)"... to its name if a folder with Files in it is already there (another playlist
// of the same name, or the same album ripped again). An empty folder is reused, as it is this
// job's own after a restart.
func (r *Ripper) claimFolder(ctx context.Context, inboxPath string, job *model.RipJob, items model.RipItems) error {
	if job.Folder == "" || slices.ContainsFunc(items, func(it model.RipItem) bool { return it.Path != "" }) {
		return nil
	}
	base := job.Folder
	for i := 1; i < 1000; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s (%d)", base, i)
		}
		dir := filepath.Join(inboxPath, name)
		if err := os.Mkdir(dir, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
			if name != job.Folder {
				job.Folder = name
				r.save(ctx, job)
			}
			return nil
		}
	}
	return fmt.Errorf("too many folders named %q in the Inbox", base)
}

// processItem rips one item into the Inbox at inboxPath, and leaves it ready or failed (or, if
// the app is stopping, downloading). It holds yt-dlp for this one item, retry included, so an
// update (scheduled or from the button) can run between two items of a long playlist. The item,
// and the job, show as downloading only once they hold it.
func (r *Ripper) processItem(ctx context.Context, inboxPath string, job *model.RipJob, item *model.RipItem) {
	if inInbox(inboxPath, item) {
		// It landed in a run the app stopped before saving the item as ready.
		item.Status = model.RipReady
		r.saveItem(ctx, item)
		return
	}
	if err := r.lockYtDlp(ctx, true); err != nil {
		return // the app is stopping
	}
	defer r.unlockYtDlp()
	if job.Status != model.RipDownloading {
		job.Status = model.RipDownloading
		r.save(ctx, job)
	}
	item.Status, item.Error = model.RipDownloading, ""
	r.saveItem(ctx, item)

	err := r.ripWithRetry(ctx, inboxPath, job, item)
	switch {
	case ctx.Err() != nil:
		return
	case err != nil:
		item.Status, item.Error = model.RipFailed, err.Error()
		log.Warn(ctx, "Rip item failed", "id", job.ID, "item", item.Position, "url", item.URL, "error", item.Error)
	default:
		item.Status = model.RipReady
	}
	r.saveItem(ctx, item)
}

// inInbox reports whether the item's File already landed in the Inbox.
func inInbox(inboxPath string, item *model.RipItem) bool {
	if item.Path == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(inboxPath, item.Path))
	return err == nil
}

// ripWithRetry rips one item; the caller holds yt-dlp. When yt-dlp fails in a way a newer yt-dlp
// might fix (see UpdateMightFix), it updates yt-dlp and tries the item once more, even if the
// update fails or finds yt-dlp already current, as the failure may have been passing. Each try
// gets its own Options.Timeout.
func (r *Ripper) ripWithRetry(ctx context.Context, inboxPath string, job *model.RipJob, item *model.RipItem) error {
	err := r.ripTimed(ctx, inboxPath, job, item)
	if err == nil || r.opts.Updater == nil || ctx.Err() != nil || !UpdateMightFix(err) {
		return err
	}
	log.Warn(ctx, "Rip failed; updating yt-dlp and trying again", "id", job.ID, "item", item.Position, "error", err)
	res, updErr := r.opts.Updater.Update(ctx)
	if updErr != nil {
		log.Warn(ctx, "Could not update yt-dlp; trying the Rip again with the one installed", "id", job.ID, updErr)
	} else {
		log.Info(ctx, "yt-dlp checked before trying the Rip again", "id", job.ID, "version", res.Version, "updated", res.Updated)
	}
	if ctx.Err() != nil {
		return err
	}
	err = r.ripTimed(ctx, inboxPath, job, item)
	if err != nil && updErr != nil {
		err = fmt.Errorf("%w (updating yt-dlp failed too: %v)", err, updErr)
	}
	return err
}

// ripTimed is rip, bounded by the Rip's timeout.
func (r *Ripper) ripTimed(ctx context.Context, inboxPath string, job *model.RipJob, item *model.RipItem) error {
	itemCtx, cancel := context.WithTimeout(ctx, r.opts.Timeout)
	defer cancel()
	err := r.rip(itemCtx, inboxPath, job, item)
	if err != nil && errors.Is(itemCtx.Err(), context.DeadlineExceeded) {
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
	r.updating.Store(true)
	defer r.updating.Store(false)
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
			if r.updating.Load() {
				return ErrUpdateRunning
			}
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

// rip fetches the item's audio into the Inbox folder at inboxPath (inside the job's Folder, if
// any), and records the File's path in the item as soon as it is there.
func (r *Ripper) rip(ctx context.Context, inboxPath string, job *model.RipJob, item *model.RipItem) error {
	work := filepath.Join(inboxPath, workDirName, item.ID)
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

	info, err := r.fetcher.Probe(ctx, item.URL)
	if err != nil {
		return err
	}
	if info.IsPlaylist {
		if job.Folder == "" && item.URL == job.URL {
			// Queued as one video because the page couldn't be read then (see Enqueue).
			return errors.New(errLatePlaylist)
		}
		return errors.New(errNestedPlaylist)
	}
	item.Title, item.Artist = info.Title, info.Artist
	r.saveItem(ctx, item)
	if job.Folder == "" && (job.Title != info.Title || job.Source != info.Source) {
		// A single video: the job is named after it.
		job.Title, job.Source = info.Title, info.Source
		r.save(ctx, job)
	}

	file, err := r.fetcher.Download(ctx, item.URL, work)
	if err != nil {
		return err
	}
	if err := r.opts.Tagger(file, info); err != nil {
		return err
	}
	dir := filepath.Join(inboxPath, job.Folder)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	dest, err := moveIntoInbox(file, dir, fileName(info, filepath.Ext(file)))
	if err != nil {
		return err
	}
	item.Path = filepath.ToSlash(filepath.Join(job.Folder, filepath.Base(dest)))
	r.saveItem(context.WithoutCancel(ctx), item)
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

func (r *Ripper) saveItem(ctx context.Context, item *model.RipItem) {
	item.UpdatedAt = time.Now()
	if err := r.ds.RipItem(ctx).Put(item); err != nil {
		log.Error(ctx, "Could not save the Rip item", "id", item.JobID, "item", item.Position, err)
	}
}

// moveIntoInbox moves the finished File into the Inbox folder dir under name, adding " (2)",
// " (3)"... to the name rather than replacing a File already there.
func moveIntoInbox(file, dir, name string) (string, error) {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; i < 1000; i++ {
		candidate := name
		if i > 1 {
			candidate = fmt.Sprintf("%s (%d)%s", base, i, ext)
		}
		dest := filepath.Join(dir, candidate)
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
	name = truncate(name, 160)
	if id := sanitize(info.ID); id != "" {
		name += " [" + id + "]"
	}
	name = strings.TrimLeft(strings.TrimSpace(name), ". -")
	return cmp.Or(name, "Rip") + strings.ToLower(ext)
}

// folderName names a playlist's Inbox subfolder after its title, else its id, safe for any disk.
func folderName(info *Info) string {
	for _, s := range []string{info.Title, info.ID} {
		name := strings.TrimLeft(truncate(sanitize(s), 120), ". -")
		if name != "" {
			return name
		}
	}
	return "Playlist"
}

func truncate(s string, n int) string {
	if len(s) > n {
		s = strings.ToValidUTF8(s[:n], "")
	}
	return strings.TrimRight(s, " .")
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
