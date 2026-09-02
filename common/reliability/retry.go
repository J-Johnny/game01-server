package reliability

import (
	"context"
	"time"
)

type RetryPolicy struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	ShouldRetry  func(error) bool
}

func (p RetryPolicy) Do(ctx context.Context, operation func(context.Context) error) error {
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = 1
	}
	if p.InitialDelay <= 0 {
		p.InitialDelay = 10 * time.Millisecond
	}
	if p.MaxDelay <= 0 {
		p.MaxDelay = time.Second
	}
	var err error
	for attempt := 0; attempt < p.MaxAttempts; attempt++ {
		err = operation(ctx)
		if err == nil {
			return nil
		}
		if attempt+1 >= p.MaxAttempts || p.ShouldRetry != nil && !p.ShouldRetry(err) {
			return err
		}
		delay := p.InitialDelay << attempt
		if delay > p.MaxDelay {
			delay = p.MaxDelay
		}
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
	return err
}
