package rip

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/rip/propose"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// Proposer works out the proposed tags for one Track. *propose.Engine implements it.
type Proposer interface {
	Propose(ctx context.Context, in propose.Input) model.RipProposal
}

// proposalQueue holds the items waiting for their proposal, in order, each once.
type proposalQueue struct {
	mu     sync.Mutex
	items  []queuedProposal
	queued map[string]bool
	wake   chan struct{}
}

type queuedProposal struct{ jobID, itemID string }

// queueProposal marks the item's proposal as pending, keeping the old one on show meanwhile, and
// queues it for the proposal worker (see runProposals). It does nothing without a Proposer, or for
// an item without a File.
func (r *Ripper) queueProposal(ctx context.Context, item *model.RipItem) {
	if r.opts.Proposer == nil || item.Status != model.RipReady || item.Path == "" {
		return
	}
	q := &r.proposals
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.queued[item.ID] {
		return
	}
	p := item.Proposal
	p.Status = model.RipProposalPending
	if err := r.ds.RipItem(ctx).SetProposal(item.ID, p); err != nil {
		if !errors.Is(err, model.ErrNotFound) {
			log.Error(ctx, "Could not queue a Rip's tag proposal", "id", item.JobID, "item", item.Position, err)
		}
		return
	}
	item.Proposal = p
	q.queued[item.ID] = true
	q.items = append(q.items, queuedProposal{jobID: item.JobID, itemID: item.ID})
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *proposalQueue) next() (queuedProposal, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return queuedProposal{}, false
	}
	next := q.items[0]
	q.items = q.items[1:]
	delete(q.queued, next.itemID)
	return next, true
}

// runProposals works out the queued proposals one at a time, until ctx ends. It runs beside the
// Rips, never in their way: a slow MusicBrainz only delays the proposals. It first queues the
// proposals a stop left pending, or that were never worked out.
func (r *Ripper) runProposals(ctx context.Context) {
	r.queuePendingProposals(ctx)
	for {
		next, ok := r.proposals.next()
		if !ok {
			select {
			case <-ctx.Done():
				return
			case <-r.proposals.wake:
				continue
			}
		}
		r.propose(ctx, next)
		if ctx.Err() != nil {
			return
		}
	}
}

func (r *Ripper) propose(ctx context.Context, next queuedProposal) {
	items, err := r.ds.RipItem(ctx).ForJobs(next.jobID)
	if err != nil {
		log.Error(ctx, "Could not read a Rip item to propose its tags", "id", next.jobID, err)
		return
	}
	for _, it := range items {
		if it.ID != next.itemID || it.Status != model.RipReady || it.Path == "" {
			continue
		}
		p := r.opts.Proposer.Propose(ctx, r.proposalInput(ctx, it))
		if ctx.Err() != nil {
			return // still pending: the next start works it out
		}
		if p.UpdatedAt.IsZero() {
			p.UpdatedAt = time.Now()
		}
		// Under the queue's lock, so a refresh can't slip in between: an item asked for again
		// while this ran stays pending, with these values on show, until its queued run is done.
		q := &r.proposals
		q.mu.Lock()
		if q.queued[it.ID] {
			p.Status = model.RipProposalPending
		}
		err := r.ds.RipItem(ctx).SetProposal(it.ID, p)
		q.mu.Unlock()
		switch {
		case errors.Is(err, model.ErrNotFound):
			// Discarded meanwhile.
		case err != nil:
			log.Error(ctx, "Could not save a Rip's tag proposal", "id", it.JobID, "item", it.Position, err)
		default:
			log.Debug(ctx, "Proposed tags for a Rip", "id", it.JobID, "item", it.Position, "album", p.Album,
				"albumFrom", p.AlbumFrom, "note", p.Note)
		}
	}
}

// queuePendingProposals queues the proposals of every ready item that has none yet, whose
// proposal a stop left pending, or whose MusicBrainz lookup failed (it was busy or unreachable).
func (r *Ripper) queuePendingProposals(ctx context.Context) {
	if r.opts.Proposer == nil {
		return
	}
	jobs, err := r.Jobs(ctx)
	if err != nil {
		log.Error(ctx, "Could not look for Rips waiting for tag proposals", err)
		return
	}
	for _, job := range jobs {
		for i := range job.Items {
			p := job.Items[i].Proposal
			if p.Status == model.RipProposalNone || p.Status == model.RipProposalPending || p.LookupFailed {
				r.queueProposal(ctx, &job.Items[i])
			}
		}
	}
}

// RefreshProposals works the proposals of the job's Tracks out again, in the background. The old
// ones stay on show, marked pending, until the new ones are there.
func (r *Ripper) RefreshProposals(ctx context.Context, jobID string) error {
	job, _, err := r.reviewable(ctx, jobID)
	if err != nil {
		return err
	}
	for i := range job.Items {
		r.queueProposal(ctx, &job.Items[i])
	}
	return nil
}

// proposalInput is what the item's proposal is worked out from: for a Rip, what yt-dlp said
// (ProposalInput); for an uploaded or dropped File (#35), the File's own tags and length, or its
// name (see fileInput). One whose File can't be read falls back to the item's title and artist.
func (r *Ripper) proposalInput(ctx context.Context, it model.RipItem) propose.Input {
	job, err := r.ds.RipJob(ctx).Get(it.JobID)
	if err != nil || job.Kind == model.RipKindRip {
		return ProposalInput(it)
	}
	inbox, err := core.FindInboxLibrary(ctx, r.ds)
	if err == nil && inbox == nil {
		err = ErrNoInbox
	}
	var full string
	if err == nil {
		full, err = inboxPath(inbox.Path, it.Path)
	}
	var in propose.Input
	if err == nil {
		in, err = fileInput(full)
	}
	if err != nil {
		log.Warn(ctx, "Could not read a File's tags to propose its tags; using its name", "id", it.JobID, "path", it.Path, err)
		return propose.Input{Artist: it.Artist, Title: it.Title, Tagged: it.Artist != ""}
	}
	return in
}

// ProposalInput is what a Rip item's proposal is worked out from: what yt-dlp said about its
// video (YouTube Music's metadata, for a Topic upload), or, for an item ripped before that was
// kept, its title and artist.
func ProposalInput(it model.RipItem) propose.Input {
	o := it.Origin
	in := propose.Input{Artist: o.Uploader, Title: o.Title,
		Duration: time.Duration(o.Duration * float64(time.Second))}
	if o.Title == "" {
		in.Artist, in.Title = it.Artist, it.Title
	}
	if o.Track != "" && o.Artist != "" {
		in.YouTubeMusic = &propose.YouTubeMusic{Artist: o.Artist, Artists: o.Artists, Track: o.Track,
			Album: o.Album, Year: o.ReleaseYear}
	}
	return in
}
