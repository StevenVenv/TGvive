package engine

import (
	"context"
	"crypto/rand"
	"math"
	"math/big"
	"time"
)

func sleepWithContext(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func randomDuration(min, max time.Duration) time.Duration {
	if min < 0 {
		min = 0
	}
	if max < 0 {
		max = 0
	}
	if max < min {
		max = min
	}
	if max == min {
		return min
	}

	span := max - min
	if span <= 0 {
		return min
	}
	// span must fit into int64 for crypto/rand.Int.
	if span > time.Duration(math.MaxInt64-1) {
		return min
	}
	n := big.NewInt(int64(span) + 1)
	r, err := rand.Int(rand.Reader, n)
	if err != nil {
		return min
	}
	return min + time.Duration(r.Int64())
}

func sleepRandom(ctx context.Context, min, max time.Duration) {
	sleepWithContext(ctx, randomDuration(min, max))
}

func normalizeDelayRange(minMs, maxMs int, defMin, defMax time.Duration) (time.Duration, time.Duration) {
	if minMs < 0 {
		minMs = 0
	}
	if maxMs < 0 {
		maxMs = 0
	}

	if minMs == 0 && maxMs == 0 {
		return defMin, defMax
	}

	min := time.Duration(minMs) * time.Millisecond
	max := time.Duration(maxMs) * time.Millisecond
	if maxMs == 0 {
		max = min
	}
	if minMs == 0 {
		min = max
	}

	if max < min {
		min, max = max, min
	}
	return min, max
}
