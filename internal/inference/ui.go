package inference

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"

	ollamaweb "github.com/qiangli/ycode/internal/inference/web"
)

// OllamaUIComponent provides the Ollama management web UI.
// It auto-detects a running Ollama server (managed by ycode or standalone)
// and proxies API requests to it. The management SPA is always available.
//
// This component is always registered on the stack, regardless of whether
// the inference engine is explicitly enabled in config.
type OllamaUIComponent struct {
	ollamaURL string // resolved Ollama API URL
	healthy   atomic.Bool
}

// NewOllamaUIComponent creates a UI component that auto-detects Ollama.
func NewOllamaUIComponent() *OllamaUIComponent {
	return &OllamaUIComponent{}
}

func (u *OllamaUIComponent) Name() string { return "ollama" }

func (u *OllamaUIComponent) Start(ctx context.Context) error {
	// Auto-detect Ollama server.
	candidates := []string{
		DefaultOllamaURL(), // OLLAMA_HOST env or localhost:11434
	}

	for _, url := range candidates {
		if DetectOllamaServer(ctx, url) {
			u.ollamaURL = url
			u.healthy.Store(true)
			slog.Info("ollama-ui: detected Ollama server", "url", url)
			return nil
		}
	}

	// No running Ollama found — UI still works, just shows "disconnected".
	u.ollamaURL = DefaultOllamaURL()
	slog.Info("ollama-ui: no running Ollama detected, UI will show disconnected state", "default", u.ollamaURL)
	return nil
}

func (u *OllamaUIComponent) Stop(ctx context.Context) error {
	u.healthy.Store(false)
	return nil
}

func (u *OllamaUIComponent) Healthy() bool {
	return true // UI is always healthy; Ollama connectivity is shown in the SPA
}

// HTTPHandler returns a composite handler: /api/* proxied to Ollama,
// everything else served from the embedded management SPA.
func (u *OllamaUIComponent) HTTPHandler() http.Handler {
	staticHandler := ollamaweb.Handler()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			// Proxy to Ollama server.
			target, err := url.Parse(u.ollamaURL)
			if err != nil {
				http.Error(w, fmt.Sprintf("invalid ollama URL: %v", err), http.StatusBadGateway)
				return
			}
			proxy := httputil.NewSingleHostReverseProxy(target)
			proxy.ServeHTTP(w, r)
			return
		}
		staticHandler.ServeHTTP(w, r)
	})
}
