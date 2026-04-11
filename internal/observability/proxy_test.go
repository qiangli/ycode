package observability

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestProxyServer(t *testing.T) {
	// Create a mock backend.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("backend:" + r.URL.Path))
	}))
	defer backend.Close()

	backendURL, _ := url.Parse(backend.URL)

	// Create proxy on a random port.
	proxy := NewProxyServer("127.0.0.1", 0)
	proxy.AddRoute("/test/", backendURL)

	// Use httptest instead of real listener for testing.
	mux := http.NewServeMux()
	mux.Handle("/test/", proxy.reverseProxy("/test/", backendURL))
	mux.HandleFunc("/", proxy.landingPage)
	mux.HandleFunc("/healthz", proxy.healthz)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Test proxy routing.
	t.Run("proxy routes to backend", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/test/foo")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
		// After prefix stripping, backend sees /foo.
		if !strings.Contains(string(body), "backend:/foo") {
			t.Errorf("body = %q, want to contain 'backend:/foo'", string(body))
		}
	})

	// Test landing page.
	t.Run("landing page", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
		if !strings.Contains(string(body), "ycode Observability") {
			t.Errorf("body should contain 'ycode Observability', got %q", string(body))
		}
		if !strings.Contains(string(body), "/test/") {
			t.Errorf("body should list /test/ route, got %q", string(body))
		}
	})

	// Test healthz.
	t.Run("healthz", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/healthz")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
		if !strings.Contains(string(body), `"status":"ok"`) {
			t.Errorf("body = %q, want JSON with status ok", string(body))
		}
	})

	// Test 404 for unknown paths.
	t.Run("404 for unknown", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/unknown")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Errorf("status = %d, want 404", resp.StatusCode)
		}
	})
}

func TestProxyServerStartStop(t *testing.T) {
	proxy := NewProxyServer("127.0.0.1", 0)

	// Allocate a free port for the test.
	ctx := context.Background()
	if err := proxy.Start(ctx); err != nil {
		t.Fatal(err)
	}

	// Give it a moment to start.
	time.Sleep(50 * time.Millisecond)

	if err := proxy.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}
