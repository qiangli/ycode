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
				case http.StatusOK, http.StatusMovedPermanently, http.StatusFound:
					// acceptable
				default:
					t.Errorf("GET %s returned %d, want 200/301/302", url, resp.StatusCode)
				}
			})
		}
	})
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
