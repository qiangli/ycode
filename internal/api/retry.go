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

	"github.com/qiangli/coreutils/pkg/telemetry"

	yotel "github.com/qiangli/ycode/internal/telemetry/otel"
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

	// serverWait is what the SERVER told us to wait, via Retry-After. It beats any
	// backoff we could compute, because it is not a guess — it is the only party that
	// knows when the window actually reopens.
	//
	// We ignored it. On a 429 we rolled our own exponential backoff and hoped, which
	// means we retried too early (wasting an attempt and another 429) or too late
	// (wasting wall-clock). Meanwhile the answer was sitting in a response header.
	var serverWait time.Duration

	for attempt := 0; attempt <= defaultMaxRetries; attempt++ {
		if attempt > 0 {
			delay := backoffWithJitter(attempt)
			if serverWait > 0 {
				delay = serverWait
				serverWait = 0
			}
			yotel.RecordRetry(ctx, attempt, delay, classifyRetryReason(lastErr))
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

		// Wait out any pacing this host has earned. See providerPacer: after a 429 we
		// SLOW DOWN rather than merely retrying the one request that failed.
		if err := providerPacer.wait(ctx, req.URL.Host); err != nil {
			return nil, err
		}

		started := time.Now()
		resp, err := client.Do(req)
		host := req.URL.Host
		if err != nil {
			yotel.RecordHTTPRequest(ctx, req.Method, host, 0, time.Since(started), false)
			if isRetryableNetError(err) {
				lastErr = fmt.Errorf("send request: %w", err)
				continue
			}
			return nil, fmt.Errorf("send request: %w", err)
		}
		yotel.RecordHTTPRequest(ctx, req.Method, host, resp.StatusCode, time.Since(started), resp.StatusCode == http.StatusOK)

		if resp.StatusCode == http.StatusOK {
			return resp, nil
		}

		// Non-200: read error body, close response, classify.
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		body := string(bodyBytes)

		// Classify the error for smart recovery routing.
		reason := ClassifyError(resp.StatusCode, body)
		action := reason.RecommendedAction()

		// Check for token limit errors first - these need special handling.
		if reason == ReasonContextOverflow {
			if tokenErr := ParseTokenLimitError(body); tokenErr != nil {
				return nil, tokenErr
			}
			// Even if we can't parse token numbers, return a classified error.
			return nil, &ClassifiedError{
				Reason:     reason,
				Action:     action,
				StatusCode: resp.StatusCode,
				Body:       body,
			}
		}

		// For abort actions, return immediately without retrying.
		if action == ActionAbort {
			return nil, &ClassifiedError{
				Reason:     reason,
				Action:     action,
				StatusCode: resp.StatusCode,
				Body:       body,
			}
		}

		// For rotate-key and fallback-model, return a classified error so callers
		// can handle the recovery strategy.
		if action == ActionRotateKey || action == ActionFallbackModel {
			return nil, &ClassifiedError{
				Reason:     reason,
				Action:     action,
				StatusCode: resp.StatusCode,
				Body:       body,
			}
		}

		// For retry actions, continue the retry loop.
		if action == ActionRetry {
			// The server may have told us exactly how long to wait. Believe it.
			serverWait = parseRetryAfter(resp.Header.Get("Retry-After"))

			// A rate limit is a fact about the PROVIDER, not about the work. Tell the
			// pacer so the NEXT request is spaced out instead of firing straight into
			// the same wall — reacting to 429s one at a time just means a steady stream
			// of them.
			if resp.StatusCode == http.StatusTooManyRequests {
				providerPacer.penalize(host, serverWait)

				// RECORD IT EVEN THOUGH WE RECOVER. Three 429s in a run all recovered on
				// retry, cost minutes, and left no signal at all — so "rate limits killed
				// it" stayed a plausible theory for hours with nothing to check it
				// against. A bound that binds and recovers is the one nobody investigates
				// until it stops recovering.
				telemetry.BoundHit(ctx, "rate_limit", int64(serverWait/time.Millisecond),
					int64(attempt), "provider 429 from "+host)
			}

			lastErr = &ClassifiedError{
				Reason:     reason,
				Action:     action,
				StatusCode: resp.StatusCode,
				Body:       body,
			}
			continue
		}

		// Fallback: use the legacy classification.
		apiErr := &APIError{
			StatusCode: resp.StatusCode,
			Body:       body,
			Retryable:  isRetryableStatus(resp.StatusCode),
		}

		if !apiErr.Retryable {
			return nil, apiErr
		}

		lastErr = apiErr
	}

	return nil, fmt.Errorf("request failed after %d attempts: %w", defaultMaxRetries+1, lastErr)
}

// classifyRetryReason maps the last error encountered before a retry into
// a coarse reason label so the ycode.api.retry.attempts counter has
// bounded cardinality. Returns "unknown" if no signal is available.
func classifyRetryReason(lastErr error) string {
	if lastErr == nil {
		return "unknown"
	}
	var apiErr *APIError
	if errors.As(lastErr, &apiErr) {
		switch {
		case apiErr.StatusCode == 429:
			return "rate_limited"
		case apiErr.StatusCode >= 500:
			return "5xx"
		case apiErr.StatusCode >= 400:
			return "4xx"
		default:
			return "http_error"
		}
	}
	var classified *ClassifiedError
	if errors.As(lastErr, &classified) {
		return classified.Reason.String()
	}
	return "net_error"
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
