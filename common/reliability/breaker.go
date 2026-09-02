package reliability

import (
	"errors"
	"sync"
	"time"
)

var ErrCircuitOpen = errors.New("circuit breaker is open")

type CircuitBreaker struct {
	mu             sync.Mutex
	failures       int
	threshold      int
	openUntil      time.Time
	resetTimeout   time.Duration
	halfOpenActive bool
}

func NewCircuitBreaker(failureThreshold int, resetTimeout time.Duration) *CircuitBreaker {
	if failureThreshold < 1 {
		failureThreshold = 1
	}
	if resetTimeout <= 0 {
		resetTimeout = time.Second
	}
	return &CircuitBreaker{threshold: failureThreshold, resetTimeout: resetTimeout}
}

func (b *CircuitBreaker) Execute(operation func() error) error {
	return b.ExecuteClassified(operation, func(error) bool { return true })
}

func (b *CircuitBreaker) ExecuteClassified(operation func() error, countsFailure func(error) bool) error {
	if b == nil {
		return operation()
	}
	if !b.allow() {
		return ErrCircuitOpen
	}
	err := operation()
	if countsFailure == nil || countsFailure(err) {
		b.complete(err)
	} else {
		b.complete(nil)
	}
	return err
}

func (b *CircuitBreaker) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.openUntil.IsZero() {
		return true
	}
	if time.Now().Before(b.openUntil) || b.halfOpenActive {
		return false
	}
	b.halfOpenActive = true
	return true
}

func (b *CircuitBreaker) complete(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.halfOpenActive = false
	if err == nil {
		b.failures = 0
		b.openUntil = time.Time{}
		return
	}
	b.failures++
	if b.failures >= b.threshold {
		b.openUntil = time.Now().Add(b.resetTimeout)
	}
}
