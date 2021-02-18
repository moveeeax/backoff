package backoff

import (
	"testing"
	"time"
)

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

func TestIntervalGrowthNoJitter(t *testing.T) {
	// rf=0 so no jitter; verify growth sequence.
	b := &Backoff{
		InitialInterval:     100 * time.Millisecond,
		MaxInterval:         10 * time.Second,
		MaxElapsed:          0,
		Multiplier:          2.0,
		RandomizationFactor: 0,
	}
	b.Reset()

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

func TestResetRestoresInitialInterval(t *testing.T) {
	b := &Backoff{
		InitialInterval:     100 * time.Millisecond,
		MaxInterval:         10 * time.Second,
		Multiplier:          2.0,
		RandomizationFactor: 0,
	}
	b.Reset()
	for i := 0; i < 5; i++ {
		b.NextBackOff()
	}
	b.Reset()
	got := b.NextBackOff()
	if got != 100*time.Millisecond {
		t.Errorf("after Reset: got %v, want 100ms", got)
	}
}
