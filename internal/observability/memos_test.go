package observability

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/observability/memosweb"
)

func findFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func TestMemosEmbeddedFrontend(t *testing.T) {
	// The memosweb.Handler() serves from go:embed assets.
	handler := memosweb.Handler()
	ts := httptest.NewServer(handler)
	defer ts.Close()

	t.Run("index.html has /memos/ prefixed paths", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		html := string(body)

		if resp.StatusCode != 200 {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if !strings.Contains(html, `src="/memos/assets/`) {
			t.Errorf("JS src should have /memos/ prefix:\n%s", html)
		}
		if !strings.Contains(html, `href="/memos/assets/`) {
			t.Errorf("CSS href should have /memos/ prefix:\n%s", html)
		}
		// Must NOT contain unprefixed asset paths.
		if strings.Contains(html, `src="/assets/`) || strings.Contains(html, `href="/assets/`) {
			t.Errorf("found unprefixed asset paths:\n%s", html)
		}
	})

	t.Run("CSS asset served with correct MIME type", func(t *testing.T) {
		// Get index.html to find the CSS filename.
		resp, _ := http.Get(ts.URL + "/")
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		cssPath := extractPath(string(body), ".css")
		if cssPath == "" {
			t.Skip("no CSS path found")
		}
		// Strip the /memos prefix since handler serves from root.
		cssPath = strings.TrimPrefix(cssPath, "/memos")

		resp, err := http.Get(ts.URL + cssPath)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("GET %s: status = %d", cssPath, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/css") {
			t.Errorf("Content-Type = %q, want text/css", ct)
		}
	})

	t.Run("CSP header set", func(t *testing.T) {
		resp, _ := http.Get(ts.URL + "/")
		resp.Body.Close()
		csp := resp.Header.Get("Content-Security-Policy")
		if !strings.Contains(csp, "connect-src 'self'") {
			t.Errorf("CSP should block external access, got: %s", csp)
		}
	})

	t.Run("SPA fallback serves index.html for unknown paths", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/some/spa/route")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)

		if resp.StatusCode != 200 {
			t.Fatalf("status = %d, want 200 (SPA fallback)", resp.StatusCode)
		}
		if !strings.Contains(string(body), "<title>Memos</title>") {
			t.Error("SPA fallback should serve index.html")
		}
	})

	t.Run("cache headers", func(t *testing.T) {
		// index.html should be no-cache.
		resp, _ := http.Get(ts.URL + "/")
		resp.Body.Close()
		if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "no-cache") {
			t.Errorf("index.html Cache-Control = %q, want no-cache", cc)
		}
	})
}

func TestMemosCompositeHandler(t *testing.T) {
	// Mock Memos API backend.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"path":"` + r.URL.Path + `"}`))
	}))
	defer backend.Close()
	backendURL, _ := url.Parse(backend.URL)

	// Build the composite handler the same way MemosComponent.HTTPHandler does.
	mux := http.NewServeMux()
	comp := &MemosComponent{port: 1} // non-zero
	_ = comp

	// Simulate the composite handler logic.
	apiProxy := newTestReverseProxy(backendURL)
	staticHandler := memosweb.Handler()
	compositeHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, prefix := range memosAPIPrefixes {
			if strings.HasPrefix(r.URL.Path, prefix) {
				apiProxy.ServeHTTP(w, r)
				return
			}
		}
		staticHandler.ServeHTTP(w, r)
	})

	// Mount with StripPrefix like the proxy does.
	mux.Handle("/memos/", http.StripPrefix("/memos", compositeHandler))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
		}
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	t.Run("static assets via /memos/", func(t *testing.T) {
		resp, _ := http.Get(ts.URL + "/memos/")
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		if !strings.Contains(string(body), "<title>Memos</title>") {
			t.Error("should serve embedded index.html")
		}
	})

	t.Run("ConnectRPC routed to API backend", func(t *testing.T) {
		resp, _ := http.Get(ts.URL + "/memos/memos.api.v1.AuthService/RefreshToken")
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("status = %d; body: %s", resp.StatusCode, body)
		}
		if !strings.Contains(string(body), "/memos.api.v1.AuthService/RefreshToken") {
			t.Errorf("API backend should receive the path; got: %s", body)
		}
	})

	t.Run("REST API routed to backend", func(t *testing.T) {
		resp, _ := http.Get(ts.URL + "/memos/api/v1/sse")
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		if !strings.Contains(string(body), "/api/v1/sse") {
			t.Errorf("got: %s", body)
		}
	})

	t.Run("file route to backend", func(t *testing.T) {
		resp, _ := http.Get(ts.URL + "/memos/file/attachments/abc/photo.jpg")
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		if !strings.Contains(string(body), "/file/") {
			t.Errorf("got: %s", body)
		}
	})

	t.Run("healthz routed to backend", func(t *testing.T) {
		resp, _ := http.Get(ts.URL + "/memos/healthz")
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		if !strings.Contains(string(body), "/healthz") {
			t.Errorf("got: %s", body)
		}
	})

	t.Run("unprefixed root paths 404", func(t *testing.T) {
		resp, _ := http.Get(ts.URL + "/memos.api.v1.Foo/Bar")
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Errorf("root-level ConnectRPC path should 404, got %d", resp.StatusCode)
		}
	})
}

func TestMemosProxyIntegration(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"path":"` + r.URL.Path + `"}`))
	}))
	defer backend.Close()
	backendURL, _ := url.Parse(backend.URL)

	port := findFreePort(t)
	proxy := NewProxyServer("127.0.0.1", port)

	// Build composite handler like MemosComponent.HTTPHandler does.
	apiProxy := newTestReverseProxy(backendURL)
	staticHandler := memosweb.Handler()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, prefix := range memosAPIPrefixes {
			if strings.HasPrefix(r.URL.Path, prefix) {
				apiProxy.ServeHTTP(w, r)
				return
			}
		}
		staticHandler.ServeHTTP(w, r)
	})
	proxy.AddHandler("/memos/", handler)

	ctx := context.Background()
	if err := proxy.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer proxy.Stop(ctx)
	time.Sleep(50 * time.Millisecond)

	base := fmt.Sprintf("http://127.0.0.1:%d", port)

	t.Run("HTML served from embedded FS", func(t *testing.T) {
		resp, err := http.Get(base + "/memos/")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), `src="/memos/assets/`) {
			t.Errorf("should serve embedded HTML with /memos/ paths")
		}
	})

	t.Run("API proxied to backend", func(t *testing.T) {
		resp, err := http.Get(base + "/memos/memos.api.v1.InstanceService/GetInstanceProfile")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			t.Fatalf("status = %d; body: %s", resp.StatusCode, body)
		}
	})

	t.Run("landing page intact", func(t *testing.T) {
		resp, err := http.Get(base + "/")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "ycode Observability") {
			t.Error("landing page broken")
		}
	})
}

// newTestReverseProxy creates a simple reverse proxy for tests.
func newTestReverseProxy(target *url.URL) http.Handler {
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
		},
	}
}

// extractPath finds the first path containing the suffix in HTML.
func extractPath(html, suffix string) string {
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
