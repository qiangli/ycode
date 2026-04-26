//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLandingPage(t *testing.T) {
	requireConnectivity(t)

	t.Run("ReturnsHTML", func(t *testing.T) {
		status, body := httpGet(t, baseURL(t)+"/")
		if status != 200 {
			t.Fatalf("GET / returned %d, want 200", status)
		}
		if !strings.Contains(body, "<!DOCTYPE html>") {
			t.Error("response is not HTML")
		}
		if !strings.Contains(body, "ycode Pulse") {
			t.Error("page title should contain 'ycode Pulse'")
		}
	})

	t.Run("ContainsTiles", func(t *testing.T) {
		_, body := httpGet(t, baseURL(t)+"/")
		requiredTiles := []string{
			"/prometheus/",
			"/traces/",
			"/logs/",
			"/dashboard/",
			"/memos/",
			"/ollama/",
		}
		for _, tile := range requiredTiles {
			if !strings.Contains(body, tile) {
				t.Errorf("landing page missing tile for %s", tile)
			}
		}
	})

	t.Run("OllamaTilePresent", func(t *testing.T) {
		_, body := httpGet(t, baseURL(t)+"/")
		if !strings.Contains(body, `"/ollama/"`) {
			t.Error("landing page should have an Ollama tile linking to /ollama/")
		}
		if !strings.Contains(body, "Ollama") {
			t.Error("landing page should show 'Ollama' label")
		}
	})

	t.Run("GridAndListViews", func(t *testing.T) {
		_, body := httpGet(t, baseURL(t)+"/")
		if !strings.Contains(body, "grid-home") {
			t.Error("landing page should contain grid-home element")
		}
		if !strings.Contains(body, "list-view") {
			t.Error("landing page should contain list-view element")
		}
		if !strings.Contains(body, "btn-toggle") {
			t.Error("landing page should contain toggle button")
		}
	})

	t.Run("HealthzLink", func(t *testing.T) {
		_, body := httpGet(t, baseURL(t)+"/")
		if !strings.Contains(body, `/healthz`) {
			t.Error("landing page should contain healthz link")
		}
	})

	t.Run("HealthzEndpoint", func(t *testing.T) {
		status, body := httpGet(t, baseURL(t)+"/healthz")
		if status != 200 {
			t.Fatalf("GET /healthz returned %d, want 200", status)
		}
		var result map[string]any
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatalf("healthz response is not valid JSON: %v", err)
		}
		if result["status"] != "ok" {
			t.Errorf("health status = %q, want \"ok\"", result["status"])
		}
	})
}
