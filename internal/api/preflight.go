package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// preflightTimeout bounds the key-health probe so a hung endpoint cannot
// stall startup or a model switch.
const preflightTimeout = 10 * time.Second

// PreflightAuthCheck cheaply validates that cfg's API key can authenticate
// BEFORE a run starts — a stale key is caught at the door, not mid-meeting.
// It issues a single GET to the provider's model-list endpoint (no inference,
// no tokens spent).
//
// The probe's only verdict is "the key is invalid": a 401/403 fails with an
// *AuthError naming the provider and fingerprinting the key. Everything else
// passes — 404 (endpoint has no model listing), 429/5xx, and unreachable
// hosts are transient or inconclusive, NOT a verdict on the key, and the run
// may still succeed via retry/fallback.
//
// Configs without an API key (local providers, OAuth bearer — which has its
// own refresh flow) are skipped: no request is made.
func PreflightAuthCheck(ctx context.Context, cfg *ProviderConfig) error {
	if cfg == nil || cfg.APIKey == "" {
		return nil
	}
	req, err := preflightRequest(ctx, cfg)
	if err != nil {
		// Cannot even build a probe (e.g. no base URL) — inconclusive.
		return nil
	}
	client := &http.Client{Timeout: preflightTimeout}
	resp, err := client.Do(req)
	if err != nil {
		// Network unreachable: transient, not a verdict on the key.
		return nil
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return NewAuthError(cfg.DisplayKind(), resp.StatusCode, cfg.APIKey)
	}
	return nil
}

// preflightRequest builds the cheap auth-probe request for a provider.
func preflightRequest(ctx context.Context, cfg *ProviderConfig) (*http.Request, error) {
	if cfg.Kind == ProviderAnthropic {
		base := cfg.BaseURL
		if base == "" {
			base = "https://api.anthropic.com"
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			strings.TrimSuffix(base, "/")+"/v1/models", nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("x-api-key", cfg.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		return req, nil
	}
	// OpenAI-compatible providers (incl. Gemini's OpenAI-compatible endpoint).
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("no base URL to probe")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimSuffix(cfg.BaseURL, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	return req, nil
}
