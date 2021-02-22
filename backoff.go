package backoff

import (
	"math/rand"
	"time"
)

// Stop is returned by NextBackOff when no further retries should be attempted.
const Stop time.Duration = -1

// Backoff computes exponential backoff intervals with optional jitter.
type Backoff struct {
	// InitialInterval is the first delay duration.
	InitialInterval time.Duration
	// MaxInterval caps the computed delay before jitter is applied.
	MaxInterval time.Duration
	// MaxElapsed is the total time after which NextBackOff returns Stop.
	// Zero means no limit.
	MaxElapsed time.Duration
	// Multiplier is the growth factor applied each call (e.g. 1.5).
	Multiplier float64
	// RandomizationFactor adds ±jitter in the range [0, 1).
	RandomizationFactor float64

	// now returns the current time. Defaults to time.Now.
	now func() time.Time
	// rnd is the random source used for jitter. Defaults to a package-level rand.
	rnd *rand.Rand

	currentInterval time.Duration
	startTime       time.Time
}

// Default returns a Backoff with sensible production defaults:
//   - InitialInterval: 500ms
//   - MaxInterval:     60s
//   - MaxElapsed:      15min
//   - Multiplier:      1.5
//   - RandomizationFactor: 0.5
func Default() *Backoff {
	b := &Backoff{
		InitialInterval:     500 * time.Millisecond,
		MaxInterval:         60 * time.Second,
		MaxElapsed:          15 * time.Minute,
		Multiplier:          1.5,
		RandomizationFactor: 0.5,
	}
	b.Reset()
	return b
}

// Reset restores the backoff to its initial state and records the current time
// as the start of the retry window.
func (b *Backoff) Reset() {
	b.currentInterval = b.InitialInterval
	if b.now != nil {
		b.startTime = b.now()
	} else {
		b.startTime = time.Now()
	}
}

// nextRandFloat64 returns a float in [0, 1) using the injected rnd or the
// package-level default source.
func (b *Backoff) nextRandFloat64() float64 {
	if b.rnd != nil {
		return b.rnd.Float64()
	}
	return rand.Float64() //nolint:gosec
}

// currentTime returns the current wall time.
func (b *Backoff) currentTime() time.Time {
	if b.now != nil {
		return b.now()
	}
	return time.Now()
}

// NextBackOff returns the next interval to wait before retrying.
// It returns Stop when MaxElapsed has been exceeded (if MaxElapsed > 0).
func (b *Backoff) NextBackOff() time.Duration {
	// Check elapsed time.
	if b.MaxElapsed > 0 {
		elapsed := b.currentTime().Sub(b.startTime)
		if elapsed >= b.MaxElapsed {
			return Stop
		}
	}

	// Apply randomization: pick a value in
	//   [interval*(1-rf), interval*(1+rf)]
	rf := b.RandomizationFactor
	interval := b.currentInterval
	var delta time.Duration
	if rf > 0 {
		minInterval := float64(interval) * (1 - rf)
		maxInterval := float64(interval) * (1 + rf)
		delta = time.Duration(minInterval + b.nextRandFloat64()*(maxInterval-minInterval+1))
	} else {
		delta = interval
	}

	// Advance current interval for next call, capping at MaxInterval.
	b.advanceInterval()

	// Clamp delta to non-negative.
	if delta < 0 {
		delta = 0
	}

	return delta
}

// advanceInterval multiplies currentInterval by Multiplier and caps at MaxInterval.
func (b *Backoff) advanceInterval() {
	if b.Multiplier <= 0 {
		return
	}
	// Use float64 arithmetic to avoid integer overflow on large intervals.
	next := float64(b.currentInterval) * b.Multiplier
	if b.MaxInterval > 0 && next > float64(b.MaxInterval) {
		b.currentInterval = b.MaxInterval
	} else {
		b.currentInterval = time.Duration(next)
	}
}
