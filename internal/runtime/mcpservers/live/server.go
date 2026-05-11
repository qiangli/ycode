//go:build experimental

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
	// doctor` to surface real-time connection state.
	mux.HandleFunc("/connected", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"connected": h.connected()})
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

// call sends a request and waits for the matching response or ctx
// cancellation.
func (h *hub) call(ctx context.Context, method string, params map[string]any) (wsResponse, error) {
	h.mu.Lock()
	conn := h.conn
	h.mu.Unlock()
	if conn == nil {
		return wsResponse{}, errors.New("live: extension not connected — open the popup and click Connect on the target tab")
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
