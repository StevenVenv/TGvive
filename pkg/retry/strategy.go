package retry

import (
	"context"
	"time"
)

type Strategy struct {
	MaxAttempts int

	BaseDelay time.Duration
	MaxDelay  time.Duration

	// JitterFraction adds [0..fraction] of delay as random jitter.
	// Example: 0.2 adds 0~20% jitter.
	JitterFraction float64

	ShouldRetry func(err error) bool
	OnRetry     func(attempt int, wait time.Duration, err error)
}

func (s Strategy) Do(ctx context.Context, fn func() error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if fn == nil {
		return nil
	}

	max := s.MaxAttempts
	if max <= 0 {
		max = 1
	}

	var last error
	for attempt := 1; attempt <= max; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := fn()
		if err == nil {
			return nil
		}
		last = err

		if attempt >= max {
			break
		}
		if s.ShouldRetry != nil && !s.ShouldRetry(err) {
			break
		}

		wait := WithJitter(Backoff(attempt, s.BaseDelay, s.MaxDelay), s.JitterFraction)
		if s.OnRetry != nil {
			s.OnRetry(attempt, wait, err)
		}
		if serr := Sleep(ctx, wait); serr != nil {
			return serr
		}
	}

	return last
}

func Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
