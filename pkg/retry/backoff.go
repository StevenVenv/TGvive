package retry

import (
	"math"
	"math/rand"
	"sync"
	"time"
)

var (
	rngMu sync.Mutex
	rng   = rand.New(rand.NewSource(time.Now().UnixNano()))
)

// Backoff returns exponential backoff delay for attempt>=1:
// base, base*2, base*4, ... capped by max.
func Backoff(attempt int, base, max time.Duration) time.Duration {
	if attempt <= 0 {
		attempt = 1
	}
	if base <= 0 {
		base = 250 * time.Millisecond
	}
	if max <= 0 {
		max = 30 * time.Second
	}

	shift := attempt - 1
	if shift > 30 {
		shift = 30
	}
	d := time.Duration(int64(base) * int64(1<<shift))
	if d < 0 || d > max {
		d = max
	}
	if d < base {
		d = base
	}
	return d
}

// WithJitter adds random jitter to d.
// fraction is [0..1], e.g. 0.2 means add [0..20%] jitter.
func WithJitter(d time.Duration, fraction float64) time.Duration {
	if d <= 0 {
		return 0
	}
	if fraction <= 0 {
		return d
	}
	if fraction > 1 {
		fraction = 1
	}
	maxJ := int64(math.Round(float64(d) * fraction))
	if maxJ <= 0 {
		return d
	}
	rngMu.Lock()
	n := rng.Int63n(maxJ + 1)
	rngMu.Unlock()
	return d + time.Duration(n)
}
