package musicbrainz_test

import (
	"context"
	"sync"
	"time"

	"github.com/navidrome/navidrome/core/musicbrainz"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeClock only moves when Advance is called.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []fakeWaiter
}

type fakeWaiter struct {
	at time.Time
	ch chan time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.now
		return ch
	}
	c.waiters = append(c.waiters, fakeWaiter{at: c.now.Add(d), ch: ch})
	return ch
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	kept := c.waiters[:0]
	for _, w := range c.waiters {
		if w.at.After(c.now) {
			kept = append(kept, w)
			continue
		}
		w.ch <- c.now
	}
	c.waiters = kept
}

func (c *fakeClock) Waiting() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.waiters)
}

var _ = Describe("Limiter", func() {
	var clock *fakeClock
	var limiter *musicbrainz.Limiter
	var mu sync.Mutex
	var granted []time.Time

	grantedCount := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(granted)
	}
	wait := func(ctx context.Context) {
		defer GinkgoRecover()
		Expect(limiter.Wait(ctx)).To(Succeed())
		mu.Lock()
		granted = append(granted, clock.Now())
		mu.Unlock()
	}

	BeforeEach(func() {
		clock = newFakeClock()
		limiter = musicbrainz.NewLimiter(time.Second, clock)
		granted = nil
	})

	It("lets concurrent callers through one per interval", func() {
		for range 3 {
			go wait(context.Background())
		}
		Eventually(grantedCount).Should(Equal(1))
		Eventually(clock.Waiting).Should(Equal(2))

		clock.Advance(999 * time.Millisecond)
		Consistently(grantedCount, 50*time.Millisecond).Should(Equal(1))
		clock.Advance(time.Millisecond)
		Eventually(grantedCount).Should(Equal(2))
		clock.Advance(time.Second)
		Eventually(grantedCount).Should(Equal(3))

		mu.Lock()
		defer mu.Unlock()
		Expect(granted[1].Sub(granted[0])).To(BeNumerically(">=", time.Second))
		Expect(granted[2].Sub(granted[1])).To(BeNumerically(">=", time.Second))
	})

	It("doesn't make a caller wait after a quiet interval", func() {
		go wait(context.Background())
		Eventually(grantedCount).Should(Equal(1))
		clock.Advance(5 * time.Second)
		go wait(context.Background())
		Eventually(grantedCount).Should(Equal(2))
	})

	It("holds every caller back after a backoff", func() {
		go wait(context.Background())
		Eventually(grantedCount).Should(Equal(1))
		limiter.Backoff(10 * time.Second)
		go wait(context.Background())
		Eventually(clock.Waiting).Should(Equal(1))
		clock.Advance(9 * time.Second)
		Consistently(grantedCount, 50*time.Millisecond).Should(Equal(1))
		clock.Advance(time.Second)
		Eventually(grantedCount).Should(Equal(2))
	})

	It("gives up when the caller's context ends", func() {
		go wait(context.Background())
		Eventually(grantedCount).Should(Equal(1))
		ctx, cancel := context.WithCancel(context.Background())
		errc := make(chan error, 1)
		go func() { errc <- limiter.Wait(ctx) }()
		Eventually(clock.Waiting).Should(Equal(1))
		cancel()
		Eventually(errc).Should(Receive(MatchError(context.Canceled)))
	})
})
