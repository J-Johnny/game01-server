package reliability

import "time"

type RetryObserver interface {
	OnAttempt(attempt int)

	OnRetry(attempt int, err error)

	OnComplete(attempts int, exhausted bool, err error, duration time.Duration)
}

type RateLimitObserver interface {
	OnDecision(allowed bool)
}

type CircuitState uint8

const (
	CircuitStateClosed CircuitState = iota

	CircuitStateOpen

	CircuitStateHalfOpen
)

type CircuitObserver interface {
	OnCall(result string)

	OnStateChange(from CircuitState, to CircuitState)
}
