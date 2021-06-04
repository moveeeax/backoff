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

// fastBackoffWithElapsed is like fastBackoff but with a step-advancing clock
// so MaxElapsed triggers after a predictable number of NextBackOff calls.
func fastBackoffWithElapsed(maxElapsed time.Duration, stepPerCall time.Duration) *Backoff {
	start := time.Unix(1000, 0)
	t := start
	clkFn := func() time.Time {
		now := t
		t = t.Add(stepPerCall)
		return now
	}
	b := &Backoff{
		InitialInterval:     1 * time.Millisecond,
		MaxInterval:         5 * time.Millisecond,
		MaxElapsed:          maxElapsed,
		Multiplier:          1.5,
		RandomizationFactor: 0,
		now:                 clkFn,
	}
	b.Reset()
	return b
}

// myError is a package-level error type used for errors.As unwrapping tests.
type myError struct{ msg string }

func (e *myError) Error() string { return e.msg }

func TestRetryPermanentError(t *testing.T) {
	underlying := errors.New("bad request")
	calls := 0

	err := Retry(context.Background(), func() error {
		calls++
		return Permanent(underlying)
	}, fastBackoff())

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, underlying) {
		t.Errorf("expected underlying error %v, got %v", underlying, err)
	}
	if calls != 1 {
		t.Errorf("expected exactly 1 call, got %d", calls)
	}
}

func TestRetryPermanentErrorUnwrapsWithErrorsAs(t *testing.T) {
	orig := &myError{msg: "fatal"}

	err := Retry(context.Background(), func() error {
		return Permanent(orig)
	}, fastBackoff())

	var target *myError
	if !errors.As(err, &target) {
		t.Fatalf("errors.As failed: err = %v (%T)", err, err)
	}
	if target.msg != "fatal" {
		t.Errorf("unexpected message: %s", target.msg)
	}
}

func TestRetryStopsOnMaxElapsed(t *testing.T) {
	b := fastBackoffWithElapsed(500*time.Millisecond, 200*time.Millisecond)

	sentinel := errors.New("always fails")
	calls := 0

	err := Retry(context.Background(), func() error {
		calls++
		return sentinel
	}, b)

	if err == nil {
		t.Fatal("expected non-nil error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
	if calls == 0 {
		t.Error("expected at least 1 call")
	}
}

func TestRetryReturnsLastErrorAtStop(t *testing.T) {
	b := fastBackoffWithElapsed(100*time.Millisecond, 60*time.Millisecond)

	attempt := 0
	errs := []error{
		errors.New("err0"),
		errors.New("err1"),
		errors.New("err2"),
	}

	err := Retry(context.Background(), func() error {
		e := errs[attempt%len(errs)]
		attempt++
		return e
	}, b)

	if err == nil {
		t.Fatal("expected error at Stop, got nil")
	}
	found := false
	for _, candidate := range errs {
		if err == candidate {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("unexpected error value: %v", err)
	}
}

func TestIsPermanent(t *testing.T) {
	base := errors.New("base")
	wrapped := Permanent(base)

	if !IsPermanent(wrapped) {
		t.Error("IsPermanent(Permanent(err)) should be true")
	}
	if IsPermanent(base) {
		t.Error("IsPermanent(plain err) should be false")
	}
	if IsPermanent(nil) {
		t.Error("IsPermanent(nil) should be false")
	}
}

func TestPermanentNilPassthrough(t *testing.T) {
	if Permanent(nil) != nil {
		t.Error("Permanent(nil) should return nil")
	}
}

func TestRetryNotifyCalledOnEachError(t *testing.T) {
	calls := 0
	notified := 0
	sentinel := errors.New("fail")

	err := RetryNotify(context.Background(), func() error {
		calls++
		if calls < 3 {
			return sentinel
		}
		return nil
	}, fastBackoff(), func(e error, d time.Duration) {
		notified++
		if !errors.Is(e, sentinel) {
			t.Errorf("notify got unexpected error: %v", e)
		}
	})

	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if notified != 2 {
		t.Errorf("expected 2 notifications, got %d", notified)
	}
}

func TestRetryNotifyNilIsOK(t *testing.T) {
	calls := 0
	err := RetryNotify(context.Background(), func() error {
		calls++
		if calls < 2 {
			return errors.New("once")
		}
		return nil
	}, fastBackoff(), nil)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
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
