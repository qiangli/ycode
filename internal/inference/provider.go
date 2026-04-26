package inference

import (
	"fmt"

	"github.com/qiangli/ycode/internal/api"
)

// NewLocalProvider creates an API provider backed by the running Ollama
// instance. It wraps the existing OpenAICompatClient, pointed at the
// local runner's /v1/ endpoint with no API key.
func NewLocalProvider(comp *OllamaComponent) (api.Provider, error) {
	if comp == nil || !comp.Healthy() {
		return nil, fmt.Errorf("inference: ollama component not healthy")
	}

	baseURL := comp.BaseURL() + "/v1"
	cfg := &api.ProviderConfig{
		Kind:    api.ProviderLocal,
		BaseURL: baseURL,
	}
	return api.NewProvider(cfg), nil
}

// LocalFallbackConfig returns a ProviderConfig suitable for use in a
// FallbackProvider chain — tries local Ollama first, then falls back.
func LocalFallbackConfig(ollamaBaseURL string) api.ProviderConfig {
	return api.ProviderConfig{
		Kind:    api.ProviderLocal,
		BaseURL: ollamaBaseURL + "/v1",
	}
}
