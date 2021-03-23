package backoff

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fastBackoff returns a Backoff configured for tests: tiny intervals so tests
// don't actually sleep long, with a fake clock so MaxElapsed checks are
// deterministic.
func fastBackoff() *Backoff {
	epoch := time.Unix(0, 0)
	b := &Backoff{
		InitialInterval:     1 * time.Millisecond,
		MaxInterval:         5 * time.Millisecond,
		MaxElapsed:          0, // no time limit by default
		Multiplier:          1.5,
		RandomizationFactor: 0,
		now:                 func() time.Time { return epoch },
	}
	b.Reset()
	return b
}

func TestRetrySuccessFirstCall(t *testing.T) {
	calls := 0
	err := Retry(context.Background(), func() error {
		calls++
		return nil
	}, fastBackoff())
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestRetrySuccessAfterNFailures(t *testing.T) {
	const target = 4
	calls := 0
	sentinel := errors.New("transient")

	err := Retry(context.Background(), func() error {
		calls++
		if calls < target {
			return sentinel
		}
		return nil
	}, fastBackoff())

	if err != nil {
		t.Fatalf("expected nil after %d calls, got %v", target, err)
	}
	if calls != target {
		t.Errorf("expected %d calls, got %d", target, calls)
	}
}
