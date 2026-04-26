//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestModelsEndpoint tests the GET /api/models endpoint on the ycode API server.
func TestModelsEndpoint(t *testing.T) {
	requireConnectivity(t)

	t.Run("ReturnsOK", func(t *testing.T) {
		status, _ := httpGet(t, baseURL(t)+"/ycode/api/models")
		if status != 200 {
			t.Fatalf("GET /api/models returned %d, want 200", status)
		}
	})

	t.Run("ReturnsJSONArray", func(t *testing.T) {
		status, body := httpGet(t, baseURL(t)+"/ycode/api/models")
		if status != 200 {
			t.Skipf("endpoint returned %d", status)
		}

		// Must be valid JSON array.
		var models []map[string]any
		if err := json.Unmarshal([]byte(body), &models); err != nil {
			t.Fatalf("response is not valid JSON array: %v\nbody: %s", err, body[:min(200, len(body))])
		}
	})

	t.Run("ContainsBuiltinModels", func(t *testing.T) {
		status, body := httpGet(t, baseURL(t)+"/ycode/api/models")
		if status != 200 {
			t.Skipf("endpoint returned %d", status)
		}

		var models []map[string]any
		json.Unmarshal([]byte(body), &models)

		builtinCount := 0
		for _, m := range models {
			if m["source"] == "builtin" {
				builtinCount++
			}
		}
		if builtinCount == 0 {
			t.Error("expected at least one builtin model in response")
		}
	})

	t.Run("ModelFieldsPresent", func(t *testing.T) {
		status, body := httpGet(t, baseURL(t)+"/ycode/api/models")
		if status != 200 {
			t.Skipf("endpoint returned %d", status)
		}

		var models []map[string]any
		json.Unmarshal([]byte(body), &models)

		if len(models) == 0 {
			t.Skip("no models returned")
		}

		for i, m := range models {
			if m["id"] == nil || m["id"] == "" {
				t.Errorf("model[%d]: id is missing or empty", i)
			}
			if m["provider"] == nil || m["provider"] == "" {
				t.Errorf("model[%d]: provider is missing or empty", i)
			}
			if m["source"] == nil || m["source"] == "" {
				t.Errorf("model[%d]: source is missing or empty", i)
			}
		}
	})

	t.Run("ValidSources", func(t *testing.T) {
		status, body := httpGet(t, baseURL(t)+"/ycode/api/models")
		if status != 200 {
			t.Skipf("endpoint returned %d", status)
		}

		var models []map[string]any
		json.Unmarshal([]byte(body), &models)

		validSources := map[string]bool{
			"builtin": true,
			"config":  true,
			"env":     true,
			"ollama":  true,
		}
		for _, m := range models {
			source, _ := m["source"].(string)
			if !validSources[source] {
				t.Errorf("unexpected source %q for model %v", source, m["id"])
			}
		}
	})

	t.Run("NoDuplicateIDs", func(t *testing.T) {
		status, body := httpGet(t, baseURL(t)+"/ycode/api/models")
		if status != 200 {
			t.Skipf("endpoint returned %d", status)
		}

		var models []map[string]any
		json.Unmarshal([]byte(body), &models)

		seen := make(map[string]int)
		for _, m := range models {
			id, _ := m["id"].(string)
			seen[id]++
			if seen[id] > 1 {
				t.Errorf("duplicate model ID: %q", id)
			}
		}
	})
}

// TestModelsEndpoint_ChatHub tests the GET /api/models endpoint on the chat hub.
func TestModelsEndpoint_ChatHub(t *testing.T) {
	requireConnectivity(t)

	// The chat hub is mounted at /chat/ on the proxy.
	status, body := httpGet(t, baseURL(t)+"/chat/api/models")
	if status != 200 {
		t.Skipf("chat models endpoint returned %d (chat hub may not be enabled)", status)
	}

	// Must be valid JSON array.
	var models []map[string]any
	if err := json.Unmarshal([]byte(body), &models); err != nil {
		t.Fatalf("response is not valid JSON array: %v\nbody: %s", err, body[:min(200, len(body))])
	}

	// Should contain at least builtin models if the lister is wired up.
	if len(models) == 0 {
		t.Log("chat hub returned empty models list (model lister may not be connected)")
	}
}

// TestModelsEndpoint_WebUI verifies the web UI can access the models endpoint.
func TestModelsEndpoint_WebUI(t *testing.T) {
	requireConnectivity(t)

	// Verify the web UI includes the model selector markup.
	status, body := httpGet(t, baseURL(t)+"/ycode/")
	if status != 200 {
		t.Skipf("web UI returned %d", status)
	}

	if !strings.Contains(body, "model-dropdown") {
		t.Error("web UI HTML does not contain model-dropdown element")
	}
	if !strings.Contains(body, "model-btn") {
		t.Error("web UI HTML does not contain model-btn element")
	}
	if !strings.Contains(body, "model-filter") {
		t.Error("web UI HTML does not contain model-filter input")
	}
}

// TestModelsEndpoint_ChatWebUI verifies the chat web UI includes model selector markup.
func TestModelsEndpoint_ChatWebUI(t *testing.T) {
	requireConnectivity(t)

	status, body := httpGet(t, baseURL(t)+"/chat/")
	if status != 200 {
		t.Skipf("chat web UI returned %d", status)
	}

	if !strings.Contains(body, "chat-model-dropdown") {
		t.Error("chat web UI HTML does not contain chat-model-dropdown element")
	}
	if !strings.Contains(body, "chat-model-btn") {
		t.Error("chat web UI HTML does not contain chat-model-btn element")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
