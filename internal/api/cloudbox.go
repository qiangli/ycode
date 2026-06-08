package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// DefaultCloudboxURL is the OpenAI-compatible gateway base URL for the
// shared cloudbox deployment. It is used when DHNT_BASE_URL is not set in
// the environment.
const DefaultCloudboxURL = "https://ai.dhnt.io/v1"

// CloudboxLister returns the cloudbox-pooled models advertised by the
// cloudbox /models endpoint. It mirrors the OllamaLister shape: short
// timeout, nil on failure (logged), so it is safe to invoke from the TUI
// Update goroutine via the /model picker path.
type CloudboxLister func(ctx context.Context) []ModelInfo

// cloudboxModelsResponse mirrors the cloudbox-extended OpenAI shape served
// by cloudbox/hub/internal/handlers/llm_openai.go.
type cloudboxModelsResponse struct {
	Object string          `json:"object"`
	Data   []cloudboxModel `json:"data"`
}

type cloudboxModel struct {
	ID            string   `json:"id"`
	Object        string   `json:"object"`
	OwnedBy       string   `json:"owned_by"`
	XCapabilities []string `json:"x_capabilities,omitempty"`
	XContextLen   int64    `json:"x_context_length,omitempty"`
}

// NewCloudboxLister builds a CloudboxLister that hits GET <baseURL>/models.
// An empty baseURL falls back to DefaultCloudboxURL. An empty bearerToken
// means no Authorization header is sent (the call will likely 401 against
// cloudbox; the lister logs and returns nil rather than propagating).
// A nil hc uses a small dedicated http.Client with a 5s timeout.
func NewCloudboxLister(baseURL, bearerToken string, hc *http.Client) CloudboxLister {
	if baseURL == "" {
		baseURL = DefaultCloudboxURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Second}
	}

	return func(ctx context.Context) []ModelInfo {
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(callCtx, http.MethodGet, baseURL+"/models", nil)
		if err != nil {
			slog.Debug("cloudbox lister: build request failed", "url", baseURL, "error", err)
			return nil
		}
		if bearerToken != "" {
			req.Header.Set("Authorization", "Bearer "+bearerToken)
		}
		req.Header.Set("Accept", "application/json")

		resp, err := hc.Do(req)
		if err != nil {
			slog.Debug("cloudbox lister: request failed", "url", baseURL, "error", err)
			return nil
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			slog.Debug("cloudbox lister: unauthorized (set DHNT_API_KEY)", "url", baseURL, "status", resp.StatusCode)
			return nil
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			slog.Debug("cloudbox lister: non-2xx response", "url", baseURL, "status", resp.StatusCode)
			return nil
		}

		var body cloudboxModelsResponse
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			slog.Debug("cloudbox lister: decode failed", "url", baseURL, "error", err)
			return nil
		}

		var out []ModelInfo
		for _, m := range body.Data {
			if m.ID == "" {
				continue
			}
			out = append(out, ModelInfo{
				ID:       m.ID,
				Provider: providerFromOwnedBy(m.OwnedBy, m.ID),
				Source:   "cloudbox",
			})
		}
		return out
	}
}

// providerFromOwnedBy maps cloudbox's `owned_by` field (e.g. "ollama",
// "openai", a hostname) to ycode's provider taxonomy. Cloudbox's gateway
// is OpenAI-compatible regardless of upstream, so any unmapped owner
// falls back to the model-name heuristic and finally to "openai".
func providerFromOwnedBy(ownedBy, id string) string {
	switch strings.ToLower(ownedBy) {
	case "ollama":
		return "ollama"
	case "openai":
		return "openai"
	case "anthropic":
		return "anthropic"
	}
	if p := DetectProviderFromModel(id); p != "unknown" {
		return p
	}
	return "openai"
}
