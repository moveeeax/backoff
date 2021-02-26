package backoff

import (
	"context"
	"errors"
	"time"
)

// permanentError wraps an error to signal that retrying must stop immediately.
type permanentError struct {
	err error
}

func (p *permanentError) Error() string { return p.err.Error() }
func (p *permanentError) Unwrap() error { return p.err }

// Permanent wraps err so that Retry stops immediately and returns the
// underlying error without further attempts.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

// IsPermanent reports whether err was wrapped with Permanent.
func IsPermanent(err error) bool {
	var pe *permanentError
	return errors.As(err, &pe)
}

// Retry calls op repeatedly with exponential backoff until one of the
// following conditions is met:
//
//   - op returns nil (success).
//   - op returns a Permanent error — Retry returns the unwrapped error.
//   - b.NextBackOff() returns Stop — Retry returns the last error from op.
//   - ctx is cancelled or its deadline is exceeded — Retry returns ctx.Err().
func Retry(ctx context.Context, op func() error, b *Backoff) error {
	b.Reset()
	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := op()
		if err == nil {
			return nil
		}

		// Unwrap permanent errors and stop immediately.
		var pe *permanentError
		if errors.As(err, &pe) {
			return pe.err
		}
		lastErr = err

		delay := b.NextBackOff()
		if delay == Stop {
			return lastErr
		}

		// Sleep for the delay, but honour context cancellation.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}
