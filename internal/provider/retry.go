package provider

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"
)

// retry tuning. Exposed as vars so tests can shorten the delays.
var (
	retryMaxAttempts = 5
	retryBaseDelay   = 1 * time.Second
	retryMaxDelay    = 30 * time.Second
)

// doWithRetry executes req via client, retrying on transient network errors and
// retryable HTTP status codes (408, 429, 5xx) with exponential backoff and jitter.
//
// The request's body, if any, must come from a source supported by http.NewRequest
// (e.g. *bytes.Reader, *bytes.Buffer, *strings.Reader) so that GetBody is set and
// the body can be replayed on retry. Callers using nil bodies are always safe.
func doWithRetry(client *http.Client, req *http.Request) (*http.Response, error) {
	var lastErr error
	for attempt := 1; attempt <= retryMaxAttempts; attempt++ {
		if attempt > 1 {
			if req.GetBody != nil {
				body, err := req.GetBody()
				if err != nil {
					return nil, fmt.Errorf("resetting request body for retry: %w", err)
				}
				req.Body = body
			}
			if err := sleepBackoff(req.Context(), attempt); err != nil {
				return nil, err
			}
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if req.Context().Err() != nil || attempt == retryMaxAttempts {
				return nil, err
			}
			continue
		}

		if !isRetryableStatus(resp.StatusCode) || attempt == retryMaxAttempts {
			return resp, nil
		}

		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		lastErr = fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil, lastErr
}

func isRetryableStatus(code int) bool {
	if code == http.StatusRequestTimeout || code == http.StatusTooManyRequests {
		return true
	}
	return code >= 500 && code <= 599
}

// sleepBackoff sleeps for an exponentially growing duration with jitter, or returns
// early if the context is cancelled. attempt is 1-indexed (attempt=2 is first retry).
func sleepBackoff(ctx context.Context, attempt int) error {
	delay := retryBaseDelay << (attempt - 2)
	if delay <= 0 || delay > retryMaxDelay {
		delay = retryMaxDelay
	}
	jitter := time.Duration(rand.Int63n(int64(delay) / 2))
	wait := delay + jitter

	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
