package stripeapi

import (
	"context"
	"sync"
	"time"
)

const (
	// DefaultMaxInFlight caps concurrent requests per client.
	DefaultMaxInFlight = 4
	// minRequestSpacing keeps the request rate at 20 per second, under the sandbox's
	// documented 25 per second and the Files API's 20 per second.
	minRequestSpacing = 50 * time.Millisecond
)

// limiter caps in-flight requests and spaces request starts.
type limiter struct {
	slots    chan struct{}
	spacing  time.Duration
	now      func() time.Time
	sleep    func(context.Context, time.Duration) error
	mu       sync.Mutex
	nextSlot time.Time
}

func newLimiter(maxInFlight int, spacing time.Duration, now func() time.Time, sleep func(context.Context, time.Duration) error) *limiter {
	return &limiter{slots: make(chan struct{}, maxInFlight), spacing: spacing, now: now, sleep: sleep}
}

// acquire waits for an in-flight slot and a start time, and returns the function that
// frees the slot.
func (l *limiter) acquire(ctx context.Context) (release func(), err error) {
	select {
	case l.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	release = func() { <-l.slots }

	l.mu.Lock()
	start := l.now()
	if l.nextSlot.After(start) {
		start = l.nextSlot
	}
	l.nextSlot = start.Add(l.spacing)
	wait := start.Sub(l.now())
	l.mu.Unlock()

	if wait > 0 {
		if err := l.sleep(ctx, wait); err != nil {
			release()
			return nil, err
		}
	}
	return release, nil
}

// sleepContext waits for d or until ctx is done.
func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
