# backoff

Exponential backoff with jitter and context-aware retry for Go.

## Why

Retrying failed operations naively — with a fixed sleep — hammers services and
makes thundering-herd problems worse. Exponential backoff grows the wait
between attempts; jitter spreads concurrent callers over time.  This library
gives you both, plus first-class context cancellation so retries don't outlive
their calling goroutine.

## Install

```
go get github.com/moveeeax/backoff
```

Requires Go 1.16 or later.

## Usage

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/moveeeax/backoff"
)

func fetchData(ctx context.Context, url string) error {
	b := backoff.Default()

	return backoff.Retry(ctx, func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			// No point retrying a malformed URL.
			return backoff.Permanent(err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err // transient — will be retried
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusUnauthorized {
			return backoff.Permanent(errors.New("unauthorized: check credentials"))
		}
		if resp.StatusCode >= 500 {
			return fmt.Errorf("server error: %d", resp.StatusCode)
		}
		return nil
	}, b)
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := fetchData(ctx, "https://example.com/api/data"); err != nil {
		fmt.Println("failed:", err)
	}
}
```

## API

### Backoff

```go
type Backoff struct {
    InitialInterval     time.Duration
    MaxInterval         time.Duration
    MaxElapsed          time.Duration
    Multiplier          float64
    RandomizationFactor float64
}
```

`Default()` returns a `*Backoff` with:

| Field                | Default |
|----------------------|---------|
| `InitialInterval`    | 500 ms  |
| `MaxInterval`        | 60 s    |
| `MaxElapsed`         | 15 min  |
| `Multiplier`         | 1.5     |
| `RandomizationFactor`| 0.5     |

**`NextBackOff() time.Duration`** — returns the next wait duration, applying
jitter in `[interval*(1-rf), interval*(1+rf)]`, growing by `Multiplier` each
call, capped at `MaxInterval`. Returns `backoff.Stop` once `MaxElapsed` is
exceeded (set `MaxElapsed = 0` for no limit).

**`Reset()`** — restores initial state and restarts the elapsed timer.

Field semantics worth knowing:

- The **zero value is usable**. A `Backoff` built as a struct literal
  initialises itself on first `NextBackOff`, so calling `Reset` up front is
  optional. `Retry` calls `Reset` for you.
- `InitialInterval` ≤ 0 falls back to 500 ms, and is capped at `MaxInterval`
  when one is set, so the *first* delay respects the cap too.
- `Multiplier` ≤ 1 means a constant interval. Values below 1 would shrink the
  delay towards zero — the opposite of backing off — so they are treated as 1.
- `RandomizationFactor` is clamped to `[0, 1]`. Below 1 the returned delay is
  always positive; at exactly 1 the jitter window starts at zero, so a zero
  delay is possible.
- The returned delay never overflows (it saturates at `math.MaxInt64`) and
  never runs past the end of the `MaxElapsed` window, so `MaxElapsed` is a true
  upper bound on the retry window rather than one a final long sleep can
  overshoot.
- A `Backoff` is stateful and **not safe for concurrent use**. Give each retry
  loop its own.

### Retry

```go
func Retry(ctx context.Context, op func() error, b *Backoff) error
func RetryNotify(ctx context.Context, op func() error, b *Backoff, notify Notify) error

type Notify func(err error, delay time.Duration)
```

`Retry` calls `op` repeatedly until it returns `nil`, the context is cancelled,
or `b.NextBackOff()` returns `Stop`. Returns the last error from `op`, or
`ctx.Err()` if the context ended the loop.

`RetryNotify` behaves identically but calls `notify` after each failed attempt
with the error and the upcoming delay. Useful for logging retry attempts:

```go
backoff.RetryNotify(ctx, op, b, func(err error, d time.Duration) {
    log.Printf("retry in %v: %v", d, err)
})
```

### Permanent errors

```go
func Permanent(err error) error
func IsPermanent(err error) bool
```

Wrap an error with `Permanent` to tell `Retry` to stop immediately.  The
underlying error is unwrapped and returned to the caller; `errors.As` and
`errors.Is` work on the result.

## Testing

```
make vet    # go vet ./...
make test   # go test -race ./...
make build  # go build ./...
```
