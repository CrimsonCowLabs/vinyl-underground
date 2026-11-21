package rip

import (
	"context"

	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
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

// alreadyHave matches each item against the Library and the Inbox, keeps the matches on the
// item, and returns the items that have some. As it is only a warning, an item that can't be
// matched is left without matches.
func (r *Ripper) alreadyHave(ctx context.Context, items model.RipItems) []PreviewItem {
	res := []PreviewItem{}
	if r.opts.Matcher == nil {
		return res
	}
	for i := range items {
		it := &items[i]
		if it.Status == model.RipFailed || it.Title == "" {
			continue
		}
		have, err := r.opts.Matcher.AlreadyHave(ctx, it.Artist, it.Title)
		if err != nil {
			log.Warn(ctx, "Could not look for the Rip in the Library; queuing it without the warning", "url", it.URL, err)
			continue
		}
		if len(have) > 0 {
			it.AlreadyHave = have
			res = append(res, PreviewItem{Position: it.Position, Title: itemTitle(*it), Tracks: have})
		}
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
