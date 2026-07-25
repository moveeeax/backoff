package backoff

import (
	"math"
	"math/rand"
	"testing"
	"time"
)

// newTestBackoff returns a Backoff wired with a deterministic clock and RNG,
// making all tests fully reproducible.
func newTestBackoff(initial, max, elapsed time.Duration, mult, rf float64, seed int64, nowFn func() time.Time) *Backoff {
	b := &Backoff{
		InitialInterval:     initial,
		MaxInterval:         max,
		MaxElapsed:          elapsed,
		Multiplier:          mult,
		RandomizationFactor: rf,
		now:                 nowFn,
		rnd:                 rand.New(rand.NewSource(seed)), //nolint:gosec
	}
	b.Reset()
	return b
}

// fixedClock returns a function that advances by step on each call.
func fixedClock(start time.Time, step time.Duration) func() time.Time {
	t := start
	return func() time.Time {
		now := t
		t = t.Add(step)
		return now
	}
}

func TestDefaultValues(t *testing.T) {
	b := Default()
	if b.InitialInterval != 500*time.Millisecond {
		t.Errorf("InitialInterval = %v, want 500ms", b.InitialInterval)
	}
	if b.MaxInterval != 60*time.Second {
		t.Errorf("MaxInterval = %v, want 60s", b.MaxInterval)
	}
	if b.MaxElapsed != 15*time.Minute {
		t.Errorf("MaxElapsed = %v, want 15m", b.MaxElapsed)
	}
	if b.Multiplier != 1.5 {
		t.Errorf("Multiplier = %v, want 1.5", b.Multiplier)
	}
	if b.RandomizationFactor != 0.5 {
		t.Errorf("RandomizationFactor = %v, want 0.5", b.RandomizationFactor)
	}
}

func TestIntervalGrowthSequence(t *testing.T) {
	// No jitter (rf=0), no elapsed limit. Verify exact growth sequence.
	epoch := time.Unix(0, 0)
	b := newTestBackoff(100*time.Millisecond, 10*time.Second, 0, 2.0, 0, 0, func() time.Time { return epoch })

	// Expected: 100ms, 200ms, 400ms, 800ms, 1600ms, 3200ms, 6400ms, 10s (capped)
	want := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
		1600 * time.Millisecond,
		3200 * time.Millisecond,
		6400 * time.Millisecond,
		10 * time.Second,
		10 * time.Second,
	}

	for i, w := range want {
		got := b.NextBackOff()
		if got != w {
			t.Errorf("call %d: got %v, want %v", i+1, got, w)
		}
	}
}

func TestCapAtMaxInterval(t *testing.T) {
	epoch := time.Unix(0, 0)
	b := newTestBackoff(1*time.Second, 2*time.Second, 0, 10.0, 0, 0, func() time.Time { return epoch })

	for i := 0; i < 5; i++ {
		got := b.NextBackOff()
		if i == 0 && got != time.Second {
			t.Errorf("call 1: got %v, want 1s", got)
		}
		if i > 0 && got != 2*time.Second {
			t.Errorf("call %d: got %v, want 2s (capped)", i+1, got)
		}
	}
}

func TestJitterWithinBounds(t *testing.T) {
	epoch := time.Unix(0, 0)
	seed := int64(42)
	rf := 0.5
	initial := 1 * time.Second

	b := newTestBackoff(initial, 60*time.Second, 0, 1.0, rf, seed, func() time.Time { return epoch })

	min := time.Duration(float64(initial) * (1 - rf))
	max := time.Duration(float64(initial) * (1 + rf))

	for i := 0; i < 100; i++ {
		got := b.NextBackOff()
		if got < min || got > max {
			t.Errorf("call %d: jitter out of bounds: got %v, want [%v, %v]", i+1, got, min, max)
		}
	}
}

func TestJitterDeterministic(t *testing.T) {
	// Two Backoffs with the same seed should produce identical sequences.
	epoch := time.Unix(0, 0)
	seed := int64(99)
	mk := func() *Backoff {
		return newTestBackoff(200*time.Millisecond, 10*time.Second, 0, 1.5, 0.3, seed, func() time.Time { return epoch })
	}
	b1, b2 := mk(), mk()

	for i := 0; i < 20; i++ {
		v1, v2 := b1.NextBackOff(), b2.NextBackOff()
		if v1 != v2 {
			t.Errorf("call %d: b1=%v b2=%v, want equal", i+1, v1, v2)
		}
	}
}

func TestResetRestoresInitialInterval(t *testing.T) {
	epoch := time.Unix(0, 0)
	b := newTestBackoff(100*time.Millisecond, 10*time.Second, 0, 2.0, 0, 0, func() time.Time { return epoch })

	// Advance a few steps.
	for i := 0; i < 5; i++ {
		b.NextBackOff()
	}

	b.Reset()
	got := b.NextBackOff()
	if got != 100*time.Millisecond {
		t.Errorf("after Reset: got %v, want 100ms", got)
	}
}

