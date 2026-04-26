//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOllamaUI(t *testing.T) {
	requireConnectivity(t)

	t.Run("SPALoads", func(t *testing.T) {
		status, body := httpGet(t, baseURL(t)+"/ollama/")
		if status != 200 {
			t.Fatalf("GET /ollama/ returned %d, want 200", status)
		}
		if !strings.Contains(body, "<!DOCTYPE html>") {
			t.Error("response is not HTML")
		}
		if !strings.Contains(body, "Ollama") {
			t.Error("page should contain 'Ollama' title")
		}
	})

	t.Run("SPAFallback", func(t *testing.T) {
		// Non-existent paths should fall back to index.html (SPA routing).
		status, body := httpGet(t, baseURL(t)+"/ollama/nonexistent")
		if status != 200 {
			t.Fatalf("GET /ollama/nonexistent returned %d, want 200 (SPA fallback)", status)
		}
		if !strings.Contains(body, "<!DOCTYPE html>") {
			t.Error("SPA fallback should return HTML")
		}
	})

	t.Run("APIProxyTags", func(t *testing.T) {
		status, body := httpGet(t, baseURL(t)+"/ollama/api/tags")
		if status != 200 {
			t.Skipf("Ollama API not reachable (status %d) — skipping API tests", status)
		}
		var result map[string]any
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatalf("API response is not valid JSON: %v\nbody: %s", err, body)
		}
		// Must have a "models" key (even if empty array).
		if _, ok := result["models"]; !ok {
			t.Error("API response should contain 'models' key")
		}
	})

	t.Run("APIProxyVersion", func(t *testing.T) {
		status, body := httpGet(t, baseURL(t)+"/ollama/api/version")
		if status != 200 {
			t.Skipf("Ollama API not reachable (status %d)", status)
		}
		var result map[string]any
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatalf("version response is not valid JSON: %v", err)
		}
		version, ok := result["version"].(string)
		if !ok || version == "" {
			t.Error("version response should contain non-empty 'version' string")
		}
	})

	t.Run("APIProxyPS", func(t *testing.T) {
		status, body := httpGet(t, baseURL(t)+"/ollama/api/ps")
		if status != 200 {
			t.Skipf("Ollama API not reachable (status %d)", status)
		}
		var result map[string]any
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatalf("ps response is not valid JSON: %v", err)
		}
		if _, ok := result["models"]; !ok {
			t.Error("ps response should contain 'models' key")
		}
	})

	t.Run("UIContainsTabs", func(t *testing.T) {
		_, body := httpGet(t, baseURL(t)+"/ollama/")
		expectedTabs := []string{"Models", "Running", "Pull", "Chat & Dashboard"}
		for _, tab := range expectedTabs {
			if !strings.Contains(body, tab) {
				t.Errorf("UI should contain tab %q", tab)
			}
		}
	})

	t.Run("UIContainsIntegrationLinks", func(t *testing.T) {
		_, body := httpGet(t, baseURL(t)+"/ollama/")
		links := []string{"../chat/", "../dashboard/", "../traces/", "../logs/"}
		for _, link := range links {
			if !strings.Contains(body, link) {
				t.Errorf("UI should contain integration link %q", link)
			}
		}
	})

	t.Run("ModelsHaveExpectedFields", func(t *testing.T) {
		status, body := httpGet(t, baseURL(t)+"/ollama/api/tags")
		if status != 200 {
			t.Skipf("Ollama API not reachable (status %d)", status)
		}
		var result struct {
			Models []struct {
				Name       string `json:"name"`
				Size       int64  `json:"size"`
				ModifiedAt string `json:"modified_at"`
				Details    struct {
					Family string `json:"family"`
					Format string `json:"format"`
				} `json:"details"`
			} `json:"models"`
		}
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(result.Models) == 0 {
			t.Skip("no models installed — skipping field validation")
		}
		m := result.Models[0]
		if m.Name == "" {
			t.Error("model name should not be empty")
		}
		if m.Size == 0 {
			t.Error("model size should not be zero")
		}
		if m.Details.Format == "" {
			t.Error("model format should not be empty")
		}
	})
}
