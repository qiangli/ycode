package observability

import (
	"context"
	"net/http"
	"sync/atomic"
)

// WebUIComponent wraps the ycode API/WebSocket server as an observability
// stack component so it appears on the proxy landing page.
// Mounted in-process on the proxy — no separate port needed.
type WebUIComponent struct {
	handler http.Handler
	healthy atomic.Bool
}

// NewWebUIComponent creates a component that serves the ycode web UI
// and API, accessible via the proxy landing page at /ycode/.
func NewWebUIComponent(handler http.Handler) *WebUIComponent {
	return &WebUIComponent{handler: handler}
}

func (w *WebUIComponent) Name() string { return "ycode" }

func (w *WebUIComponent) Start(ctx context.Context) error {
	w.healthy.Store(true)
	return nil
}

func (w *WebUIComponent) Stop(ctx context.Context) error {
	w.healthy.Store(false)
	return nil
}

func (w *WebUIComponent) Healthy() bool {
	return w.healthy.Load()
}

// HTTPHandler returns the handler for in-process mounting on the proxy.
// The proxy's StripPrefix removes the /ycode/ prefix before passing
// requests to this handler, so the mux sees /api/*, /, etc.
func (w *WebUIComponent) HTTPHandler() http.Handler {
	return w.handler
}