func TestResetRestartsElapsedWindow(t *testing.T) {
	// MaxElapsed = 1s; each clock call advances 600ms.
	// Reset() calls now() once: T0 = start (elapsed=0).
	// NextBackOff call 1: now() returns start+600ms → elapsed=600ms < 1s → ok
	// NextBackOff call 2: now() returns start+1200ms → elapsed=1200ms > 1s → Stop
	start := time.Unix(1000, 0)
	clk := fixedClock(start, 600*time.Millisecond)

	b := newTestBackoff(100*time.Millisecond, 10*time.Second, 1*time.Second, 1.0, 0, 0, clk)

	if got := b.NextBackOff(); got == Stop {
		t.Fatal("call 1: expected non-Stop, got Stop")
	}
	if got := b.NextBackOff(); got != Stop {
		t.Errorf("call 2: expected Stop, got %v", got)
	}

	// After Reset the elapsed window restarts; call 1 should be non-Stop again.
	b.Reset()
	if got := b.NextBackOff(); got == Stop {
		t.Fatal("after Reset, call 1: expected non-Stop, got Stop")
	}
}

func TestStopAfterMaxElapsed(t *testing.T) {
	start := time.Unix(0, 0)
	// Reset() consumes one clock tick (T0=start).
	// NextBackOff call 1: now=start+2m  → elapsed=2m  < 5m → ok
	// NextBackOff call 2: now=start+4m  → elapsed=4m  < 5m → ok
	// NextBackOff call 3: now=start+6m  → elapsed=6m  > 5m → Stop
	clk := fixedClock(start, 2*time.Minute)

	b := newTestBackoff(100*time.Millisecond, 10*time.Second, 5*time.Minute, 1.5, 0, 0, clk)

	okCount := 0
	for {
		d := b.NextBackOff()
		if d == Stop {
			break
		}
		okCount++
		if okCount > 10 {
			t.Fatal("never reached Stop")
		}
	}
	if okCount != 2 {
		t.Errorf("expected 2 non-Stop results before Stop, got %d", okCount)
	}
}

func TestNoMaxElapsedRunsForever(t *testing.T) {
	epoch := time.Unix(0, 0)
	// MaxElapsed=0 means no time limit; run 1000 calls, none should be Stop.
	b := newTestBackoff(10*time.Millisecond, 1*time.Second, 0, 1.5, 0, 0, func() time.Time { return epoch })

	for i := 0; i < 1000; i++ {
		if d := b.NextBackOff(); d == Stop {
			t.Fatalf("call %d: got Stop with MaxElapsed=0", i+1)
		}
	}
}

func TestNonNegativeJitter(t *testing.T) {
	// Even with RandomizationFactor=1.0, result must be >= 0.
	epoch := time.Unix(0, 0)
	b := newTestBackoff(1*time.Millisecond, 10*time.Second, 0, 1.5, 1.0, 7, func() time.Time { return epoch })
	for i := 0; i < 500; i++ {
		if d := b.NextBackOff(); d < 0 {
			t.Fatalf("call %d: got negative duration %v", i+1, d)
		}
	}
}

func TestIntervalGrowthNoOverflow(t *testing.T) {
	// With no MaxInterval and a large multiplier, interval should not go negative.
	epoch := time.Unix(0, 0)
	b := &Backoff{
		InitialInterval:     1 * time.Hour,
		MaxInterval:         0, // no cap
		MaxElapsed:          0,
		Multiplier:          100.0,
		RandomizationFactor: 0,
		now:                 func() time.Time { return epoch },
	}
	b.Reset()

	for i := 0; i < 10; i++ {
		got := b.NextBackOff()
		if got < 0 {
			t.Fatalf("call %d: negative interval %v after large multiplier", i+1, got)
		}
	}
}

func TestZeroValueBackoffIsUsable(t *testing.T) {
	// The zero value must behave as if Reset had just been called, rather than
	// handing back a 0s delay forever and busy-looping the caller.
	var b Backoff

	for i := 0; i < 3; i++ {
		got := b.NextBackOff()
		if got != defaultInitialInterval {
			t.Fatalf("call %d: got %v, want %v", i+1, got, defaultInitialInterval)
		}
	}
}

func TestStructLiteralWithoutResetIsUsable(t *testing.T) {
	// Constructing a Backoff from a struct literal is the documented API; it
	// must not require an explicit Reset before the first NextBackOff.
	b := &Backoff{
		InitialInterval: 100 * time.Millisecond,
		MaxInterval:     10 * time.Second,
		Multiplier:      2,
	}

	want := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
	}
	for i, w := range want {
		if got := b.NextBackOff(); got != w {
			t.Errorf("call %d: got %v, want %v", i+1, got, w)
		}
	}
}

func TestMultiplierBelowOneDoesNotShrinkToZero(t *testing.T) {
	// A Multiplier < 1 would otherwise halve the interval on every call until
	// it collapsed to 0 and the retry loop spun.
	epoch := time.Unix(0, 0)
	b := newTestBackoff(1*time.Second, 1*time.Minute, 0, 0.5, 0, 0, func() time.Time { return epoch })

	for i := 0; i < 100; i++ {
		if got := b.NextBackOff(); got != time.Second {
			t.Fatalf("call %d: got %v, want a constant 1s", i+1, got)
		}
	}
}

