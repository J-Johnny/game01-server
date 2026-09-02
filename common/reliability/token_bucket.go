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
}

func NewTokenBucket(capacity int, refillPerSecond float64) *TokenBucket {
	if capacity < 1 {
		capacity = 1
	}
	if refillPerSecond <= 0 {
		refillPerSecond = float64(capacity)
	}
	now := time.Now()
	return &TokenBucket{capacity: float64(capacity), rate: refillPerSecond, tokens: float64(capacity), last: now}
}

func (b *TokenBucket) Allow() bool {
	return b.AllowN(1)
}

func (b *TokenBucket) AllowN(n int) bool {
	if b == nil || n <= 0 {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refill(time.Now())
	if b.tokens < float64(n) {
		return false
	}
	b.tokens -= float64(n)
	return true
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
