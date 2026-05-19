package gateway

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// TestE2E_GatewayRemoteToSocketBridgedUpstream simulates the full chain
// the production system runs:
//
//	docker / podman CLI → local gateway socket → HTTPS+Bearer →
//	    (cloudbox stub) → outpost socket-bridge (unix DialContext) →
//	    upstream unix socket (the "podman daemon")
//
// The cloudbox stub verifies Authorization: Bearer is present and forwards
// the request to the faux-outpost; the faux-outpost mirrors the
// production socket-bridge in outpost/internal/agent (scheme=unix +
// per-app Transport.DialContext); the upstream socket is an in-process
// stub that echoes the request path.
//
// Asserts:
//   - The CLI sees one consistent localhost socket regardless of where
//     the daemon actually lives (the whole reason the gateway exists).
//   - The Bearer token is read from the configured token file (cloudbox
//     auth boundary) and not exposed to the upstream.
//   - The token file can be replaced mid-flight and the next request
//     picks up the new value (cloudbox token rotation).
func TestE2E_GatewayRemoteToSocketBridgedUpstream(t *testing.T) {
	dir := shortTempDir(t)

	// 1) Upstream: unix-socket HTTP server stands in for the podman daemon.
	upstreamSock := filepath.Join(dir, "upstream.sock")
	upstreamLn, err := net.Listen("unix", upstreamSock)
	if err != nil {
		t.Fatalf("upstream listen: %v", err)
	}
	t.Cleanup(func() { _ = upstreamLn.Close() })
	var upstreamCalls atomic.Int32
	go func() {
		_ = http.Serve(upstreamLn, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			upstreamCalls.Add(1)
			// The upstream must not see the cloudbox Bearer — that's a
			// boundary token, stripped at outpost.
			if r.Header.Get("Authorization") != "" {
				t.Errorf("upstream saw Authorization header: %s", r.Header.Get("Authorization"))
			}
			_, _ = io.WriteString(w, "upstream-saw:"+r.URL.Path)
		}))
	}()

	// 2) Faux outpost: HTTP server that proxies /app/podman/* to the
	// upstream unix socket. Mirrors outpost's production socket-bridge.
	outpostTarget, _ := url.Parse("http://socket")
	outpostProxy := httputil.NewSingleHostReverseProxy(outpostTarget)
	outpostProxy.Transport = &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", upstreamSock)
		},
	}
	outpostMux := http.NewServeMux()
	outpostMux.HandleFunc("/app/podman/", func(w http.ResponseWriter, r *http.Request) {
		// Strip the outpost prefix the same way the production handler
		// does (outpost/internal/agent/apps.go:singleJoin).
		r.URL.Path = strings.TrimPrefix(r.URL.Path, "/app/podman")
		if r.URL.Path == "" {
			r.URL.Path = "/"
		}
		// Drop the cloudbox-edge bearer before forwarding — matches
		// host_proxy.go behaviour.
		r.Header.Del("Authorization")
		outpostProxy.ServeHTTP(w, r)
	})
	outpostSrv := httptest.NewServer(outpostMux)
	t.Cleanup(outpostSrv.Close)

	// 3) Faux cloudbox: HTTPS-ish reverse-proxy at /h/<host>/app/* that
	// verifies a Bearer token is present, then forwards to the outpost.
	const expectToken = "tok-secret-v1"
	var observedTokens []string
	var observedMu atomic.Int32 // gate for the observedTokens slice (cheap atomic-int as a guard)
	cloudboxMux := http.NewServeMux()
	cloudboxMux.HandleFunc("/h/", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		observedMu.Add(1)
		observedTokens = append(observedTokens, auth)
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "missing Bearer", http.StatusUnauthorized)
			return
		}
		// Forward /h/<host>/app/<rest> → outpost /app/<rest>.
		// Strip /h/<host>; gin in production does this via router params.
		const prefix = "/h/home-box"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			http.NotFound(w, r)
			return
		}
		r.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)
		// Forward to faux outpost using its full URL.
		outpostURL, _ := url.Parse(outpostSrv.URL)
		proxy := httputil.NewSingleHostReverseProxy(outpostURL)
		proxy.ServeHTTP(w, r)
	})
	cloudboxSrv := httptest.NewServer(cloudboxMux)
	t.Cleanup(cloudboxSrv.Close)

	// 4) Token file on disk — the gateway re-reads this on each request,
	// so we can rotate the token without restarting ycode.
	tokenPath := filepath.Join(dir, "cloudbox.token")
	if err := os.WriteFile(tokenPath, []byte(expectToken+"\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	// 5) Bring up the ycode gateway in REMOTE mode pointed at our cloudbox.
	gwSock := filepath.Join(dir, "gw.sock")
	g, err := Start(context.Background(), Config{
		Podman: PodmanBackend{
			Mode:      ModeRemote,
			URL:       cloudboxSrv.URL + "/h/home-box/app/podman/",
			TokenFile: tokenPath,
		},
		PodmanSocketPath: gwSock,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = g.Close() })

	// 6) Drive a request the way a CLI tool would — DOCKER_HOST is the
	// gateway socket; the tool has no idea cloudbox is in the loop.
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", gwSock)
		},
	}}
	resp, err := client.Get("http://podman/v4.0.0/libpod/info")
	if err != nil {
		t.Fatalf("cli get: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %q", resp.StatusCode, string(body))
	}
	if want := "upstream-saw:/v4.0.0/libpod/info"; string(body) != want {
		t.Errorf("end-to-end body = %q, want %q", string(body), want)
	}
	if got := upstreamCalls.Load(); got != 1 {
		t.Errorf("upstream calls = %d, want 1", got)
	}
	if len(observedTokens) == 0 || observedTokens[0] != "Bearer "+expectToken {
		t.Errorf("cloudbox observed tokens = %v, want first to be 'Bearer %s'", observedTokens, expectToken)
	}

	// 7) Rotate the token file mid-flight. The next request must pick
	// up the new value — this is how cloudbox token rotation propagates
	// without a ycode restart.
	const expectTokenV2 = "tok-rotated-v2"
	if err := os.WriteFile(tokenPath, []byte(expectTokenV2), 0o600); err != nil {
		t.Fatalf("rotate token: %v", err)
	}
	resp2, err := client.Get("http://podman/v4.0.0/libpod/_ping")
	if err != nil {
		t.Fatalf("post-rotate get: %v", err)
	}
	defer resp2.Body.Close()
	_, _ = io.Copy(io.Discard, resp2.Body)
	if len(observedTokens) < 2 || observedTokens[1] != "Bearer "+expectTokenV2 {
		t.Errorf("cloudbox observed tokens after rotation = %v, want second = 'Bearer %s'", observedTokens, expectTokenV2)
	}
}
