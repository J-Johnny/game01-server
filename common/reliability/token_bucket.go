package reliability

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrRateLimited = errors.New("rate limit exceeded")

type TokenBucket struct {
	mu       sync.Mutex
	capacity float64
	rate     float64
	tokens   float64
	last     time.Time
	observer RateLimitObserver
}

func NewTokenBucket(capacity int, refillPerSecond float64, observers ...RateLimitObserver) *TokenBucket {
	if capacity < 1 {
		capacity = 1
	}
	if refillPerSecond <= 0 {
		refillPerSecond = float64(capacity)
	}
	now := time.Now()
	var observer RateLimitObserver
	if len(observers) > 0 {
		observer = observers[0]
	}
	return &TokenBucket{capacity: float64(capacity), rate: refillPerSecond, tokens: float64(capacity), last: now, observer: observer}
}

func (b *TokenBucket) Allow() bool {
	return b.AllowN(1)
}

func (b *TokenBucket) AllowN(n int) bool {
	if b == nil || n <= 0 {
		return true
	}
	b.mu.Lock()
	b.refill(time.Now())
	allowed := b.tokens >= float64(n)
	if allowed {
		b.tokens -= float64(n)
	}
	observer := b.observer
	b.mu.Unlock()
	if observer != nil {
		observer.OnDecision(allowed)
	}
	return allowed
}

func (b *TokenBucket) Wait(ctx context.Context) error {
	for {
		if b.Allow() {
			return nil
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
}

func (b *TokenBucket) refill(now time.Time) {
	if now.After(b.last) {
		b.tokens += now.Sub(b.last).Seconds() * b.rate
		if b.tokens > b.capacity {
			b.tokens = b.capacity
		}
		b.last = now
	}
}
