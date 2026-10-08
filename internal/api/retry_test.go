package api

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIsRetryableStatus(t *testing.T) {
	retryable := []int{408, 429, 500, 502, 503, 504}
	for _, code := range retryable {
		if !isRetryableStatus(code) {
			t.Errorf("expected %d to be retryable", code)
		}
	}
	// 409 is NOT retryable (Sprint #379, Story #1528): a conflict refuses
	// the request itself, so the identical retry gets the identical
	// refusal — retrying it burned the whole backoff budget.
	nonRetryable := []int{200, 400, 401, 403, 404, 409, 422}
	for _, code := range nonRetryable {
		if isRetryableStatus(code) {
			t.Errorf("expected %d to NOT be retryable", code)
		}
	}
}

func TestBackoffWithJitter(t *testing.T) {
	// Verify exponential growth up to max.
	for attempt := 1; attempt <= 10; attempt++ {
		d := backoffWithJitter(attempt)
		expectedBase := defaultInitialBackoff << uint(attempt-1)
		if expectedBase > defaultMaxBackoff {
			expectedBase = defaultMaxBackoff
		}
		// Delay should be in [base, 2*base].
		if d < expectedBase || d > 2*expectedBase {
			t.Errorf("attempt %d: delay %v not in [%v, %v]", attempt, d, expectedBase, 2*expectedBase)
		}
	}
}

func TestDoWithRetry_SuccessOnFirst(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	resp, err := doWithRetry(context.Background(), srv.Client(), func() (*http.Request, error) {
		return http.NewRequest("GET", srv.URL, nil)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()
}

func TestDoWithRetry_RetriesOn429(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			w.WriteHeader(429)
			w.Write([]byte(`{"error":"overloaded"}`))
			return
		}
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	// Use a short backoff for testing by calling doWithRetry directly.
	// The default backoff starts at 1s which is too slow for tests.
	// Instead, we test with a context timeout to ensure it retries.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := doWithRetry(ctx, srv.Client(), func() (*http.Request, error) {
		return http.NewRequest("GET", srv.URL, nil)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()

	if got := attempts.Load(); got != 3 {
		t.Errorf("expected 3 attempts, got %d", got)
	}
}

func TestDoWithRetry_NonRetryableError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte("unauthorized"))
	}))
	defer srv.Close()

	_, err := doWithRetry(context.Background(), srv.Client(), func() (*http.Request, error) {
		return http.NewRequest("GET", srv.URL, nil)
	})
	if err == nil {
		t.Fatal("expected error for 401")
	}
	// 401 is a PERMANENT auth failure: the key itself is rejected, so no
	// retry, rotation, or cooldown can help — it must abort immediately.
	classErr, ok := err.(*ClassifiedError)
	if !ok {
		t.Fatalf("expected *ClassifiedError, got %T: %v", err, err)
	}
	if classErr.StatusCode != 401 {
		t.Errorf("expected status 401, got %d", classErr.StatusCode)
	}
	if classErr.Reason != ReasonAuthPermanent {
		t.Errorf("expected ReasonAuthPermanent, got %v", classErr.Reason)
	}
	if classErr.Action != ActionAbort {
		t.Errorf("expected ActionAbort, got %v", classErr.Action)
	}
	if !IsPermanentAuthError(err) {
		t.Error("IsPermanentAuthError should be true for a 401")
	}
}

