//go:build integration

package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestSmoke(t *testing.T) {
	requireConnectivity(t)

	t.Run("PrometheusQueryJSON", func(t *testing.T) {
		// Verify that the embedded Prometheus returns valid JSON for queries.
		url := baseURL(t) + "/prometheus/api/v1/query?query=up"
		status, body := httpGet(t, url)
		if status != 200 {
			t.Fatalf("GET %s returned %d, want 200", url, status)
		}
		var result map[string]any
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatalf("Prometheus response is not valid JSON: %v\nbody: %s", err, body)
		}
		if result["status"] != "success" {
			t.Errorf("Prometheus query status = %q, want \"success\"", result["status"])
		}
		data, ok := result["data"].(map[string]any)
		if !ok {
			t.Fatalf("missing data in Prometheus response")
		}
		// result must be an array (even if empty), not null or missing.
		switch r := data["result"].(type) {
		case []any:
			// valid
		default:
			t.Errorf("Prometheus result is %T, want []any (got: %v)", r, r)
		}
	})

	t.Run("PrometheusQueryRangeJSON", func(t *testing.T) {
		url := baseURL(t) + "/prometheus/api/v1/query_range?query=up&start=0&end=0&step=15"
		status, body := httpGet(t, url)
		if status != 200 {
			t.Fatalf("GET %s returned %d, want 200", url, status)
		}
		var result map[string]any
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatalf("Prometheus range response is not valid JSON: %v\nbody: %s", err, body)
		}
		if result["status"] != "success" {
			t.Errorf("status = %q, want \"success\"", result["status"])
		}
	})

	t.Run("HealthEndpoint", func(t *testing.T) {
		status, body := httpGet(t, baseURL(t)+"/healthz")
		if status != 200 {
			t.Fatalf("health check returned status %d, want 200", status)
		}
		var result map[string]any
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatalf("health response is not valid JSON: %v", err)
		}
		if result["status"] != "ok" {
			t.Errorf("health status = %q, want \"ok\"", result["status"])
		}
	})

	t.Run("VersionViaCLI", func(t *testing.T) {
		bin := binaryPath()
		if bin == "" {
			t.Skip("ycode binary not found")
		}
		if !isLocal(t) {
			t.Skip("CLI tests only run locally")
		}
		out, err := exec.Command(bin, "version").CombinedOutput()
		if err != nil {
			t.Fatalf("ycode version failed: %v\n%s", err, out)
		}
		if len(strings.TrimSpace(string(out))) == 0 {
			t.Error("ycode version returned empty output")
		}
	})

	t.Run("ServerStatus", func(t *testing.T) {
		bin := binaryPath()
		if bin == "" {
			t.Skip("ycode binary not found")
		}
		if !isLocal(t) {
			t.Skip("CLI tests only run locally")
		}
		port := "58080"
		if p := os.Getenv("PORT"); p != "" {
			port = p
		}
		out, err := exec.Command(bin, "serve", "status", "--port", port).CombinedOutput()
		if err != nil {
			t.Fatalf("ycode serve status failed: %v\n%s", err, out)
		}
	})
}
