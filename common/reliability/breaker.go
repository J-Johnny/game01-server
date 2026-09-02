package reliability

import (
	"errors"
	"sync"
	"time"
)

var ErrCircuitOpen = errors.New("circuit breaker is open")

type CircuitBreaker struct {
	mu           sync.Mutex
	failures     int
	threshold    int
	openUntil    time.Time
	resetTimeout time.Duration
	state        CircuitState
	observer     CircuitObserver
}

func NewCircuitBreaker(failureThreshold int, resetTimeout time.Duration, observers ...CircuitObserver) *CircuitBreaker {
	if failureThreshold < 1 {
		failureThreshold = 1
	}
	if resetTimeout <= 0 {
		resetTimeout = time.Second
	}
	var observer CircuitObserver
	if len(observers) > 0 {
		observer = observers[0]
	}
	return &CircuitBreaker{threshold: failureThreshold, resetTimeout: resetTimeout, state: CircuitStateClosed, observer: observer}
}

func (b *CircuitBreaker) Execute(operation func() error) error {
	return b.ExecuteClassified(operation, func(error) bool { return true })
}

func (b *CircuitBreaker) ExecuteClassified(operation func() error, countsFailure func(error) bool) error {
	if b == nil {
		return operation()
	}
	if !b.allow() {
		if b.observer != nil {
			b.observer.OnCall("circuit_open")
		}
		return ErrCircuitOpen
	}
	err := operation()
	counts := countsFailure == nil || countsFailure(err)
	if counts {
		b.complete(err)
	} else {
		b.complete(nil)
	}
	if b.observer != nil {
		result := "success"
		if err != nil {
			result = "ignored_failure"
			if counts {
				result = "failure"
			}
		}
		b.observer.OnCall(result)
	}
	return err
}

func (b *CircuitBreaker) allow() bool {
	var from CircuitState
	var to CircuitState
	b.mu.Lock()
	now := time.Now()
	switch b.state {
	case CircuitStateClosed:
		b.mu.Unlock()
		return true
	case CircuitStateOpen:
		if now.Before(b.openUntil) {
			b.mu.Unlock()
			return false
		}
		from = b.state
		to = CircuitStateHalfOpen
		b.state = to
		b.mu.Unlock()
		b.notifyStateChange(from, to)
		return true
	case CircuitStateHalfOpen:
		b.mu.Unlock()
		return false
	}
	b.mu.Unlock()
	return false
}

func (b *CircuitBreaker) complete(err error) {
	var from CircuitState
	var to CircuitState
	b.mu.Lock()
	if err == nil {
		b.failures = 0
		b.openUntil = time.Time{}
		if b.state != CircuitStateClosed {
			from = b.state
			to = CircuitStateClosed
			b.state = to
		}
	} else {
		b.failures++
		if b.failures >= b.threshold {
			b.openUntil = time.Now().Add(b.resetTimeout)
			if b.state != CircuitStateOpen {
				from = b.state
				to = CircuitStateOpen
				b.state = to
			}
		}
	}
	b.mu.Unlock()
	if from != to {
		b.notifyStateChange(from, to)
	}
}

func (b *CircuitBreaker) State() CircuitState {
	if b == nil {
		return CircuitStateClosed
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

func (b *CircuitBreaker) SetObserver(observer CircuitObserver) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.observer = observer
	state := b.state
	b.mu.Unlock()
	if observer != nil {
		observer.OnStateChange(state, state)
	}
}

func (b *CircuitBreaker) notifyStateChange(from CircuitState, to CircuitState) {
	if b.observer != nil {
		b.observer.OnStateChange(from, to)
	}
}
