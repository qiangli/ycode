package gateway

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGateway_EmbeddedPodman_ProxiesToUnixSocket: the gateway exposes a
// localhost unix socket and forwards every request to the configured
// upstream socket. We use a tiny HTTP server bound to an upstream unix
// socket as the stand-in for the embedded libpod handler.
func TestGateway_EmbeddedPodman_ProxiesToUnixSocket(t *testing.T) {
	upstreamDir := shortTempDir(t)
	upstreamPath := filepath.Join(upstreamDir, "upstream.sock")
	upstreamLn, err := net.Listen("unix", upstreamPath)
	if err != nil {
		t.Fatalf("upstream listen: %v", err)
	}
	t.Cleanup(func() { _ = upstreamLn.Close() })

	upstream := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "info-from-"+r.URL.Path)
	})}
	go func() { _ = upstream.Serve(upstreamLn) }()
	t.Cleanup(func() { _ = upstream.Close() })

	gwSockPath := filepath.Join(upstreamDir, "gw.sock")
	g, err := Start(context.Background(), Config{
		Podman: PodmanBackend{
			Mode:     ModeEmbedded,
			Upstream: upstreamPath,
		},
		PodmanSocketPath: gwSockPath,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = g.Close() })

	if got := g.Endpoints().PodmanSocket; got != gwSockPath {
		t.Fatalf("gateway socket = %q, want %q", got, gwSockPath)
	}
	if got := g.Endpoints().PodmanMode; got != ModeEmbedded {
		t.Errorf("podman mode = %q, want %q", got, ModeEmbedded)
	}

	body := dialAndGet(t, gwSockPath, "/v4.0.0/libpod/info")
	if want := "info-from-/v4.0.0/libpod/info"; body != want {
		t.Errorf("body = %q, want %q", body, want)
	}

	// Env() must publish DOCKER_HOST/CONTAINER_HOST pointing at the
	// gateway socket so spawned subprocesses pick it up.
	env := g.Env()
	wantHost := "unix://" + gwSockPath
	if env["DOCKER_HOST"] != wantHost || env["CONTAINER_HOST"] != wantHost {
		t.Errorf("env = %+v, want DOCKER_HOST=%s", env, wantHost)
	}
}

// TestGateway_RemotePodman_InjectsBearer: in remote mode the gateway
// reads the token file and adds Authorization: Bearer to every outbound
// request.
func TestGateway_RemotePodman_InjectsBearer(t *testing.T) {
	gotAuth := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth <- r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "remote-ok")
	}))
	t.Cleanup(upstream.Close)

	dir := shortTempDir(t)
	tokenPath := filepath.Join(dir, "cloudbox.token")
	if err := os.WriteFile(tokenPath, []byte("secret-token\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	gwSock := filepath.Join(dir, "gw.sock")
	g, err := Start(context.Background(), Config{
		Podman: PodmanBackend{
			Mode:      ModeRemote,
			URL:       upstream.URL,
			TokenFile: tokenPath,
		},
		PodmanSocketPath: gwSock,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = g.Close() })

	body := dialAndGet(t, gwSock, "/v4.0.0/libpod/info")
	if body != "remote-ok" {
		t.Errorf("body = %q, want remote-ok", body)
	}
	if got := <-gotAuth; got != "Bearer secret-token" {
		t.Errorf("upstream Authorization = %q, want Bearer secret-token", got)
	}
}

// TestGateway_EmbeddedOllama_ProxiesToHTTP: in embedded mode the ollama
// listener is a thin HTTP reverse proxy to the local runner's URL.
func TestGateway_EmbeddedOllama_ProxiesToHTTP(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "tags-"+r.URL.Path)
	}))
	t.Cleanup(upstream.Close)

	g, err := Start(context.Background(), Config{
		Ollama: OllamaBackend{Mode: ModeEmbedded, Upstream: upstream.URL},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = g.Close() })

	resp, err := http.Get(g.Endpoints().OllamaURL + "/api/tags")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if got := string(body); got != "tags-/api/tags" {
		t.Errorf("body = %q, want tags-/api/tags", got)
	}
	if got := g.Env()["OLLAMA_HOST"]; got != g.Endpoints().OllamaURL {
		t.Errorf("env OLLAMA_HOST = %q, want %q", got, g.Endpoints().OllamaURL)
	}
}

// TestGateway_RestartReclaimsStaleSocket: a previous unclean shutdown
// leaves an inode behind. Start must remove it before binding.
func TestGateway_RestartReclaimsStaleSocket(t *testing.T) {
	dir := shortTempDir(t)
	upstreamPath := filepath.Join(dir, "upstream.sock")
	upstreamLn, err := net.Listen("unix", upstreamPath)
	if err != nil {
		t.Fatalf("upstream listen: %v", err)
	}
	t.Cleanup(func() { _ = upstreamLn.Close() })
	go func() {
		_ = http.Serve(upstreamLn, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "ok")
		}))
	}()

	gwSock := filepath.Join(dir, "gw.sock")
	// Plant a stale inode at the gateway socket path.
	if err := os.WriteFile(gwSock, []byte("stale"), 0o600); err != nil {
		t.Fatalf("plant: %v", err)
	}

	g, err := Start(context.Background(), Config{
		Podman:           PodmanBackend{Mode: ModeEmbedded, Upstream: upstreamPath},
		PodmanSocketPath: gwSock,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = g.Close() })

	if dialAndGet(t, gwSock, "/_ping") == "" {
		t.Error("expected response from gateway after reclaiming stale socket")
	}
}

// dialAndGet issues a GET via the unix socket and returns the body.
func dialAndGet(t *testing.T, sock, path string) string {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", sock)
		},
	}}
	resp, err := client.Get("http://socket" + path)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return strings.TrimSpace(string(b))
}

// shortTempDir returns a tempdir under /tmp (avoiding the macOS
// /var/folders/... path which is too long for AF_UNIX's 104-char limit).
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "ycode-gw-")
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
