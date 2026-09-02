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
	Observer     RetryObserver
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
	startedAt := time.Now()
	var err error
	for attempt := 0; attempt < p.MaxAttempts; attempt++ {
		currentAttempt := attempt + 1
		if p.Observer != nil {
			p.Observer.OnAttempt(currentAttempt)
		}
		err = operation(ctx)
		if err == nil {
			if p.Observer != nil {
				p.Observer.OnComplete(currentAttempt, false, nil, time.Since(startedAt))
			}
			return nil
		}
		shouldRetry := attempt+1 < p.MaxAttempts && (p.ShouldRetry == nil || p.ShouldRetry(err))
		if !shouldRetry {
			if p.Observer != nil {
				p.Observer.OnComplete(currentAttempt, attempt > 0 && attempt+1 >= p.MaxAttempts, err, time.Since(startedAt))
			}
			return err
		}
		if p.Observer != nil {
			p.Observer.OnRetry(currentAttempt, err)
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
			if p.Observer != nil {
				p.Observer.OnComplete(currentAttempt, false, ctx.Err(), time.Since(startedAt))
			}
			return ctx.Err()
		}
	}
	if p.Observer != nil {
		p.Observer.OnComplete(p.MaxAttempts, p.MaxAttempts > 1, err, time.Since(startedAt))
	}
	return err
}
