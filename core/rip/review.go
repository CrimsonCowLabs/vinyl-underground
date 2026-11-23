package rip

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// ErrNotReady is returned when the admin tries to review or Discard a job that is still queued
// or downloading: its Files are still landing.
var ErrNotReady = errors.New("this Rip is still running; review or discard it once it is ready")

// Review is a job's review card (#33): the album-level tags, and each of the job's Tracks with
// its own tags, read from the Files themselves. The card is driven by the job and its items only,
// so a job of uploaded or dropped Files (#35) gets the same card, and proposed tags (#34) can be
// dropped into the same fields.
type Review struct {
	JobID string `json:"jobId"`
	// Album, AlbumArtist and Year are the album-level fields, from the first Track with a File.
	Album       string        `json:"album"`
	AlbumArtist string        `json:"albumArtist"`
	Year        string        `json:"year"`
	Tracks      []ReviewTrack `json:"tracks"`
	// Proposal is the album-level tags proposed (#34): each field only when every Track with a
	// proposal agrees on it, as album-level edits go into every File. Nil when no Track has a
	// proposal worked out.
	Proposal *AlbumProposal `json:"proposal,omitempty"`
	// ProposalsPending is true while some Track's proposal is still being worked out.
	ProposalsPending bool `json:"proposalsPending"`
}

// AlbumProposal is the album-level part of the Tracks' proposals. An empty field has none.
type AlbumProposal struct {
	Album       string `json:"album,omitempty"`
	AlbumArtist string `json:"albumArtist,omitempty"`
	Year        string `json:"year,omitempty"`
}

// ReviewTrack is one item of a job on its review card. Only a ready item has a File, and so tags.
type ReviewTrack struct {
	ItemID   string          `json:"itemId"`
	Position int             `json:"position"`
	Status   model.RipStatus `json:"status"`
	Error    string          `json:"error,omitempty"`
	// Path is the File's, relative to the Inbox.
	Path string `json:"path,omitempty"`
	// MediaFileID is the Track scanned from the File, once the Inbox has been scanned.
	MediaFileID string `json:"mediaFileId,omitempty"`
	// AlreadyHave are the Tracks the Library already seemed to have when the item was queued.
	AlreadyHave model.RipMatches `json:"alreadyHave"`
	// Proposal is the tags proposed for the Track (#34), nil when it has none. Only the admin's
	// save writes anything into the File.
	Proposal *model.RipProposal `json:"proposal,omitempty"`
	FileTags
}

// ReviewEdits are the admin's changes on a review card. A nil field is left as it is. Album,
// AlbumArtist and Year go into every File of the job.
type ReviewEdits struct {
	Album       *string     `json:"album,omitempty"`
	AlbumArtist *string     `json:"albumArtist,omitempty"`
	Year        *string     `json:"year,omitempty"`
	Tracks      []TrackEdit `json:"tracks,omitempty"`
}

// TrackEdit is the admin's change to one Track.
type TrackEdit struct {
	ItemID      string  `json:"itemId"`
	Title       *string `json:"title,omitempty"`
	Artist      *string `json:"artist,omitempty"`
	TrackNumber *string `json:"trackNumber,omitempty"`
}

// reviewable loads a job that has finished, with its items, and the Inbox they are in. A job
// still queued or downloading is ErrNotReady.
func (r *Ripper) reviewable(ctx context.Context, jobID string) (*model.RipJob, *model.Library, error) {
	job, err := r.ds.RipJob(ctx).Get(jobID)
	if err != nil {
		return nil, nil, err
	}
	if job.Status == model.RipQueued || job.Status == model.RipDownloading {
		return nil, nil, ErrNotReady
	}
	if job.Items, err = r.ds.RipItem(ctx).ForJobs(job.ID); err != nil {
		return nil, nil, err
	}
	inbox, err := core.FindInboxLibrary(ctx, r.ds)
	if err == nil && inbox == nil {
		err = ErrNoInbox
	}
	if err != nil {
		return nil, nil, err
	}
	return job, inbox, nil
}

// Review returns the job's review card, with the tags read from its Files.
func (r *Ripper) Review(ctx context.Context, jobID string) (*Review, error) {
	job, inbox, err := r.reviewable(ctx, jobID)
	if err != nil {
		return nil, err
	}
	return r.review(ctx, job, inbox)
}

func (r *Ripper) review(ctx context.Context, job *model.RipJob, inbox *model.Library) (*Review, error) {
	rev := &Review{JobID: job.ID, Tracks: []ReviewTrack{}}
	tracks := r.tracksByPath(ctx, inbox.ID, job.Items)
	first := true
	for _, it := range job.Items {
		t := ReviewTrack{ItemID: it.ID, Position: it.Position, Status: it.Status, Error: it.Error,
			AlreadyHave: it.AlreadyHave}
		if it.Status == model.RipReady && it.Path != "" {
			tags, err := readItemTags(inbox.Path, it)
			if errors.Is(err, ErrNoInbox) {
				return nil, err
			}
			if err != nil {
				// One unreadable File doesn't hide the card: the Track can still be discarded.
				log.Warn(ctx, "Could not read the tags of a Rip's File", "id", job.ID, "path", it.Path, err)
				t.Error = errFileUnreadable
				rev.Tracks = append(rev.Tracks, t)
				continue
			}
			t.Path, t.FileTags = it.Path, tags
			t.MediaFileID = tracks[it.Path]
			if it.Proposal.Status != model.RipProposalNone {
				t.Proposal = &it.Proposal
			}
			if first {
				rev.Album, rev.AlbumArtist, rev.Year = t.Album, t.AlbumArtist, t.Year
				first = false
			}
		}
		rev.Tracks = append(rev.Tracks, t)
	}
	rev.Proposal, rev.ProposalsPending = albumProposal(rev.Tracks)
	return rev, nil
}

