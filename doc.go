// Package backoff implements exponential backoff with jitter and context-aware retry.
//
// Basic usage:
//
//	b := backoff.Default()
//	err := backoff.Retry(ctx, func() error {
//	    return doSomething()
//	}, b)
//
// To stop retrying immediately on a specific error, wrap it with Permanent:
//
//	err := backoff.Retry(ctx, func() error {
//	    resp, err := http.Get(url)
//	    if err != nil {
//	        return err
//	    }
//	    if resp.StatusCode == http.StatusUnauthorized {
//	        return backoff.Permanent(errors.New("unauthorized"))
//	    }
//	    return nil
//	}, b)
package backoff
