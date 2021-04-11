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

func TestRetryContextCancelledBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	calls := 0
	err := Retry(ctx, func() error {
		calls++
		return errors.New("should not be reached")
	}, fastBackoff())

	if err == nil {
		t.Fatal("expected context error, got nil")
	}
	if calls != 0 {
		t.Errorf("expected 0 op calls with pre-cancelled ctx, got %d", calls)
	}
}

func TestRetryContextCancelledDuringSleep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	calls := 0
	transient := errors.New("transient")

	b := &Backoff{
		InitialInterval:     50 * time.Millisecond,
		MaxInterval:         200 * time.Millisecond,
		MaxElapsed:          0,
		Multiplier:          1.5,
		RandomizationFactor: 0,
	}
	b.Reset()

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := Retry(ctx, func() error {
		calls++
		return transient
	}, b)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if calls < 1 {
		t.Errorf("expected at least 1 op call, got %d", calls)
	}
}

func TestRetryContextDeadlineExceeded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	b := &Backoff{
		InitialInterval:     50 * time.Millisecond,
		MaxInterval:         200 * time.Millisecond,
		MaxElapsed:          0,
		Multiplier:          1.5,
		RandomizationFactor: 0,
	}
	b.Reset()

	err := Retry(ctx, func() error {
		return errors.New("transient")
	}, b)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}
