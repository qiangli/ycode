package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"time"
)

const (
	defaultInitialBackoff = 1 * time.Second
	defaultMaxBackoff     = 128 * time.Second
	defaultMaxRetries     = 8
)

// APIError represents an API error with status code information.
type APIError struct {
	StatusCode int
	Body       string
	Retryable  bool
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error %d: %s", e.StatusCode, e.Body)
}

// isRetryableStatus returns true for HTTP status codes that warrant a retry.
func isRetryableStatus(code int) bool {
	switch code {
	case 408, 409, 429, 500, 502, 503, 504:
		return true
	}
	return false
}

// isRetryableNetError returns true for network errors that warrant a retry
// (connection refused, timeout, DNS failures, etc.).
func isRetryableNetError(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr)
}

// doWithRetry executes an HTTP request with exponential backoff and jitter.
// makeReq is called for each attempt to create a fresh *http.Request.
// On success (HTTP 200), it returns the response with body open.
// On non-retryable errors or retries exhausted, it returns an error.
func doWithRetry(ctx context.Context, client *http.Client, makeReq func() (*http.Request, error)) (*http.Response, error) {
	var lastErr error

	for attempt := 0; attempt <= defaultMaxRetries; attempt++ {
		if attempt > 0 {
			delay := backoffWithJitter(attempt)
			slog.Warn("retrying API request",
				"attempt", attempt+1,
				"delay", delay.Round(time.Millisecond),
				"last_error", lastErr,
			)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		req, err := makeReq()
		if err != nil {
			// Request creation errors are not retryable.
			return nil, err
		}

		resp, err := client.Do(req)
		if err != nil {
			if isRetryableNetError(err) {
				lastErr = fmt.Errorf("send request: %w", err)
				continue
			}
			return nil, fmt.Errorf("send request: %w", err)
		}

		if resp.StatusCode == http.StatusOK {
			return resp, nil
		}

		// Non-200: read error body, close response, classify.
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()

		apiErr := &APIError{
			StatusCode: resp.StatusCode,
			Body:       string(bodyBytes),
			Retryable:  isRetryableStatus(resp.StatusCode),
		}

		if !apiErr.Retryable {
			return nil, apiErr
		}

		lastErr = apiErr
	}

	return nil, fmt.Errorf("request failed after %d attempts: %w", defaultMaxRetries+1, lastErr)
}

// backoffWithJitter returns an exponential backoff duration with random jitter.
// For attempt n (1-based), the delay is in [base, 2*base] where base = initialBackoff * 2^(n-1),
// capped at maxBackoff. The jitter decorrelates concurrent retries.
func backoffWithJitter(attempt int) time.Duration {
	base := defaultInitialBackoff << uint(attempt-1)
	if base > defaultMaxBackoff {
		base = defaultMaxBackoff
	}
	// Jitter in [0, base], so total delay in [base, 2*base].
	jitter := time.Duration(rand.Int64N(int64(base) + 1))
	return base + jitter
}