func TestDoWithRetry_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte("overloaded"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately after first attempt sees 429.
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	_, err := doWithRetry(ctx, srv.Client(), func() (*http.Request, error) {
		return http.NewRequest("GET", srv.URL, nil)
	})
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

// A request cancelled in flight is the caller's cancellation (a turn stopped
// with ESC), not a network fault: one attempt, no retry.
func TestDoWithRetry_CancelledInFlightIsNotRetried(t *testing.T) {
	release := make(chan struct{})
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	defer slog.SetDefault(previous)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	_, err := doWithRetry(ctx, srv.Client(), func() (*http.Request, error) {
		return http.NewRequestWithContext(ctx, "GET", srv.URL, nil)
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if n := attempts.Load(); n != 1 {
		t.Fatalf("attempts = %d, want 1", n)
	}
	if strings.Contains(logged.String(), "retrying") {
		t.Fatalf("a cancelled request was reported as a retry: %s", logged.String())
	}
}

// Sprint: #379; Story: #1528; Story-ID: 145f0ee22c3a
//
// A 409 sticky refusal is terminal: the identical request would be refused
// the same way on every attempt, so the loop must return after ONE attempt
// instead of burning the whole exponential-backoff budget. It must also
// return fast (well under the ~1-2s first backoff) and carry the abort
// classification so the fallback chain stops instead of failing over.
func TestDoWithRetry_409StickyRefusalIsTerminal(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(409)
		w.Write([]byte(`{"error":"refusal is sticky: request conflicts with current state"}`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	started := time.Now()
	_, err := doWithRetry(ctx, srv.Client(), func() (*http.Request, error) {
		return http.NewRequest("GET", srv.URL, nil)
	})
	elapsed := time.Since(started)
	if err == nil {
		t.Fatal("expected an error for a 409 sticky refusal")
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("expected exactly 1 attempt for a terminal 409, got %d", got)
	}
	if elapsed > time.Second {
		t.Errorf("slept a backoff before returning a terminal 409: took %v", elapsed)
	}
	classified, ok := err.(*ClassifiedError)
	if !ok {
		t.Fatalf("expected *ClassifiedError, got %T: %v", err, err)
	}
	if classified.StatusCode != 409 {
		t.Errorf("expected status 409, got %d", classified.StatusCode)
	}
	if classified.Action != ActionAbort {
		t.Errorf("expected ActionAbort, got %v", classified.Action)
	}
	if IsTransientError(err) {
		t.Error("IsTransientError should be false for a terminal 409")
	}
}

// A caller with a deadline shorter than the first backoff must NOT sleep into it.
//
// This is the tool-pre-activation case: classification gets a 3s budget, the first
// backoff is 1-2s, so sleeping it would burn the budget and then die mid-attempt.
// The retry could never have succeeded — it could only turn a fast, honest failure
// into `context deadline exceeded` plus a wasted API call. So: one attempt, return.
func TestDoWithRetry_DoesNotSleepPastDeadline(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(429) // retryable, so only the deadline check can stop the loop
		w.Write([]byte(`{"error":"overloaded"}`))
	}))
	defer srv.Close()

	// Well under the ~1-2s first backoff.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	started := time.Now()
	resp, err := doWithRetry(ctx, srv.Client(), func() (*http.Request, error) {
		return http.NewRequest("GET", srv.URL, nil)
	})
	elapsed := time.Since(started)
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected an error from a server that only ever returns 429")
	}

	if got := attempts.Load(); got != 1 {
		t.Errorf("expected exactly 1 attempt when no budget remains for a retry, got %d", got)
	}

	// THE ASSERTION THAT MATTERS. Previously this path entered the retry branch,
	// logged "retrying API request", then blocked in a select that lost the race to
	// ctx.Done() — so the caller got `context deadline exceeded`, which names the
	// symptom and BURIES the cause. The server said 429; that is what should
	// surface. Reporting the deadline instead sends whoever reads the log looking
	// for a slow network when the real answer was rate limiting.
	if errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("returned the deadline instead of the real cause: %v", err)
	}
	var status int
	var classified *ClassifiedError
	var apiErr *APIError
	switch {
	case errors.As(err, &classified):
		status = classified.StatusCode
	case errors.As(err, &apiErr):
		status = apiErr.StatusCode
	}
	if status != 429 {
		t.Errorf("expected the underlying 429 to surface, got %#v", err)
	}

	// And it must not have slept the backoff to get there.
	if elapsed > time.Second {
		t.Errorf("slept into the deadline instead of returning early: took %v", elapsed)
	}
}

// The converse: a caller with room to spare still retries normally, so the
// deadline check cannot silently disable retries for everyone.
func TestDoWithRetry_StillRetriesWithAmpleDeadline(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(429)
			w.Write([]byte(`{"error":"overloaded"}`))
			return
		}
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := doWithRetry(ctx, srv.Client(), func() (*http.Request, error) {
		return http.NewRequest("GET", srv.URL, nil)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()

	if got := attempts.Load(); got != 2 {
		t.Errorf("expected the retry to still happen with an ample deadline, got %d attempts", got)
	}
}
