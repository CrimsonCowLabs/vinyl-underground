package stations

import (
	"context"
	"sync"
	"time"

	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// Between two Tracks, a Station shows a Blurb: a short on-screen text in its DJ's voice about
// what just played and what's next. It is never spoken.
//
// Blurbs come from the LLM, which is slow, so they are written ahead, in the background: each
// refill queues a Blurb for every Track change among the Tracks it served, which the web app
// asks for at least 2–3 Tracks ahead of what is playing. Nothing ever waits for one. A Blurb
// not written by the time its Track starts is simply not shown. Blurbs are only kept in memory,
// for each Listener's Station they last listened to, so a restart or a new Station forgets them.

const (
	// blurbTimeout is a backstop for writing one Blurb, so a stuck LLM call never holds up the
	// Blurbs behind it forever.
	blurbTimeout = 2 * time.Minute
	// keptBlurbs is how many written Blurbs each Listener keeps, newest first; a refill serves
	// at most MaxCount Tracks, so this covers a few refills.
	keptBlurbs = 3 * MaxCount
)

// Blurb is the short text a DJ shows between two Tracks.
type Blurb struct {
	DJName string `json:"djName"`
	Text   string `json:"text"`
}

// BlurbWriter writes the Blurb a DJ shows between prev, which just played, and next.
type BlurbWriter interface {
	Write(ctx context.Context, dj DJ, theme string, prev, next model.MediaFile) (string, error)
}

// trackChange is a Blurb to write: the change from prev to next.
type trackChange struct {
	prev, next model.MediaFile
}

// writtenBlurb is a Blurb written for the change into its next Track.
type writtenBlurb struct {
	prevID, nextID string
	text           string
}

// stationBlurbs is the Blurbs of one Listener's Station.
type stationBlurbs struct {
	dj      DJ
	theme   string
	last    *model.MediaFile // the last Track served, which the next refill's first one follows
	pending []trackChange    // in the order they will play
	written []writtenBlurb   // newest first
}

// blurbs holds each Listener's Blurbs, for the Station they last listened to.
type blurbs struct {
	writer     BlurbWriter
	mu         sync.Mutex
	byListener map[string]*stationBlurbs
	// writing holds the Listeners whose Blurbs are being written. It is per Listener, not per
	// Station, so switching Station never has two slow LLM calls running for one Listener.
	writing map[string]bool
	wg      sync.WaitGroup // background writing, for tests
}

// queueAhead queues Blurbs for the Track changes into the Tracks just served on the Listener's
// Station with the DJ and Theme, and starts writing them in the background. A different Station
// from last time drops the old one's Blurbs, written or not. It returns at once.
func (b *blurbs) queueAhead(ctx context.Context, userID string, dj DJ, theme string, served model.MediaFiles) {
	if b.writer == nil || len(served) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.byListener == nil {
		b.byListener = map[string]*stationBlurbs{}
		b.writing = map[string]bool{}
	}
	st := b.byListener[userID]
	if st == nil || st.dj.ID != dj.ID || st.theme != theme {
		st = &stationBlurbs{dj: dj, theme: theme}
		b.byListener[userID] = st
	}
	for _, mf := range served {
		if st.last != nil {
			st.pending = append(st.pending, trackChange{prev: *st.last, next: mf})
		}
		st.last = &mf
	}
	if b.writing[userID] || len(st.pending) == 0 {
		return
	}
	b.writing[userID] = true
	b.wg.Add(1)
	// Writing outlives the refill's request.
	go b.write(context.WithoutCancel(ctx), userID)
}

// write writes the pending Blurbs of the Listener's Station, in order, until none is left. It
// always takes the next one from the Station they listen to now.
func (b *blurbs) write(ctx context.Context, userID string) {
	defer b.wg.Done()
	for {
		b.mu.Lock()
		st := b.byListener[userID]
		if len(st.pending) == 0 {
			delete(b.writing, userID)
			b.mu.Unlock()
			return
		}
		change := st.pending[0]
		st.pending = st.pending[1:]
		b.mu.Unlock()

		wctx, cancel := context.WithTimeout(ctx, blurbTimeout)
		text, err := b.writer.Write(wctx, st.dj, st.theme, change.prev, change.next)
		cancel()
		if err != nil {
			log.Warn(ctx, "Writing a Blurb failed; its Track plays without one", "dj", st.dj.ID, "track", change.next.ID, err)
			continue
		}
		b.mu.Lock()
		// A Blurb for a Station the Listener has left meanwhile goes with it.
		st.written = append([]writtenBlurb{{prevID: change.prev.ID, nextID: change.next.ID, text: text}}, st.written...)
		if len(st.written) > keptBlurbs {
			st.written = st.written[:keptBlurbs]
		}
		b.mu.Unlock()
	}
}

// get returns the Blurb written for the change from afterID into trackID on the Listener's
// Station with the DJ, if it is ready. Asking means the Listener reached trackID, so Blurbs
// still to write up to it are dropped.
func (b *blurbs) get(userID, djID, trackID, afterID string) (Blurb, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := b.byListener[userID]
	if st == nil || st.dj.ID != djID {
		return Blurb{}, false
	}
	// The Listener has reached trackID, so the Blurbs up to it are too late to write.
	for i, c := range st.pending {
		if c.next.ID == trackID {
			st.pending = st.pending[i+1:]
			break
		}
	}
	for _, w := range st.written {
		if w.nextID == trackID && w.prevID == afterID {
			return Blurb{DJName: st.dj.Name, Text: w.text}, true
		}
	}
	return Blurb{}, false
}
