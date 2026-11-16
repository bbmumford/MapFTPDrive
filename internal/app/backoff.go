package app

import (
	"math"
	"math/rand/v2"
	"time"
)

// Backoff is the reconnect schedule for one endpoint: Initial after the
// first failure, multiplied by Factor per further failure up to Max, each
// delay spread by ±Jitter so endpoints on one server do not reconnect in
// lockstep after an outage.
type Backoff struct {
	Initial time.Duration
	Max     time.Duration
	Factor  float64
	Jitter  float64
	// Rand returns a value in [0,1); nil uses math/rand.
	Rand func() float64
}

// DefaultBackoff retries after about 5 s, 10 s, 20 s ... capped at 5 min.
// A network change notification retries sooner regardless.
var DefaultBackoff = Backoff{Initial: 5 * time.Second, Max: 5 * time.Minute, Factor: 2, Jitter: 0.2}

// Delay returns the wait after the given number of consecutive failures
// (1 for the first).
func (b Backoff) Delay(failures int) time.Duration {
	if failures < 1 {
		failures = 1
	}
	base := float64(b.Initial) * math.Pow(b.Factor, float64(failures-1))
	if base > float64(b.Max) || math.IsInf(base, 0) {
		base = float64(b.Max)
	}
	random := b.Rand
	if random == nil {
		random = rand.Float64
	}
	jittered := base * (1 + b.Jitter*(2*random()-1))
	if jittered < float64(time.Millisecond) {
		jittered = float64(time.Millisecond)
	}
	return time.Duration(jittered)
}