func TestIntervalSaturatesInsteadOfOverflowing(t *testing.T) {
	// With no MaxInterval and a large multiplier the interval reaches the
	// int64 ceiling within a handful of calls. Converting an out-of-range
	// float64 to an integer is implementation-defined in Go (amd64 yields
	// MinInt64), so without an explicit clamp the interval wraps negative and
	// every subsequent delay collapses to 0 — a busy loop.
	epoch := time.Unix(0, 0)

	for _, rf := range []float64{0, 0.5} {
		b := newTestBackoff(1*time.Hour, 0, 0, 100.0, rf, 1, func() time.Time { return epoch })

		for i := 0; i < 200; i++ {
			got := b.NextBackOff()
			if got <= 0 {
				t.Fatalf("rf=%v call %d: non-positive delay %v (currentInterval=%d)",
					rf, i+1, got, int64(b.currentInterval))
			}
		}
		if b.currentInterval != maxDuration {
			t.Errorf("rf=%v: currentInterval = %d, want it saturated at %d",
				rf, int64(b.currentInterval), int64(maxDuration))
		}
	}
}

func TestRandomizationFactorAboveOneIsClamped(t *testing.T) {
	// rf > 1 makes the low end of the jitter window negative; those delays used
	// to be clamped to 0 while the high end grew past interval*2.
	epoch := time.Unix(0, 0)
	interval := 1 * time.Second
	b := newTestBackoff(interval, 1*time.Minute, 0, 1.0, 5.0, 3, func() time.Time { return epoch })

	for i := 0; i < 500; i++ {
		got := b.NextBackOff()
		if got < 0 || got > 2*interval {
			t.Fatalf("call %d: got %v, want within [0, %v]", i+1, got, 2*interval)
		}
	}
}

func TestJitterStaysPositiveBelowFullRandomization(t *testing.T) {
	// For rf strictly below 1 the delay must never reach zero.
	epoch := time.Unix(0, 0)
	b := newTestBackoff(1*time.Second, 1*time.Minute, 0, 1.0, 0.99, 11, func() time.Time { return epoch })

	for i := 0; i < 1000; i++ {
		if got := b.NextBackOff(); got <= 0 {
			t.Fatalf("call %d: got %v, want > 0", i+1, got)
		}
	}
}

func TestDelayNeverExceedsMaxElapsedWindow(t *testing.T) {
	// MaxElapsed is a budget for the whole retry window. Handing back a delay
	// longer than the time left in it makes the caller overshoot its deadline
	// by up to a full MaxInterval.
	start := time.Unix(0, 0)
	cur := start
	b := &Backoff{
		InitialInterval: 25 * time.Second,
		MaxInterval:     60 * time.Second,
		MaxElapsed:      30 * time.Second,
		Multiplier:      2,
		now:             func() time.Time { return cur },
	}
	b.Reset()

	var total time.Duration
	for i := 0; i < 20; i++ {
		d := b.NextBackOff()
		if d == Stop {
			break
		}
		if d < 0 {
			t.Fatalf("call %d: negative delay %v", i+1, d)
		}
		total += d
		cur = cur.Add(d) // simulate the caller actually sleeping
	}

	if total > b.MaxElapsed {
		t.Errorf("total delay %v exceeds MaxElapsed %v", total, b.MaxElapsed)
	}
}

func TestInitialIntervalCappedAtMaxInterval(t *testing.T) {
	epoch := time.Unix(0, 0)
	b := newTestBackoff(30*time.Second, 5*time.Second, 0, 2.0, 0, 0, func() time.Time { return epoch })

	if got := b.NextBackOff(); got != 5*time.Second {
		t.Errorf("first delay = %v, want it capped at MaxInterval 5s", got)
	}
}

func TestDurationFromFloatSaturates(t *testing.T) {
	cases := []struct {
		name string
		in   float64
		want time.Duration
	}{
		{"nan", math.NaN(), 0},
		{"negative", -1e9, 0},
		{"zero", 0, 0},
		{"in range", 1e9, time.Second},
		{"max int64 rounds up out of range", float64(math.MaxInt64), maxDuration},
		{"far above range", float64(math.MaxInt64) * 4, maxDuration},
		{"positive infinity", math.Inf(1), maxDuration},
		{"negative infinity", math.Inf(-1), 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := durationFromFloat(tc.in); got != tc.want {
				t.Errorf("durationFromFloat(%v) = %d, want %d", tc.in, int64(got), int64(tc.want))
			}
		})
	}
}

func TestZeroInitialIntervalDefaultsToSane(t *testing.T) {
	// InitialInterval=0 should default to 500ms, not spin at 0.
	epoch := time.Unix(0, 0)
	b := &Backoff{
		InitialInterval:     0,
		MaxInterval:         10 * time.Second,
		MaxElapsed:          0,
		Multiplier:          1.5,
		RandomizationFactor: 0,
		now:                 func() time.Time { return epoch },
	}
	b.Reset()
	got := b.NextBackOff()
	if got <= 0 {
		t.Errorf("expected positive interval with zero InitialInterval, got %v", got)
	}
}
