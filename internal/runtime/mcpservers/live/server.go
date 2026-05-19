package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	telotel "github.com/qiangli/ycode/internal/telemetry/otel"
)

// hub owns the websocket connection to the (one) currently-connected
// browser extension client and routes request/response pairs.
type hub struct {
	addr string
	srv  *http.Server
	up   websocket.Upgrader

	mu      sync.Mutex
	conn    *websocket.Conn // nil when no extension connected
	pending map[int64]chan wsResponse

	// Populated by the extension's `_hello` frame on connect. Used by
	// version-drift detection and by /health / /connected reporters.
	extVersion     string
	extMethods     []string
	extPermissions []string

	// helloReceived is closed when the current connection's `_hello`
	// frame has been parsed. Re-created on every handleWS, so each
	// reconnect starts a fresh handshake. awaitHello blocks on this
	// to gate every dispatched call — pre-0.4 extensions that never
	// send _hello hit the LiveHandshakeTimeout and surface a clear
	// stale-extension error instead of timing out the inner call.
	helloReceived chan struct{}

	// lastTab is the URL the extension last reported (via navigate /
	// extract result). Surfaced in the "extension not connected"
	// error so a fresh agent can re-attach to the right tab.
	lastTabURL string

	nextID atomic.Int64
}

func newHub(port int) *hub {
	return &hub{
		addr:    fmt.Sprintf("127.0.0.1:%d", port),
		up:      websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }},
		pending: make(map[int64]chan wsResponse),
	}
}

// start binds the listener and spawns the http.Server in the
// background. Returns an error if the port is already in use, per the
// plan ("refuse and prompt for a different port").
func (h *hub) start(ctx context.Context) error {
	ln, err := net.Listen("tcp", h.addr)
	if err != nil {
		return fmt.Errorf("live: bind %s: %w (set browser.livePort in settings.json to pick a different port)", h.addr, err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", h.handleWS)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	// /connected reports whether the extension's websocket is up
	// without poking the extension itself. Used by `ycode browser
	// doctor` to surface real-time connection state plus the version
	// and method-list reported by the extension's _hello handshake —
	// so users can self-diagnose stale-extension drift before they
	// hit it inside a session.
	mux.HandleFunc("/connected", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		ver := h.ExtVersion()
		methods := h.ExtMethods()
		stale := ver != "" && versionLess(ver, LiveExtensionMinVersion)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"connected":     h.connected(),
			"version":       ver,
			"methods_count": len(methods),
			"min_version":   LiveExtensionMinVersion,
			"stale":         stale,
		})
	})
	// /dispatch is the cross-process bridge: a ycode prompt running
	// in a separate process can POST a {method, params} JSON here
	// instead of binding its own hub. The hub owner (typically
	// `ycode serve`) forwards over the websocket and returns the
	// extension's response synchronously. 30 s ceiling per call.
	mux.HandleFunc("/dispatch", h.handleDispatch)
	h.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := h.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Warn("live: http.Server.Serve", "error", err)
		}
	}()
	slog.Info("live: listening for extension", "addr", h.addr)
	return nil
}

func (h *hub) stop(ctx context.Context) error {
	h.mu.Lock()
	if h.conn != nil {
		_ = h.conn.Close()
		h.conn = nil
	}
	srv := h.srv
	h.mu.Unlock()
	if srv != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
	return nil
}

func (h *hub) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := h.up.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("live: upgrade", "error", err)
		return
	}
	h.mu.Lock()
	if h.conn != nil {
		// Replace any prior connection — the extension popup
		// reconnects when the user clicks Connect again.
		_ = h.conn.Close()
	}
	h.conn = conn
	// Reset hello state for the new connection; the extension will
	// resend _hello as its first frame.
	h.extVersion = ""
	h.extMethods = nil
	h.extPermissions = nil
	h.helloReceived = make(chan struct{})
	h.mu.Unlock()
	telotel.RecordBrowserHubConnect(r.Context())
	slog.Info("live: extension connected", "remote", r.RemoteAddr)

	go h.readLoop(conn)
}

func (h *hub) readLoop(conn *websocket.Conn) {
	defer func() {
		h.mu.Lock()
		if h.conn == conn {
			h.conn = nil
		}
		// Fail any in-flight requests for this connection.
		for id, ch := range h.pending {
			ch <- wsResponse{ID: id, Error: "extension disconnected"}
			delete(h.pending, id)
		}
		h.mu.Unlock()
		_ = conn.Close()
		telotel.RecordBrowserHubDisconnect(context.Background())
		slog.Info("live: extension disconnected")
	}()

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var resp wsResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			slog.Warn("live: bad response from extension", "raw", string(raw), "error", err)
			continue
		}
		// Unsolicited frames (id == 0) carry a `method` field: today
		// only `_hello` for the version handshake.
		if resp.ID == 0 && resp.Method != "" {
			h.handleUnsolicited(resp)
			continue
		}
		h.mu.Lock()
		ch, ok := h.pending[resp.ID]
		if ok {
			delete(h.pending, resp.ID)
		}
		h.mu.Unlock()
		if ok {
			ch <- resp
		}
	}
}

// handleUnsolicited processes frames the extension pushes without a
// matching request. Today: `_hello` (version handshake).
func (h *hub) handleUnsolicited(resp wsResponse) {
	if resp.Method != "_hello" {
		return
	}
	var hi extHello
	if err := json.Unmarshal(resp.Result, &hi); err != nil {
		slog.Warn("live: bad _hello payload", "error", err)
		return
	}
	h.mu.Lock()
	firstHello := h.extVersion == ""
	h.extVersion = hi.Version
	h.extMethods = hi.Methods
	h.extPermissions = hi.Permissions
	hr := h.helloReceived
	h.mu.Unlock()
	if firstHello && hr != nil {
		close(hr)
	}
	slog.Info("live: extension hello", "version", hi.Version, "methods", len(hi.Methods))
}