// albumProposal is the album-level proposal of the Tracks: each field the one value all the
// Tracks' proposals agree on, if they do.
func albumProposal(tracks []ReviewTrack) (*AlbumProposal, bool) {
	var res *AlbumProposal
	pending := false
	agree := func(have *string, v string) {
		if *have != v {
			*have = ""
		}
	}
	for _, t := range tracks {
		if t.Proposal != nil && t.Proposal.Status == model.RipProposalPending {
			pending = true
		}
		switch {
		case t.Proposal == nil:
		case t.Proposal.UpdatedAt.IsZero() && t.Proposal.Album == "" && t.Proposal.Title == "":
			// Pending for the first time: nothing to offer yet. A refreshed one keeps its old values.
		case res == nil:
			res = &AlbumProposal{Album: t.Proposal.Album, AlbumArtist: t.Proposal.AlbumArtist, Year: t.Proposal.Year}
		default:
			agree(&res.Album, t.Proposal.Album)
			agree(&res.AlbumArtist, t.Proposal.AlbumArtist)
			agree(&res.Year, t.Proposal.Year)
		}
	}
	return res, pending
}

// errFileUnreadable is a ready Track's error when its File can't be read (gone, or no longer
// audio), so it shows on the card instead of its tags.
const errFileUnreadable = "its File is missing from the Inbox or can't be read"

func readItemTags(root string, it model.RipItem) (FileTags, error) {
	path, err := inboxPath(root, it.Path)
	if err != nil {
		return FileTags{}, err
	}
	return readFileTags(path)
}

// tracksByPath maps the paths of the items' Files to the Tracks scanned from them. A File not
// scanned yet has none.
func (r *Ripper) tracksByPath(ctx context.Context, libID int, items model.RipItems) map[string]string {
	res := map[string]string{}
	var paths []string
	for _, it := range items {
		if it.Path != "" {
			paths = append(paths, it.Path)
		}
	}
	if len(paths) == 0 {
		return res
	}
	mfs, err := r.ds.MediaFile(ctx).GetAll(model.QueryOptions{Filters: squirrel.And{
		squirrel.Eq{"media_file.library_id": libID},
		squirrel.Eq{"media_file.path": paths},
		squirrel.Eq{"media_file.missing": false},
	}})
	if err != nil {
		log.Warn(ctx, "Could not look up the Tracks of a Rip", err)
		return res
	}
	for _, mf := range mfs {
		res[mf.Path] = mf.ID
	}
	return res
}

// SaveTags writes the admin's edits into the job's Files, then rescans the Inbox so the Tracks
// update. Every edit is checked before any File is written. Each File is written as a copy that
// replaces it only once the write succeeded (see editTags), so a failed write never leaves a File
// half-written. It returns the card as the Files now are.
func (r *Ripper) SaveTags(ctx context.Context, jobID string, edits ReviewEdits) (*Review, error) {
	r.reviewMu.Lock()
	defer r.reviewMu.Unlock()
	job, inbox, err := r.editable(ctx, jobID)
	if err != nil {
		return nil, err
	}

	byItem := map[string]TrackEdit{}
	for _, e := range edits.Tracks {
		byItem[e.ItemID] = e
	}
	type write struct {
		path string
		edit TagEdit
	}
	var writes []write
	for _, it := range job.Items {
		e, edited := byItem[it.ID]
		delete(byItem, it.ID)
		if it.Status != model.RipReady || it.Path == "" {
			if edited {
				return nil, fmt.Errorf("%w: Track %d has no File to tag", ErrInvalidTags, it.Position)
			}
			continue
		}
		edit := TagEdit{Album: edits.Album, AlbumArtist: edits.AlbumArtist, Year: edits.Year,
			Title: e.Title, Artist: e.Artist, TrackNumber: e.TrackNumber}
		if err := edit.validate(); err != nil {
			return nil, err
		}
		path, err := inboxPath(inbox.Path, it.Path)
		if err == nil {
			_, err = os.Stat(path)
		}
		if errors.Is(err, fs.ErrNotExist) && !edited {
			// Its File is gone (the card shows it): the album-level edits go to the others.
			continue
		}
		if err != nil {
			return nil, err
		}
		writes = append(writes, write{path, edit})
	}
	if len(byItem) > 0 {
		return nil, fmt.Errorf("%w: a Track edited is not in this Rip", model.ErrNotFound)
	}

	changed := 0
	var writeErr error
	for _, w := range writes {
		ok, err := editTags(w.path, w.edit, r.writeTags)
		if err != nil {
			writeErr = err
			break
		}
		if ok {
			changed++
		}
	}
	if changed > 0 {
		log.Info(ctx, "Saved the tags of a Rip", "id", job.ID, "files", changed)
		r.rescan(ctx, inbox.ID, nil)
	}
	if writeErr != nil {
		return nil, writeErr
	}
	return r.review(ctx, job, inbox)
}
