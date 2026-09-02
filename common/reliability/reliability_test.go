package reliability

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryPolicyRetriesTransientFailures(t *testing.T) {
	attempts := 0
	err := RetryPolicy{MaxAttempts: 3, InitialDelay: time.Millisecond, ShouldRetry: func(error) bool { return true }}.Do(context.Background(), func(context.Context) error {
		attempts++
		if attempts < 3 {
			return errors.New("temporary")
		}
		return nil
	})
	if err != nil || attempts != 3 {
		t.Fatalf("retry result: attempts=%d error=%v", attempts, err)
	}
}

func TestTokenBucketLimitsBurst(t *testing.T) {
	bucket := NewTokenBucket(2, 1)
	for i := 0; i <= 2; i++ {
		if (i <= 1 && !bucket.Allow()) || (i > 1 && bucket.Allow()) {
			t.Fatal("token bucket did not allow burst")
		}
	}
}

func TestCircuitBreakerOpensAndRecovers(t *testing.T) {
	breaker := NewCircuitBreaker(2, time.Millisecond)
	failed := func() error { return errors.New("failed") }
	_ = breaker.Execute(failed)
	_ = breaker.Execute(failed)
	if err := breaker.Execute(func() error { return nil }); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected open circuit, got %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := breaker.Execute(func() error { return nil }); err != nil {
		t.Fatalf("expected half-open success, got %v", err)
	}
}
