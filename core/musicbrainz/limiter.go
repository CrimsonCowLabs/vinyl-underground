package musicbrainz

import (
	"context"
	"sync"
	"time"
)

// Clock is the time the Limiter goes by, so tests can move it by hand.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Limiter spaces requests at least one interval apart, however many callers share it. Each
// caller reserves the next free slot, so they go through in the order they arrived.
type Limiter struct {
	interval time.Duration
	clock    Clock

	mu sync.Mutex
	// next is the earliest time the next request may go.
	next time.Time
}

func NewLimiter(interval time.Duration, clock Clock) *Limiter {
	if clock == nil {
		clock = realClock{}
	}
	return &Limiter{interval: interval, clock: clock}
}

// Wait blocks until the caller may send its request, or ctx ends. A caller that gives up still
// leaves its slot empty, which only ever makes the requests further apart.
func (l *Limiter) Wait(ctx context.Context) error {
	l.mu.Lock()
	now := l.clock.Now()
	slot := l.next
	if slot.Before(now) {
		slot = now
	}
	l.next = slot.Add(l.interval)
	l.mu.Unlock()

	d := slot.Sub(now)
	if d <= 0 {
		return ctx.Err()
	}
	select {
	case <-l.clock.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Backoff holds every caller back for at least d from now, as when the server says it is busy.
func (l *Limiter) Backoff(d time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if until := l.clock.Now().Add(d); until.After(l.next) {
		l.next = until
	}
}
