package rip

import (
	"cmp"
	"context"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/core/rip/propose"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
)

// dropState is what the drop reconciler remembers between two passes: how each File it may adopt
// looked when it was first seen like that, so it can tell when the File stopped changing.
type dropState struct {
	mu   sync.Mutex
	seen map[string]fileStamp
	// wake is sent to after each scan (see InboxScanned).
	wake chan struct{}
}

// fileStamp is a File's size and modification time, and since when it has had them.
// rescanned is when its folder was rescanned in full because its Track was scanned at another
// size (zero if it wasn't).
type fileStamp struct {
	size      int64
	mod       time.Time
	since     time.Time
	rescanned time.Time
}

// dropCheckIdle is how often the reconciler looks at the Inbox with nothing waiting to settle,
// besides after every scan.
const dropCheckIdle = 5 * time.Minute

// RunInbox works out the queued tag proposals and adopts the Files dropped into the Inbox (see
// ReconcileDrops) until ctx ends. Unlike Run, it runs whether ripping is on or not: uploads and
// dropped Files aren't Rips.
func (r *Ripper) RunInbox(ctx context.Context) {
	go r.runProposals(ctx)
	for ctx.Err() == nil {
		wait := dropCheckIdle
		pending, err := false, error(nil)
		if !r.opts.IgnoreDrops {
			pending, err = r.ReconcileDrops(ctx)
		}
		if err != nil {
			log.Error(ctx, "Could not look for Files dropped into the Inbox", err)
		}
		if pending {
			wait = max(r.opts.DropSettle/2, 100*time.Millisecond)
		}
		select {
		case <-ctx.Done():
			return
		case <-r.drops.wake:
		case <-time.After(wait):
		}
	}
}

// InboxScanned tells the drop reconciler a scan has finished, so it looks for new Files.
func (r *Ripper) InboxScanned() {
	select {
	case r.drops.wake <- struct{}{}:
	default:
	}
}

// ReconcileDrops adopts the Files someone copied straight into the Inbox folder (#35): the
// Inbox's Tracks no job knows about. They are grouped by the top-level folder they are in, one
// card (a drop job) per folder, and a File dropped loose in the Inbox gets a card of its own, as
// a single video's Rip does. A File joins the card of its folder if there is one already.
//
// A File is adopted only once it has stopped changing: its size and modification time have
// stayed the same for Options.DropSettle, its Track was scanned at its full size, and its folder
// has had no File added or renamed in it for as long. A whole folder is adopted at once, when
// all its Files are. Hidden Files and folders (the temp Files of Rips, uploads and tag saves,
// and copies under way by tools such as rsync) are never adopted, nor is any File a job knows.
// Running it again adopts nothing twice. It reports whether some File is waiting to settle.
func (r *Ripper) ReconcileDrops(ctx context.Context) (bool, error) {
	inbox, err := core.FindInboxLibrary(ctx, r.ds)
	if err != nil || inbox == nil {
		return false, err
	}
	ctx = auth.WithAdminUser(ctx, r.ds)

	groups, pending, err := r.settledDrops(ctx, inbox)
	if err != nil || len(groups) == 0 {
		return pending, err
	}
	// The Files' tags are read before the locks are taken, as a big drop takes a while.
	prepared := make([]model.RipItems, len(groups))
	for i, g := range groups {
		prepared[i] = r.dropItems(ctx, inbox, g)
	}

	// Under the landing lock no Rip or upload can be between its File landing and its item being
	// saved, so the Files some job knows are all known; under the review lock no Discard or
	// Promote of the folder's card can be halfway.
	r.landing.Lock()
	r.reviewMu.Lock()
	var adopted []model.RipItems
	known, err := r.knownPaths(ctx)
	for i := 0; err == nil && i < len(groups); i++ {
		items := slices.DeleteFunc(prepared[i], func(it model.RipItem) bool { return known[it.Path] })
		if len(items) == 0 {
			continue
		}
		if items, err = r.adopt(ctx, groups[i].folder, items); err == nil {
			adopted = append(adopted, items)
		}
	}
	r.reviewMu.Unlock()
	r.landing.Unlock()

	for _, items := range adopted {
		r.landed(ctx, inbox, items)
	}
	return pending, err
}

