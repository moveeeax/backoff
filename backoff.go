package backoff

import (
	"math"
	"math/rand"
	"time"
)

// Stop is returned by NextBackOff when no further retries should be attempted.
const Stop time.Duration = -1

// maxDuration is the largest representable time.Duration (~292 years).
const maxDuration time.Duration = math.MaxInt64

// Backoff computes exponential backoff intervals with optional jitter.
//
// The zero value is usable: NextBackOff initialises itself on first use, so a
// Backoff built as a struct literal behaves as if Reset had just been called.
// Unset fields fall back to the documented defaults.
//
// A Backoff is stateful and is not safe for concurrent use by multiple
// goroutines. Give each retry loop its own Backoff.
type Backoff struct {
	// InitialInterval is the first delay duration.
	InitialInterval time.Duration
	// MaxInterval caps the computed delay before jitter is applied.
	MaxInterval time.Duration
	// MaxElapsed is the total time after which NextBackOff returns Stop.
	// Zero means no limit. While it is in force, NextBackOff never returns a
	// delay that would run past the deadline, so the retry window is a true
	// upper bound rather than one that can be overshot by a final long sleep.
	MaxElapsed time.Duration
	// Multiplier is the growth factor applied each call (e.g. 1.5). Values
	// below 1 would shrink the interval towards zero and turn retries into a
	// busy loop, so they are treated as 1 (constant interval).
	Multiplier float64
	// RandomizationFactor adds ±jitter, as a fraction of the current interval,
	// in the range [0, 1]. Values outside that range are clamped into it. At
	// exactly 1 the low end of the jitter window is zero, so a delay of zero is
	// possible; below 1 the returned delay is always positive.
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

// defaultInitialInterval is used by Reset when InitialInterval is not set.
const defaultInitialInterval = 500 * time.Millisecond

// Reset restores the backoff to its initial state and records the current time
// as the start of the retry window. If InitialInterval is zero or negative, it
// defaults to defaultInitialInterval to avoid a busy-loop; if it exceeds
// MaxInterval it is capped there, so the very first delay respects the cap too.
func (b *Backoff) Reset() {
	b.currentInterval = b.InitialInterval
	if b.currentInterval <= 0 {
		b.currentInterval = defaultInitialInterval
	}
	if b.MaxInterval > 0 && b.currentInterval > b.MaxInterval {
		b.currentInterval = b.MaxInterval
	}
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

// ensureStarted lazily initialises internal state so that a Backoff built as a
// struct literal (or its zero value) works without an explicit Reset call.
func (b *Backoff) ensureStarted() {
	if b.startTime.IsZero() || b.currentInterval <= 0 {
		b.Reset()
	}
}

// durationFromFloat converts f to a time.Duration, saturating instead of
// wrapping. Converting an out-of-range float64 to an integer is
// implementation-defined in Go — arm64 saturates to MaxInt64 while amd64
// produces MinInt64 — so the range check has to happen in float space, before
// the conversion. Note that float64(math.MaxInt64) rounds up to 2^63, which is
// itself out of range, hence the >= comparison.
func durationFromFloat(f float64) time.Duration {
	if math.IsNaN(f) || f <= 0 {
		return 0
	}
	if f >= float64(maxDuration) {
		return maxDuration
	}
	return time.Duration(f)
}

// randomizationFactor returns RandomizationFactor clamped to [0, 1]. Values
// above 1 would make the low end of the jitter window negative, which used to
// collapse a large share of delays to zero and busy-loop the retry caller.
func (b *Backoff) randomizationFactor() float64 {
	rf := b.RandomizationFactor
	if math.IsNaN(rf) || rf <= 0 {
		return 0
	}
	if rf > 1 {
		return 1
	}
	return rf
}

// NextBackOff returns the next interval to wait before retrying.
// It returns Stop when MaxElapsed has been exceeded (if MaxElapsed > 0).
// The returned delay is never negative and never exceeds the time remaining in
// the MaxElapsed window.
func (b *Backoff) NextBackOff() time.Duration {
	b.ensureStarted()

	// Check elapsed time.
	remaining := maxDuration
	if b.MaxElapsed > 0 {
		elapsed := b.currentTime().Sub(b.startTime)
		if elapsed >= b.MaxElapsed {
			return Stop
		}
		remaining = b.MaxElapsed - elapsed
	}

	// Apply randomization: pick a value in
	//   [interval*(1-rf), interval*(1+rf)]
	rf := b.randomizationFactor()
	interval := b.currentInterval
	var delta time.Duration
	if rf > 0 {
		minInterval := float64(interval) * (1 - rf)
		maxInterval := float64(interval) * (1 + rf)
		// rand.Float64 returns [0, 1); multiply across the full range.
		delta = durationFromFloat(minInterval + b.nextRandFloat64()*(maxInterval-minInterval))
	} else {
		delta = interval
	}

	// Advance current interval for next call, capping at MaxInterval.
	b.advanceInterval()

	// Never sleep past the end of the MaxElapsed window: the caller would
	// otherwise overshoot its own deadline by up to a full MaxInterval.
	if delta > remaining {
		delta = remaining
	}
	if delta < 0 {
		delta = 0
	}

	return delta
}

// advanceInterval multiplies currentInterval by Multiplier and caps at
// MaxInterval. All arithmetic is done in float64 and clamped before the
// conversion back to time.Duration, so a large multiplier applied many times
// saturates at the cap instead of overflowing int64 and wrapping negative.
func (b *Backoff) advanceInterval() {
	// A multiplier below 1 would shrink the interval towards zero, which is the
	// opposite of backing off; treat it as "no growth".
	if math.IsNaN(b.Multiplier) || b.Multiplier <= 1 {
		return
	}

	limit := maxDuration
	if b.MaxInterval > 0 {
		limit = b.MaxInterval
	}

	next := durationFromFloat(float64(b.currentInterval) * b.Multiplier)
	if next > limit || next < b.currentInterval {
		next = limit
	}
	b.currentInterval = next
}
