package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestIsRetryableStatus(t *testing.T) {
	retryable := []int{408, 409, 429, 500, 502, 503, 504}
	for _, code := range retryable {
		if !isRetryableStatus(code) {
			t.Errorf("expected %d to be retryable", code)
		}
	}
	nonRetryable := []int{200, 400, 401, 403, 404, 422}
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
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 401 {
		t.Errorf("expected status 401, got %d", apiErr.StatusCode)
	}
	if apiErr.Retryable {
		t.Error("expected non-retryable")
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