func (r *Ripper) knownPaths(ctx context.Context) (map[string]bool, error) {
	known, err := r.ds.RipItem(ctx).Paths()
	res := make(map[string]bool, len(known))
	for _, p := range known {
		res[p] = true
	}
	return res, err
}

// dropGroup is the Files of one top-level Inbox folder (or one loose File) to adopt together.
type dropGroup struct {
	// folder is the top-level folder, "" for a loose File.
	folder string
	tracks model.MediaFiles
}

// settledDrops lists the groups of dropped Files that are ready to adopt, and reports whether
// some other File is still settling.
func (r *Ripper) settledDrops(ctx context.Context, inbox *model.Library) ([]dropGroup, bool, error) {
	isKnown, err := r.knownPaths(ctx)
	if err != nil {
		return nil, false, err
	}
	mfs, err := r.ds.MediaFile(ctx).GetAll(model.QueryOptions{Filters: squirrel.And{
		squirrel.Eq{"media_file.library_id": inbox.ID},
		squirrel.Eq{"media_file.missing": false},
	}})
	if err != nil {
		return nil, false, err
	}

	now := time.Now()
	settle := r.opts.DropSettle
	d := &r.drops
	d.mu.Lock()
	defer d.mu.Unlock()
	stillThere := map[string]bool{}
	byKey := map[string]*dropGroup{}
	busy := map[string]bool{}
	var keys []string
	for _, mf := range mfs {
		if mf.LibraryID != inbox.ID || mf.Missing || isKnown[mf.Path] || hiddenPath(mf.Path) {
			continue
		}
		full, err := inboxPath(inbox.Path, mf.Path)
		if err != nil {
			continue
		}
		info, err := os.Stat(full)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		folder, key := topFolder(mf.Path), mf.Path
		if folder != "" {
			key = folder + "/"
		}
		if byKey[key] == nil {
			byKey[key] = &dropGroup{folder: folder}
			keys = append(keys, key)
		}
		byKey[key].tracks = append(byKey[key].tracks, mf)

		stillThere[mf.Path] = true
		st := fileStamp{size: info.Size(), mod: info.ModTime(), since: now}
		prev, ok := d.seen[mf.Path]
		if !ok || prev.size != st.size || !prev.mod.Equal(st.mod) {
			d.seen[mf.Path] = st
			busy[key] = true
			continue
		}
		if now.Sub(prev.since) < settle || now.Sub(st.mod) < settle {
			busy[key] = true
			continue
		}
		if mf.Size != st.size {
			// Scanned while it was still being copied. A quick scan may never read it again (a
			// copy that keeps the original's time looks older than its Track), so its folder is
			// scanned in full, once; if its Track still differs after that, it is adopted anyway.
			switch {
			case prev.rescanned.IsZero():
				prev.rescanned = now
				d.seen[mf.Path] = prev
				busy[key] = true
				r.rescanFolderSoon(ctx, inbox.ID, path.Dir(mf.Path))
			case now.Sub(prev.rescanned) < settle:
				busy[key] = true
			default:
				log.Warn(ctx, "A dropped File's Track still has another size after a full rescan; adopting it anyway", "path", mf.Path)
			}
		}
	}
	for p := range d.seen {
		if !stillThere[p] {
			delete(d.seen, p)
		}
	}

	var ready []dropGroup
	for _, key := range keys {
		g := byKey[key]
		if !busy[key] && g.folder != "" {
			// A File still being added to the folder may not even be scanned yet.
			info, err := os.Stat(filepath.Join(inbox.Path, g.folder))
			busy[key] = err != nil || now.Sub(info.ModTime()) < settle
		}
		if busy[key] {
			continue
		}
		for _, mf := range g.tracks {
			delete(d.seen, mf.Path)
		}
		ready = append(ready, *g)
	}
	pending := len(d.seen) > 0 || len(ready) < len(keys)
	return ready, pending, nil
}

