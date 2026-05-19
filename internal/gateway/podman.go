package gateway

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// startPodman opens the local socket (or named pipe) and serves either
// a reverse-proxy to the embedded libpod socket (Mode=embedded) or to
// the cloudbox URL with Bearer injection (Mode=remote).
func (g *Gateway) startPodman() error {
	sockPath := g.cfg.PodmanSocketPath
	if sockPath == "" {
		p, err := PodmanSocketPath()
		if err != nil {
			return err
		}
		sockPath = p
	}

	// Stale socket from a previous run that didn't shut down cleanly.
	if _, err := os.Stat(sockPath); err == nil {
		_ = os.Remove(sockPath)
	}

	ln, err := listenPodman(sockPath)
	if err != nil {
		return fmt.Errorf("listen %s: %w", sockPath, err)
	}

	mux, err := g.podmanMux()
	if err != nil {
		_ = ln.Close()
		return err
	}

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			// The serve loop terminates on Close — log other errors.
			logServeError("podman", err)
		}
	}()

	g.podmanLn = ln
	g.podmanSrv = srv
	g.endpoints.PodmanSocket = sockPath
	g.endpoints.PodmanMode = g.cfg.Podman.Mode
	return nil
}

// podmanMux is an http.Handler that proxies every request to the
// configured backend. We use a Handler (not just a ReverseProxy) so we
// can return a clear 503 when the upstream URL is missing, rather than
// crashing with a nil-deref.
func (g *Gateway) podmanMux() (http.Handler, error) {
	switch g.cfg.Podman.Mode {
	case ModeEmbedded:
		upstream := strings.TrimPrefix(g.cfg.Podman.Upstream, "unix://")
		if upstream == "" {
			return nil, errors.New("embedded mode requires Podman.Upstream (unix socket path)")
		}
		target, _ := url.Parse("http://socket")
		tr := &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", upstream)
			},
			MaxIdleConns:       4,
			IdleConnTimeout:    30 * time.Second,
			DisableCompression: true,
		}
		return newReverseProxy(target, tr), nil
	case ModeRemote:
		if g.cfg.Podman.URL == "" {
			return nil, errors.New("remote mode requires Podman.URL")
		}
		target, err := url.Parse(strings.TrimRight(g.cfg.Podman.URL, "/") + "/")
		if err != nil {
			return nil, fmt.Errorf("parse Podman.URL: %w", err)
		}
		base := &http.Transport{
			TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
			IdleConnTimeout:   60 * time.Second,
			ForceAttemptHTTP2: true,
		}
		tr := &bearerInjector{base: base, tokenFile: g.cfg.Podman.TokenFile}
		return newReverseProxy(target, tr), nil
	default:
		return nil, fmt.Errorf("unknown podman mode %q", g.cfg.Podman.Mode)
	}
}
