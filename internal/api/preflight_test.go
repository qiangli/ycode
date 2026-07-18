package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestPreflightAuthCheck_RejectsInvalidKey is the "caught at the door"
// regression test: a stale key (HTTP 401) must be rejected BEFORE a run with
// a clear error naming the provider, the status, and the key fingerprint.
func TestPreflightAuthCheck_RejectsInvalidKey(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid API key"}}`))
	}))
	defer srv.Close()

	cfg := &ProviderConfig{
		Kind:        ProviderOpenAI,
		DisplayName: "deepseek",
		APIKey:      "sk-deepseek-stale-key-1c75",
		BaseURL:     srv.URL,
	}
	err := PreflightAuthCheck(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected preflight to reject a 401 key")
	}
	if !strings.Contains(err.Error(), `provider "deepseek"`) {
		t.Errorf("error should name the provider, got: %v", err)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should include the 401 status, got: %v", err)
	}
	if !strings.Contains(err.Error(), "…1c75") {
		t.Errorf("error should include the last-4 key fingerprint, got: %v", err)
	}
	if strings.Contains(err.Error(), "sk-deepseek-stale-key") {
		t.Errorf("error leaks key material: %v", err)
	}
	if !IsPermanentAuthError(err) {
		t.Errorf("preflight rejection should classify as permanent auth, got: %v", err)
	}
	// The probe must actually present the credential.
	if gotAuth != "Bearer sk-deepseek-stale-key-1c75" {
		t.Errorf("probe sent Authorization %q, want the bearer key", gotAuth)
	}
}

func TestPreflightAuthCheck_RejectsForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	cfg := &ProviderConfig{Kind: ProviderOpenAI, DisplayName: "zai", APIKey: "glm-key-9f8e", BaseURL: srv.URL}
	err := PreflightAuthCheck(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected a clear 403 rejection, got: %v", err)
	}
}

func TestPreflightAuthCheck_AcceptsValidKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	cfg := &ProviderConfig{Kind: ProviderOpenAI, DisplayName: "deepseek", APIKey: "sk-good-key-1c75", BaseURL: srv.URL}
	if err := PreflightAuthCheck(context.Background(), cfg); err != nil {
		t.Fatalf("valid key should pass preflight, got: %v", err)
	}
}

// TestPreflightAuthCheck_IgnoresTransient: the probe's only verdict is
// "the key is invalid". Rate limits, server errors, missing model-list
// endpoints, and unreachable hosts are NOT a verdict on the key — the run
// may still succeed via retry/fallback, so they must not block the door.
func TestPreflightAuthCheck_IgnoresTransient(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
		}))
		cfg := &ProviderConfig{Kind: ProviderOpenAI, DisplayName: "deepseek", APIKey: "sk-key-1c75", BaseURL: srv.URL}
		if err := PreflightAuthCheck(context.Background(), cfg); err != nil {
			t.Errorf("status %d: transient/inconclusive must pass preflight, got: %v", code, err)
		}
		srv.Close()
	}

	// Unreachable host: connection refused is transient, not an auth verdict.
	cfg := &ProviderConfig{Kind: ProviderOpenAI, DisplayName: "local", APIKey: "sk-key-1c75", BaseURL: "http://127.0.0.1:1"}
	if err := PreflightAuthCheck(context.Background(), cfg); err != nil {
		t.Errorf("unreachable host must pass preflight, got: %v", err)
	}
}

// TestPreflightAuthCheck_SkipsNoCredential: keyless providers (local Ollama,
// cloudbox without a key) and OAuth bearer configs have no API key to
// validate — the probe must not fire at all.
func TestPreflightAuthCheck_SkipsNoCredential(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	if err := PreflightAuthCheck(context.Background(), &ProviderConfig{Kind: ProviderOpenAI, BaseURL: srv.URL}); err != nil {
		t.Errorf("no-key config should skip preflight, got: %v", err)
	}
	if err := PreflightAuthCheck(context.Background(), nil); err != nil {
		t.Errorf("nil config should skip preflight, got: %v", err)
	}
	if hits.Load() != 0 {
		t.Errorf("probe must not fire without an API key, got %d requests", hits.Load())
	}
}

// TestPreflightAuthCheck_Anthropic: the Anthropic probe hits /v1/models with
// the x-api-key header (not a bearer token).
func TestPreflightAuthCheck_Anthropic(t *testing.T) {
	var gotPath, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-api-key")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &ProviderConfig{Kind: ProviderAnthropic, APIKey: "sk-ant-good-1c75", BaseURL: srv.URL}
	if err := PreflightAuthCheck(context.Background(), cfg); err != nil {
		t.Fatalf("valid anthropic key should pass preflight, got: %v", err)
	}
	if gotPath != "/v1/models" {
		t.Errorf("anthropic probe path = %q, want /v1/models", gotPath)
	}
	if gotKey != "sk-ant-good-1c75" {
		t.Errorf("anthropic probe x-api-key = %q, want the configured key", gotKey)
	}
}

// TestPreflightAuthCheck_AnthropicRejectsInvalidKey: a stale Anthropic key
// gets the same clear, fingerprinted error as any other provider.
func TestPreflightAuthCheck_AnthropicRejectsInvalidKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
	}))
	defer srv.Close()

	cfg := &ProviderConfig{Kind: ProviderAnthropic, APIKey: "sk-ant-stale-1c75", BaseURL: srv.URL}
	err := PreflightAuthCheck(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "…1c75") {
		t.Fatalf("expected a clear 401 rejection with fingerprint, got: %v", err)
	}
}