// dropItems are the new items for the group's Files, in track order, named after their tags.
// They have no job or position yet.
func (r *Ripper) dropItems(ctx context.Context, inbox *model.Library, g dropGroup) model.RipItems {
	slices.SortFunc(g.tracks, func(a, b model.MediaFile) int {
		return cmp.Or(cmp.Compare(a.DiscNumber, b.DiscNumber), cmp.Compare(a.TrackNumber, b.TrackNumber),
			strings.Compare(a.Path, b.Path))
	})
	now := time.Now()
	var items model.RipItems
	for _, mf := range g.tracks {
		full, err := inboxPath(inbox.Path, mf.Path)
		if err != nil {
			continue
		}
		in, err := fileInput(full, path.Base(mf.Path))
		if err != nil {
			log.Warn(ctx, "Could not read the tags of a File dropped into the Inbox; naming it after its Track", "path", mf.Path, err)
			in = propose.Input{Artist: mf.Artist, Title: mf.Title}
		}
		items = append(items, newFileItem("", 0, mf.Path, in, now))
	}
	return items
}

// adopt gives the items a drop job: their folder's, if one is there already, else a new one, and
// saves them. The caller holds the landing and review locks.
func (r *Ripper) adopt(ctx context.Context, folder string, items model.RipItems) (model.RipItems, error) {
	now := time.Now()
	var job *model.RipJob
	var existing model.RipItems
	if folder != "" {
		jobs, err := r.ds.RipJob(ctx).GetAll()
		if err != nil {
			return nil, err
		}
		for i := range jobs {
			if jobs[i].Kind == model.RipKindDrop && jobs[i].Folder == folder {
				job = &jobs[i]
				break
			}
		}
		if job != nil {
			if existing, err = r.ds.RipItem(ctx).ForJobs(job.ID); err != nil {
				return nil, err
			}
		}
	}

	first := nextPosition(existing)
	for i := range items {
		items[i].Position = first + i
	}
	if job == nil {
		user, _ := request.UserFrom(ctx)
		job = &model.RipJob{Kind: model.RipKindDrop, UserID: user.ID, Status: model.RipReady,
			Folder: folder, Title: folder, CreatedAt: now, UpdatedAt: now}
		if folder == "" {
			job.Title = itemTitle(items[0])
		}
	}
	err := r.ds.WithTx(func(tx model.DataStore) error {
		job.UpdatedAt = now
		if err := tx.RipJob(ctx).Put(job); err != nil {
			return err
		}
		for i := range items {
			items[i].JobID = job.ID
			if err := tx.RipItem(ctx).Put(&items[i]); err != nil {
				return err
			}
		}
		return nil
	}, "rip adopt drop")
	if err != nil {
		return nil, err
	}
	log.Info(ctx, "Files dropped into the Inbox are ready for review", "id", job.ID, "folder", folder, "files", len(items))
	return items, nil
}

// topFolder is the top-level Inbox folder of rel, "" for a File loose in the Inbox.
func topFolder(rel string) string {
	if i := strings.IndexByte(rel, '/'); i > 0 {
		return rel[:i]
	}
	return ""
}

// hiddenPath reports whether any part of rel is hidden ("." first): the scanner's rule, which the
// temp Files and folders of Rips (.rips), uploads (.uploads) and tag saves follow.
func hiddenPath(rel string) bool {
	for part := range strings.SplitSeq(rel, "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

// rescanFolderSoon scans the Inbox folder dir (relative to the Inbox) in full, in the background,
// so its Files are read again whatever their time, waiting for any other scan to finish first.
func (r *Ripper) rescanFolderSoon(ctx context.Context, libID int, dir string) {
	if r.scanner == nil {
		return
	}
	ctx = context.WithoutCancel(ctx)
	go func() {
		target := model.ScanTarget{LibraryID: libID, FolderPath: dir}
		if err := r.scanWithRetryFull(ctx, true, target); err != nil {
			log.Warn(ctx, "Could not rescan a dropped folder", "folder", dir, err)
		}
	}()
}

// scanInboxSoon scans the Inbox in the background a moment from now, once for any number of
// calls meanwhile (a batch of uploads), waiting for any other scan to finish first.
func (r *Ripper) scanInboxSoon(ctx context.Context, libID int) {
	if r.scanner == nil || !r.scanPending.CompareAndSwap(false, true) {
		return
	}
	ctx = context.WithoutCancel(ctx)
	go func() {
		time.Sleep(r.opts.UploadScanDelay)
		r.scanPending.Store(false)
		if err := r.scanInbox(ctx, libID); err != nil {
			log.Warn(ctx, "Inbox scan after an upload failed; the Files show up after the next scan", err)
		}
	}()
}