// ExtVersion returns the version reported by the extension's _hello
// frame, or "" if no _hello has been received (older extension).
func (h *hub) ExtVersion() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.extVersion
}

// ExtMethods returns the dispatch table reported by the extension's
// _hello frame; empty for older extensions.
func (h *hub) ExtMethods() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.extMethods == nil {
		return nil
	}
	out := make([]string, len(h.extMethods))
	copy(out, h.extMethods)
	return out
}

// ExtPermissions returns the chrome.* permissions reported by the
// extension's _hello frame.
func (h *hub) ExtPermissions() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.extPermissions == nil {
		return nil
	}
	out := make([]string, len(h.extPermissions))
	copy(out, h.extPermissions)
	return out
}

// LastTabURL returns the most recent URL the extension reported.
func (h *hub) LastTabURL() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastTabURL
}

// RecordLastTab updates the last-known tab URL. Called when an action
// result includes a URL, so the "not connected" error can show the
// tab the agent was last working with.
func (h *hub) RecordLastTab(url string) {
	if url == "" {
		return
	}
	h.mu.Lock()
	h.lastTabURL = url
	h.mu.Unlock()
}

// awaitHello blocks until the extension's `_hello` frame is parsed
// for the current connection, the ctx deadline expires, or
// LiveHandshakeTimeout elapses. A pre-0.4.0 extension never sends
// _hello, so it surfaces here as a stale-extension error instead of
// silently failing a downstream call with "unknown method: X".
func (h *hub) awaitHello(ctx context.Context) error {
	h.mu.Lock()
	ver := h.extVersion
	ch := h.helloReceived
	h.mu.Unlock()
	if ver != "" {
		return nil
	}
	if ch == nil {
		return errors.New("live: extension connection missing handshake channel")
	}
	timer := time.NewTimer(LiveHandshakeTimeout)
	defer timer.Stop()
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return errors.New("live: extension did not send _hello within 3s — likely an older extension (pre-0.4.0). " +
			"Reload it at chrome://extensions, or run: ycode browser install-extension")
	}
}

// call sends a request and waits for the matching response or ctx
// cancellation. Gated by awaitHello so we never dispatch against a
// pre-handshake extension.
func (h *hub) call(ctx context.Context, method string, params map[string]any) (wsResponse, error) {
	h.mu.Lock()
	conn := h.conn
	lastTab := h.lastTabURL
	h.mu.Unlock()
	if conn == nil {
		return wsResponse{}, errors.New(notConnectedError(lastTab))
	}
	if err := h.awaitHello(ctx); err != nil {
		return wsResponse{}, err
	}

	id := h.nextID.Add(1)
	req := wsRequest{ID: id, Method: method, Params: params}
	raw, err := json.Marshal(req)
	if err != nil {
		return wsResponse{}, err
	}

	ch := make(chan wsResponse, 1)
	h.mu.Lock()
	h.pending[id] = ch
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.pending, id)
		h.mu.Unlock()
	}()

	if err := conn.WriteMessage(websocket.TextMessage, raw); err != nil {
		return wsResponse{}, fmt.Errorf("live: write: %w", err)
	}

	select {
	case resp := <-ch:
		return resp, nil
	case <-ctx.Done():
		return wsResponse{}, ctx.Err()
	}
}

// connected reports whether an extension is currently attached. Used
// by `ycode browser doctor`.
func (h *hub) connected() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.conn != nil
}

// notConnectedError builds the user-facing "extension not connected"
// message with as much actionable context as we have. Includes the
// last-known tab URL when the extension previously connected, so a
// reattach lands on the right tab; otherwise lists the setup paths.
func notConnectedError(lastTab string) string {
	base := "live: extension not connected. " +
		"(a) reload at chrome://extensions if recently updated; " +
		"(b) open the popup on your target tab and click Connect; " +
		"(c) first-time setup: ycode browser setup live"
	if lastTab != "" {
		base += " — last tab: " + lastTab
	}
	return base
}

// handleDispatch is the cross-process forwarder. Body:
//
//	{"method": "navigate", "params": {"url": "..."}}
//
// Response:
//
//	{"result": {...}}        // success
//	{"error":  "..."}        // failure (HTTP 200 with error field)
//
// Returns 503 when no extension is currently connected.
func (h *hub) handleDispatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("bad json: %v", err), http.StatusBadRequest)
		return
	}
	if req.Method == "" {
		http.Error(w, "method required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Span + metrics for the cross-process path. The agent-side
	// Manager.Execute is instrumented separately; this branch
	// catches `yc tab`, `curl /dispatch`, and any external client.
	url, _ := req.Params["url"].(string)
	sel, _ := req.Params["selector"].(string)
	ctx, finish := telotel.StartBrowserActionSpan(ctx, "live", req.Method, url, sel)
	resp, err := h.call(ctx, req.Method, req.Params)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		// Extension not connected, or write to socket failed.
		finish("BLOCKED", nil, err)
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	outcome := "SUCCESS"
	if resp.Error != "" {
		outcome = "BLOCKED"
	}
	finish(outcome, nil, nil)
	// resp.Result is already a json.RawMessage; either path goes
	// through to the caller untouched.
	_ = json.NewEncoder(w).Encode(map[string]any{
		"result": resp.Result,
		"error":  resp.Error,
	})
}
