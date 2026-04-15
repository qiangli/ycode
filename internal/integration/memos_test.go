//go:build integration

package integration

import (
	"io"
	"strings"
	"testing"
)

func TestMemos(t *testing.T) {
	requireConnectivity(t)

	t.Run("HTMLRewritten", func(t *testing.T) {
		u := baseURL(t) + "/memos/"
		resp, err := httpClient().Get(u)
		if err != nil {
			t.Fatalf("GET %s: %v", u, err)
		}
		defer resp.Body.Close()
		bodyBytes, _ := io.ReadAll(resp.Body)
		body := string(bodyBytes)

		if resp.StatusCode != 200 {
			t.Fatalf("GET %s returned %d, want 200\nbody: %s", u, resp.StatusCode, body)
		}

		// CSP header must block external network access (air-gapped).
		csp := resp.Header.Get("Content-Security-Policy")
		if csp == "" {
			t.Error("missing Content-Security-Policy header")
		} else if !strings.Contains(csp, "connect-src 'self'") {
			t.Errorf("CSP should restrict connect-src to 'self', got: %s", csp)
		}

		// Asset paths must have /memos/ prefix (built with base: '/memos/').
		if !strings.Contains(body, `/memos/assets/`) {
			t.Errorf("HTML should contain /memos/assets/ paths\ngot:\n%s", body)
		}

		// Original unprefixed paths must not appear.
		badPatterns := []string{
			`href="/assets/`,
			`src="/assets/`,
			`href="/site.webmanifest"`,
			`href="/logo.webp"`,
		}
		for _, bad := range badPatterns {
			if strings.Contains(body, bad) {
				t.Errorf("HTML must NOT contain unprefixed %q", bad)
			}
		}
	})

	t.Run("CSSAsset", func(t *testing.T) {
		_, html := httpGet(t, baseURL(t)+"/memos/")
		cssPath := extractAssetPath(html, ".css")
		if cssPath == "" {
			t.Skip("no CSS asset found in HTML")
		}
		status, _ := httpGet(t, baseURL(t)+cssPath)
		if status != 200 {
			t.Fatalf("GET %s returned %d, want 200", cssPath, status)
		}
	})

	t.Run("JSAsset", func(t *testing.T) {
		_, html := httpGet(t, baseURL(t)+"/memos/")
		jsPath := extractAssetPath(html, ".js")
		if jsPath == "" {
			t.Skip("no JS asset found in HTML")
		}
		status, _ := httpGet(t, baseURL(t)+jsPath)
		if status != 200 {
			t.Fatalf("GET %s returned %d, want 200", jsPath, status)
		}
	})

	t.Run("Manifest", func(t *testing.T) {
		_, html := httpGet(t, baseURL(t)+"/memos/")
		if !strings.Contains(html, `/memos/site.webmanifest`) {
			t.Skip("no manifest path found in HTML")
		}
		status, _ := httpGet(t, baseURL(t)+"/memos/site.webmanifest")
		if status != 200 {
			t.Fatalf("manifest returned %d, want 200", status)
		}
	})

	t.Run("MemosHealthz", func(t *testing.T) {
		status, body := httpGet(t, baseURL(t)+"/memos/healthz")
		if status != 200 {
			t.Fatalf("GET /memos/healthz returned %d, want 200\nbody: %s", status, body)
		}
	})

	t.Run("ConnectRPC", func(t *testing.T) {
		// ConnectRPC paths go through /memos/ prefix (the frontend is built
		// with baseUrl including /memos). After StripPrefix, the composite
		// handler routes to the API backend.
		url := baseURL(t) + "/memos/memos.api.v1.InstanceService/GetInstanceProfile"
		status, body := httpGet(t, url)
		if status == 404 {
			t.Fatalf("GET %s returned 404 — composite handler not routing ConnectRPC\nbody: %s", url, body)
		}
	})

	t.Run("RESTAPI", func(t *testing.T) {
		url := baseURL(t) + "/memos/api/v1/status"
		status, _ := httpGet(t, url)
		if status == 404 {
			t.Logf("GET %s returned 404 — may be expected if endpoint doesn't exist", url)
		}
	})
}

// extractAssetPath finds the first asset path with the given suffix in HTML.
func extractAssetPath(html, suffix string) string {
	search := `/memos/assets/`
	for {
		idx := strings.Index(html, search)
		if idx < 0 {
			return ""
		}
		rest := html[idx:]
		end := strings.IndexAny(rest, `"'> `)
		if end < 0 {
			return ""
		}
		path := rest[:end]
		if strings.HasSuffix(path, suffix) {
			return path
		}
		html = html[idx+len(search):]
	}
}
