//go:build integration

package integration

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// baseURL returns the target server URL from env or defaults to localhost:58080.
func baseURL(t *testing.T) string {
	t.Helper()
	if u := os.Getenv("BASE_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	host := os.Getenv("HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "58080"
	}
	return fmt.Sprintf("http://%s:%s", host, port)
}

// otelHost returns the OTEL collector host (same as the server host).
func otelHost(t *testing.T) string {
	t.Helper()
	if h := os.Getenv("HOST"); h != "" {
		return h
	}
	return "localhost"
}

// isLocal returns true when testing against localhost.
func isLocal(t *testing.T) bool {
	t.Helper()
	h := otelHost(t)
	return h == "localhost" || h == "127.0.0.1"
}

// binaryPath returns the path to the ycode binary, or empty string if not found.
func binaryPath() string {
	if p := os.Getenv("YCODE_BIN"); p != "" {
		return p
	}
	if _, err := os.Stat("bin/ycode"); err == nil {
		return "bin/ycode"
	}
	// When tests run from the integration package dir, binary is at repo root.
	if _, err := os.Stat("../../bin/ycode"); err == nil {
		return "../../bin/ycode"
	}
	return ""
}

// httpClient returns an HTTP client with a reasonable timeout for integration tests.
func httpClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

// httpGet is a helper that performs a GET and returns status + body.
func httpGet(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := httpClient().Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body from %s: %v", url, err)
	}
	return resp.StatusCode, string(body)
}

// httpPost is a helper that performs a POST with the given content type and body.
func httpPost(t *testing.T, url, contentType, payload string) (int, string) {
	t.Helper()
	resp, err := httpClient().Post(url, contentType, strings.NewReader(payload))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body from %s: %v", url, err)
	}
	return resp.StatusCode, string(body)
}

// readBody reads the full body from an http.Response.
func readBody(resp *http.Response) (string, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// requireConnectivity skips the test if the server is unreachable.
func requireConnectivity(t *testing.T) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(baseURL(t) + "/healthz")
	if err != nil {
		t.Skipf("server not reachable at %s: %v (run 'make deploy' first)", baseURL(t), err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Skipf("server health check failed at %s: status %d", baseURL(t), resp.StatusCode)
	}
}
