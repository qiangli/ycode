package gateway

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// startOllama opens 127.0.0.1:0 (kernel-assigned port) and serves a
// reverse proxy to the configured backend.
func (g *Gateway) startOllama() error {
	mux, err := g.ollamaMux()
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen 127.0.0.1: %w", err)
	}
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logServeError("ollama", err)
		}
	}()

	g.ollamaLn = ln
	g.ollamaSrv = srv
	g.endpoints.OllamaURL = "http://" + ln.Addr().String()
	g.endpoints.OllamaMode = g.cfg.Ollama.Mode
	return nil
}

func (g *Gateway) ollamaMux() (http.Handler, error) {
	switch g.cfg.Ollama.Mode {
	case ModeEmbedded:
		if g.cfg.Ollama.Upstream == "" {
			return nil, errors.New("embedded mode requires Ollama.Upstream (http URL of the local runner)")
		}
		target, err := url.Parse(strings.TrimRight(g.cfg.Ollama.Upstream, "/") + "/")
		if err != nil {
			return nil, fmt.Errorf("parse Ollama.Upstream: %w", err)
		}
		return newReverseProxy(target, http.DefaultTransport), nil
	case ModeRemote:
		if g.cfg.Ollama.URL == "" {
			return nil, errors.New("remote mode requires Ollama.URL")
		}
		target, err := url.Parse(strings.TrimRight(g.cfg.Ollama.URL, "/") + "/")
		if err != nil {
			return nil, fmt.Errorf("parse Ollama.URL: %w", err)
		}
		base := &http.Transport{
			TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
			IdleConnTimeout:   60 * time.Second,
			ForceAttemptHTTP2: true,
		}
		tr := &bearerInjector{base: base, tokenFile: g.cfg.Ollama.TokenFile}
		return newReverseProxy(target, tr), nil
	default:
		return nil, fmt.Errorf("unknown ollama mode %q", g.cfg.Ollama.Mode)
	}
}
