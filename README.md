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

### Retry

```go
func Retry(ctx context.Context, op func() error, b *Backoff) error
```

Calls `op` repeatedly until it returns `nil`, the context is cancelled, or
`b.NextBackOff()` returns `Stop`. Returns the last error from `op`, or
`ctx.Err()` if the context ended the loop.

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
