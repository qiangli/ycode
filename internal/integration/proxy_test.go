//go:build integration

package integration

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestProxyApps(t *testing.T) {
	requireConnectivity(t)

	t.Run("LandingPageHasAppLinks", func(t *testing.T) {
		status, body := httpGet(t, baseURL(t)+"/")
		if status != 200 {
			t.Fatalf("landing page returned status %d, want 200", status)
		}
		if !strings.Contains(body, "ycode") {
			t.Error("landing page does not contain 'ycode'")
		}

		routes := discoverAppRoutes(t, body)
		if len(routes) == 0 {
			t.Fatal("landing page returned no app links")
		}
		t.Logf("discovered %d app routes: %v", len(routes), routes)
	})

	t.Run("AllProxiedAppsReachable", func(t *testing.T) {
		_, body := httpGet(t, baseURL(t)+"/")
		routes := discoverAppRoutes(t, body)
		if len(routes) == 0 {
			t.Skip("no app routes discovered")
		}

		for _, route := range routes {
			route := route
			name := strings.Trim(route, "/")
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				url := baseURL(t) + route
				resp, err := httpClient().Get(url)
				if err != nil {
					t.Fatalf("GET %s: %v", url, err)
				}
				resp.Body.Close()
				switch resp.StatusCode {
				case http.StatusOK, http.StatusMovedPermanently, http.StatusFound,
					http.StatusMethodNotAllowed: // MCP endpoints (e.g. /pulse/) are POST-only
					// acceptable
				default:
					t.Errorf("GET %s returned %d, want 200/301/302/405", url, resp.StatusCode)
				}
			})
		}
	})
}

// TestProxyAppUIContent verifies that each proxied app serves its real
// third-party web UI, not a placeholder or simple fallback page.
func TestProxyAppUIContent(t *testing.T) {
	requireConnectivity(t)

	tests := []struct {
		name     string
		path     string
		follow   bool   // follow redirects
		contains string // marker that identifies the real UI
		absent   string // marker that should NOT be present (placeholder)
	}{
		{
			name:     "Prometheus",
			path:     "/prometheus/",
			contains: "<title>Prometheus</title>",
			absent:   "ycode Prometheus",
		},
		{
			name:     "Alertmanager",
			path:     "/alerts/",
			contains: "script.js",
			absent:   "ycode Alerts",
		},
		{
			name:     "Perses",
			path:     "/dashboard/",
			contains: "Perses",
		},
		{
			name:     "VictoriaLogs",
			path:     "/logs/",
			follow:   true,
			contains: "VictoriaLogs",
		},
		{
			name:     "Jaeger",
			path:     "/traces/",
			contains: "Jaeger",
			absent:   "This is not the Jaeger UI",
		},
		{
			name:     "Collector",
			path:     "/collector/",
			contains: `"status"`,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			url := baseURL(t) + tc.path

			var statusCode int
			var body string

			if tc.follow {
				// Follow redirects to get final content.
				resp, err := httpClient().Get(url)
				if err != nil {
					t.Fatalf("GET %s: %v", url, err)
				}
				defer resp.Body.Close()
				statusCode = resp.StatusCode
				b, err := readBody(resp)
				if err != nil {
					t.Fatalf("reading body from %s: %v", url, err)
				}
				body = b
			} else {
				statusCode, body = httpGet(t, url)
			}

			if statusCode != 200 {
				t.Fatalf("GET %s returned %d, want 200", url, statusCode)
			}
			if tc.contains != "" && !strings.Contains(body, tc.contains) {
				t.Errorf("GET %s: body missing expected marker %q (body length: %d)", url, tc.contains, len(body))
				if len(body) < 500 {
					t.Logf("body: %s", body)
				}
			}
			if tc.absent != "" && strings.Contains(body, tc.absent) {
				t.Errorf("GET %s: body contains placeholder marker %q — not the real UI", url, tc.absent)
			}
		})
	}
}

// TestProxyAppHealthEndpoints checks that each third-party app's health
// endpoint is accessible through the proxy.
func TestProxyAppHealthEndpoints(t *testing.T) {
	requireConnectivity(t)

	endpoints := []struct {
		name string
		path string
	}{
		{"PrometheusHealth", "/prometheus/-/healthy"},
		{"AlertmanagerHealth", "/alerts/-/healthy"},
		{"PersesHealth", "/dashboard/api/v1/health"},
		{"HealthzAggregated", "/healthz"},
	}

	for _, ep := range endpoints {
		ep := ep
		t.Run(ep.name, func(t *testing.T) {
			t.Parallel()
			url := baseURL(t) + ep.path
			status, body := httpGet(t, url)
			if status != 200 {
				t.Errorf("GET %s returned %d, want 200 (body: %s)", url, status, body)
			}
		})
	}
}

// discoverAppRoutes extracts href="/path/" links from the landing page HTML.
var appRouteRe = regexp.MustCompile(`href="(/[^"]+/)"`)

func discoverAppRoutes(t *testing.T, html string) []string {
	t.Helper()
	matches := appRouteRe.FindAllStringSubmatch(html, -1)
	seen := make(map[string]bool)
	var routes []string
	for _, m := range matches {
		route := m[1]
		if !seen[route] {
			seen[route] = true
			routes = append(routes, route)
		}
	}
	return routes
}
