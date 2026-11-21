package rip

import (
	"context"
	"sync"
	"time"

	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"golang.org/x/sync/errgroup"
)

// Matcher finds the Tracks the Library and the Inbox already seem to have for a video, among
// those the user in ctx can see. *match.Matcher implements it.
type Matcher interface {
	AlreadyHave(ctx context.Context, artist, title string) (model.RipMatches, error)
}

// PreviewItem is an item the Library already seems to have, with the Tracks that look like it.
type PreviewItem struct {
	// Position is the item's place in the playlist, from 1.
	Position int `json:"position"`
	// Title is the item's "<Artist> - <Title>".
	Title  string           `json:"title"`
	Tracks model.RipMatches `json:"tracks"`
}

const (
	// matchWorkers is how many items are matched at once. It leaves database connections free
	// for the rest of the app.
	matchWorkers = 3
	// matchesKeptFor is how long a preview's matches are kept for the admin's confirmation.
	matchesKeptFor = 10 * time.Minute
)

// previewMatches keeps the matches of the previews the admin was asked to confirm, by user and
// URL, so confirming doesn't search the Library for every item again.
type previewMatches struct {
	mu      sync.Mutex
	entries map[string]keptMatches
}

type keptMatches struct {
	at      time.Time
	matches []model.RipMatches
}

func (p *previewMatches) put(key string, matches []model.RipMatches) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.entries == nil {
		p.entries = map[string]keptMatches{}
	}
	now := time.Now()
	for k, e := range p.entries {
		if now.Sub(e.at) > matchesKeptFor {
			delete(p.entries, k)
		}
	}
	p.entries[key] = keptMatches{at: now, matches: matches}
}

// take returns, and forgets, the matches kept for key, if they are recent and for n items.
func (p *previewMatches) take(key string, n int) ([]model.RipMatches, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.entries[key]
	delete(p.entries, key)
	if !ok || time.Since(e.at) > matchesKeptFor || len(e.matches) != n {
		return nil, false
	}
	return e.matches, true
}

// alreadyHave matches each item of the page at rawURL against the Library and the Inbox, keeps
// the matches on the item, and returns the items that have some. When the admin is to confirm
// (not confirmed, and big or with matches), the matches are kept for the confirmation; once
// confirmed, the kept ones are used, if any. As it is only a warning, items that can't be
// matched are left without matches.
func (r *Ripper) alreadyHave(ctx context.Context, rawURL string, items model.RipItems, confirmed, big bool) []PreviewItem {
	if r.opts.Matcher == nil {
		return []PreviewItem{}
	}
	user, _ := request.UserFrom(ctx)
	key := user.ID + " " + rawURL
	matches, kept := []model.RipMatches(nil), false
	if confirmed {
		matches, kept = r.previews.take(key, len(items))
	}
	if !kept {
		matches = r.match(ctx, items)
	}

	res := []PreviewItem{}
	for i := range items {
		if len(matches[i]) > 0 {
			items[i].AlreadyHave = matches[i]
			res = append(res, PreviewItem{Position: items[i].Position, Title: itemTitle(items[i]), Tracks: matches[i]})
		}
	}
	if !confirmed && (big || len(res) > 0) {
		r.previews.put(key, matches)
	}
	return res
}

// match matches the items, a few at a time. It stops at the first error, which it logs.
func (r *Ripper) match(ctx context.Context, items model.RipItems) []model.RipMatches {
	res := make([]model.RipMatches, len(items))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(matchWorkers)
	for i, it := range items {
		if it.Status == model.RipFailed || it.Title == "" {
			continue
		}
		g.Go(func() error {
			if gctx.Err() != nil {
				return nil
			}
			have, err := r.opts.Matcher.AlreadyHave(gctx, it.Artist, it.Title)
			res[i] = have
			return err
		})
	}
	if err := g.Wait(); err != nil {
		log.Warn(ctx, "Could not look for the Rip in the Library; queuing it without the full warning", err)
	}
	return res
}

// itemTitle is the item's "<Artist> - <Title>", or its title alone when it has no artist.
func itemTitle(it model.RipItem) string {
	if it.Artist != "" {
		return it.Artist + " - " + it.Title
	}
	return it.Title
}
