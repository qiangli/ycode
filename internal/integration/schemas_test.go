//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestSchemas(t *testing.T) {
	requireConnectivity(t)
	base := baseURL(t)

	t.Run("HealthEndpoint", func(t *testing.T) {
		status, body := httpGet(t, base+"/ycode/api/health")
		if status != http.StatusOK {
			t.Fatalf("status %d, want 200", status)
		}
		var resp struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			t.Fatalf("unmarshal: %v; body: %s", err, body)
		}
		if resp.Status != "ok" {
			t.Errorf("status = %q, want %q", resp.Status, "ok")
		}
	})

	t.Run("HealthzEndpoint", func(t *testing.T) {
		status, body := httpGet(t, base+"/healthz")
		if status != http.StatusOK {
			t.Fatalf("status %d, want 200", status)
		}
		var resp struct {
			Status string `json:"status"`
			Routes int    `json:"routes"`
		}
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			t.Fatalf("unmarshal: %v; body: %s", err, body)
		}
		if resp.Status != "ok" {
			t.Errorf("status = %q, want %q", resp.Status, "ok")
		}
		if resp.Routes <= 0 {
			t.Errorf("routes = %d, want > 0", resp.Routes)
		}
	})

	t.Run("ConfigEndpoint", func(t *testing.T) {
		status, body := httpGet(t, base+"/ycode/api/config")
		if status != http.StatusOK {
			t.Fatalf("status %d, want 200", status)
		}
		var resp map[string]any
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			t.Fatalf("unmarshal: %v; body: %s", err, body)
		}
		// Config should at minimum have a model field.
		if _, ok := resp["model"]; !ok {
			t.Error("config response missing 'model' field")
		}
	})

	t.Run("StatusEndpoint", func(t *testing.T) {
		status, body := httpGet(t, base+"/ycode/api/status")
		if status != http.StatusOK {
			t.Fatalf("status %d, want 200", status)
		}
		var resp struct {
			Model        string `json:"model"`
			ProviderKind string `json:"provider_kind"`
			Version      string `json:"version"`
		}
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			t.Fatalf("unmarshal: %v; body: %s", err, body)
		}
		if resp.Version == "" {
			t.Error("version is empty")
		}
	})

	t.Run("SessionsEndpoint", func(t *testing.T) {
		status, body := httpGet(t, base+"/ycode/api/sessions")
		if status != http.StatusOK {
			t.Fatalf("status %d, want 200", status)
		}
		// Sessions should be a JSON array (possibly empty).
		var sessions []struct {
			ID        string `json:"id"`
			CreatedAt string `json:"created_at"`
		}
		if err := json.Unmarshal([]byte(body), &sessions); err != nil {
			t.Fatalf("unmarshal sessions: %v; body: %s", err, body)
		}
	})

	t.Run("ProxiedComponentHealth", func(t *testing.T) {
		// Verify key proxied components serve valid responses.
		components := []struct {
			name string
			path string
		}{
			{"prometheus", "/prometheus/-/healthy"},
			{"jaeger", "/traces/"},
			{"victoria-logs", "/logs/health"},
			{"perses", "/dashboard/api/v1/health"},
		}

		for _, c := range components {
			t.Run(c.name, func(t *testing.T) {
				t.Parallel()
				url := base + c.path
				resp, err := httpClient().Get(url)
				if err != nil {
					t.Fatalf("GET %s: %v", url, err)
				}
				defer resp.Body.Close()
				if resp.StatusCode < 200 || resp.StatusCode >= 400 {
					body, _ := readBody(resp)
					t.Errorf("%s returned %d; body: %s", c.name, resp.StatusCode, body)
				}
			})
		}
	})
}
