package observability

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"server/common/reliability"
)

func TestReliabilityObserversExposeRetryRateLimitAndCircuitMetrics(t *testing.T) {
	metrics := NewMetrics()
	retryObserver := metrics.RetryObserver("gateway", "usercenter", "authenticate", func(error) string {
		return "request_timeout"
	})
	retryPolicy := reliability.RetryPolicy{
		MaxAttempts:  2,
		InitialDelay: time.Millisecond,
		Observer:     retryObserver,
		ShouldRetry:  func(error) bool { return true },
	}
	if err := retryPolicy.Do(context.Background(), func(context.Context) error { return errors.New("temporary") }); err == nil {
		t.Fatal("retry policy unexpectedly succeeded")
	}
	if got := testutil.ToFloat64(metrics.requestAttempts.WithLabelValues("gateway", "usercenter", "authenticate")); got != 2 {
		t.Fatalf("request attempts = %v, want 2", got)
	}
	if got := testutil.ToFloat64(metrics.requestRetries.WithLabelValues("gateway", "usercenter", "authenticate", "request_timeout")); got != 1 {
		t.Fatalf("request retries = %v, want 1", got)
	}
	if got := testutil.ToFloat64(metrics.requestRetryExhausted.WithLabelValues("gateway", "usercenter", "authenticate", "request_timeout")); got != 1 {
		t.Fatalf("retry exhausted = %v, want 1", got)
	}

	rateObserver := metrics.RateLimitObserver("websocket_connection")
	bucket := reliability.NewTokenBucket(1, 1, rateObserver)
	if !bucket.Allow() || bucket.Allow() {
		t.Fatal("unexpected token bucket decisions")
	}
	if got := testutil.ToFloat64(metrics.rateLimitDecisions.WithLabelValues("websocket_connection", "allowed")); got != 1 {
		t.Fatalf("allowed decisions = %v, want 1", got)
	}
	if got := testutil.ToFloat64(metrics.rateLimitDecisions.WithLabelValues("websocket_connection", "rejected")); got != 1 {
		t.Fatalf("rejected decisions = %v, want 1", got)
	}

	circuitObserver := metrics.CircuitObserver("gateway", "usercenter", "authenticate")
	breaker := reliability.NewCircuitBreaker(1, time.Hour, circuitObserver)
	if err := breaker.Execute(func() error { return errors.New("downstream unavailable") }); err == nil {
		t.Fatal("circuit breaker unexpectedly succeeded")
	}
	if err := breaker.Execute(func() error { return nil }); !errors.Is(err, reliability.ErrCircuitOpen) {
		t.Fatalf("second circuit call = %v, want circuit open", err)
	}
	if got := testutil.ToFloat64(metrics.circuitState.WithLabelValues("gateway", "usercenter", "authenticate")); got != float64(reliability.CircuitStateOpen) {
		t.Fatalf("circuit state = %v, want open", got)
	}
	if got := testutil.ToFloat64(metrics.circuitCalls.WithLabelValues("gateway", "usercenter", "authenticate", "circuit_open")); got != 1 {
		t.Fatalf("circuit-open calls = %v, want 1", got)
	}
}
