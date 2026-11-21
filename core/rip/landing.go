package rip

import (
	"context"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/navidrome/navidrome/core/rip/propose"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"go.senan.xyz/taglib"
	"golang.org/x/sync/errgroup"
)

// fileInput is what an uploaded or dropped File's proposal (#35) is worked out from: its own
// artist, title, album and year tags and its length. A File with no title tag is read from its
// name instead ("Artist - Title.mp3", without a leading track number): name, as path may be a
// temp File's.
func fileInput(path, name string) (propose.Input, error) {
	tags, err := taglib.ReadTags(path)
	if err != nil {
		return propose.Input{}, err
	}
	ft := fileTags(tags)
	in := propose.Input{Artist: ft.Artist, Title: ft.Title, Album: ft.Album}
	in.Year, _ = strconv.Atoi(ft.Year)
	if props, err := taglib.ReadProperties(path); err == nil {
		in.Duration = props.Length
	}
	if strings.TrimSpace(in.Title) == "" {
		in.Title = titleFromName(name)
	} else {
		in.Tagged = true
	}
	return in, nil
}

// trackNumberPrefix is a track number a File's name starts with: "01 ", "1. ", "03 - ", "07_".
var trackNumberPrefix = regexp.MustCompile(`^\d{1,3}(?:\s*[-._]\s*|\s+)`)

// titleFromName is a File's name as a title: without its extension or a leading track number,
// with underscores read as spaces.
func titleFromName(path string) string {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	name = strings.TrimSpace(strings.ReplaceAll(name, "_", " "))
	if t := strings.TrimSpace(trackNumberPrefix.ReplaceAllString(name, "")); t != "" {
		name = t
	}
	return name
}

// landed is what follows a batch of uploaded or dropped Files (#35) joining a job, as for a Rip's:
// their "already have" matches, then their tag proposals in the background. ins are the items'
// inputs, by item ID. The Files' own Tracks in the Inbox are never matches of themselves.
func (r *Ripper) landed(ctx context.Context, inbox *model.Library, items model.RipItems) {
	r.matchLanded(ctx, inbox, items)
	for i := range items {
		r.queueProposal(ctx, &items[i])
	}
}

// matchLanded finds and saves the Tracks the Library or the Inbox already seem to have for each
// item, a few at a time, leaving out the Track scanned from the item's own File.
func (r *Ripper) matchLanded(ctx context.Context, inbox *model.Library, items model.RipItems) {
	if r.opts.Matcher == nil {
		return
	}
	own := r.tracksByPath(ctx, inbox.ID, items)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(matchWorkers)
	for i := range items {
		it := &items[i]
		if it.Title == "" {
			continue
		}
		g.Go(func() error {
			have, err := r.opts.Matcher.AlreadyHave(gctx, it.Artist, it.Title)
			if err != nil {
				return err
			}
			have = withoutTrack(have, own[it.Path])
			if len(have) == 0 {
				return nil
			}
			it.AlreadyHave = have
			return r.ds.RipItem(gctx).SetAlreadyHave(it.ID, have)
		})
	}
	if err := g.Wait(); err != nil {
		log.Warn(ctx, "Could not look for new Inbox Files in the Library; they are shown without the full warning", err)
	}
}

func withoutTrack(have model.RipMatches, id string) model.RipMatches {
	if id == "" {
		return have
	}
	res := model.RipMatches{}
	for _, m := range have {
		if m.ID != id {
			res = append(res, m)
		}
	}
	return res
}

// newFileItem is a ready item for a File that is in the Inbox at rel already, named for the
// review list after its proposal's starting point.
func newFileItem(jobID string, pos int, rel string, in propose.Input, now time.Time) model.RipItem {
	s := propose.StartFrom(in)
	return model.RipItem{JobID: jobID, Position: pos, Status: model.RipReady, Path: rel,
		Title: s.Title, Artist: s.Artist, CreatedAt: now, UpdatedAt: now}
}
