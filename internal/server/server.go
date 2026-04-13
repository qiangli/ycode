package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.opentelemetry.io/otel/trace"

	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/service"
	"github.com/qiangli/ycode/internal/web"
)

// Config holds server configuration.
type Config struct {
	Port     int
	Hostname string
	Token    string // bearer token for authentication
}

// Server is the HTTP + WebSocket API server.
type Server struct {
	config  Config
	service service.Service
	mux     *http.ServeMux
	server  *http.Server
	logger  *slog.Logger

	// WebSocket connection tracking.
	wsMu    sync.Mutex
	wsConns map[*websocket.Conn]struct{}

	upgrader websocket.Upgrader

	// OTEL instrumentation (optional).
	otelCfg     *OTELConfig
	otelMetrics *otelMetrics
	tracer      trace.Tracer
}

// New creates a new API server.
func New(cfg Config, svc service.Service) *Server {
	s := &Server{
		config:  cfg,
		service: svc,
		mux:     http.NewServeMux(),
		logger:  slog.Default(),
		wsConns: make(map[*websocket.Conn]struct{}),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // allow all origins for local dev
			},
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
	}
	s.registerRoutes()
	return s
}

// SetOTEL configures optional observability instrumentation.
// Must be called before Start.
func (s *Server) SetOTEL(cfg *OTELConfig) {
	s.setupOTEL(cfg)
}

// Start begins listening on the configured address.
func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.config.Hostname, s.config.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	s.server = &http.Server{
		Handler:      s.otelMiddleware(s.corsMiddleware(s.requestLogger(s.mux))),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // no timeout for WebSocket/SSE
		IdleTimeout:  120 * time.Second,
	}

	s.logger.Info("API server listening", "addr", addr)
	go func() {
		if err := s.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.logger.Error("server error", "error", err)
		}
	}()
	return nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop(ctx context.Context) error {
	// Close all WebSocket connections.
	s.wsMu.Lock()
	for conn := range s.wsConns {
		conn.Close()
	}
	s.wsConns = make(map[*websocket.Conn]struct{})
	s.wsMu.Unlock()

	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

// Mux returns the HTTP mux for testing with httptest.Server.
func (s *Server) Mux() *http.ServeMux {
	return s.mux
}

// Addr returns the listen address. Only valid after Start.
func (s *Server) Addr() string {
	return fmt.Sprintf("%s:%d", s.config.Hostname, s.config.Port)
}

// registerRoutes sets up all HTTP and WebSocket routes.
func (s *Server) registerRoutes() {
	// REST endpoints.
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/config", s.authMiddleware(s.handleGetConfig))
	s.mux.HandleFunc("PUT /api/config/model", s.authMiddleware(s.handleSwitchModel))
	s.mux.HandleFunc("GET /api/sessions", s.authMiddleware(s.handleListSessions))
	s.mux.HandleFunc("POST /api/sessions", s.authMiddleware(s.handleCreateSession))
	s.mux.HandleFunc("GET /api/sessions/{id}", s.authMiddleware(s.handleGetSession))
	s.mux.HandleFunc("GET /api/sessions/{id}/messages", s.authMiddleware(s.handleGetMessages))
	s.mux.HandleFunc("POST /api/commands/{name}", s.authMiddleware(s.handleCommand))
	s.mux.HandleFunc("GET /api/status", s.authMiddleware(s.handleGetStatus))

	// WebSocket endpoint.
	s.mux.HandleFunc("GET /api/sessions/{id}/ws", s.handleWebSocket)

	// Web UI (embedded SPA).
	s.mux.Handle("/", web.Handler())
}

// authMiddleware wraps a handler with bearer token authentication.
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.config.Token == "" {
			next(w, r)
			return
		}
		token := r.Header.Get("Authorization")
		if token == "Bearer "+s.config.Token {
			next(w, r)
			return
		}
		// Also check query parameter (for browser/WebSocket).
		if r.URL.Query().Get("token") == s.config.Token {
			next(w, r)
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}
}

// corsMiddleware adds CORS headers for browser clients.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requestLogger logs HTTP requests.
func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		// Skip logging WebSocket upgrades and static files.
		if r.Header.Get("Upgrade") == "" && !strings.HasPrefix(r.URL.Path, "/api/") {
			return
		}
		s.logger.Debug("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start))
	})
}

// --- REST handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.service.GetConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleSwitchModel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.service.SwitchModel(r.Context(), body.Model); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"model": body.Model})
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.service.ListSessions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	info, err := s.service.CreateSession(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, info)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	info, err := s.service.GetSession(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleGetMessages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	msgs, err := s.service.GetMessages(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (s *Server) handleCommand(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var body struct {
		Args string `json:"args"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	result, err := s.service.ExecuteCommand(r.Context(), name, body.Args)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"result": result})
}

func (s *Server) handleGetStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.service.GetStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// --- WebSocket handler ---

// wsMessage is a message from the WebSocket client.
type wsMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Auth check for WebSocket upgrade.
	if s.config.Token != "" {
		token := r.URL.Query().Get("token")
		auth := r.Header.Get("Authorization")
		if token != s.config.Token && auth != "Bearer "+s.config.Token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	sessionID := r.PathValue("id")

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("websocket upgrade failed", "error", err)
		return
	}

	// Track connection.
	s.wsMu.Lock()
	s.wsConns[conn] = struct{}{}
	s.wsMu.Unlock()
	s.trackWSConnect()

	defer func() {
		s.wsMu.Lock()
		delete(s.wsConns, conn)
		s.wsMu.Unlock()
		s.trackWSDisconnect()
		conn.Close()
	}()

	// Replay missed events if last_event_id is provided.
	if lastIDStr := r.URL.Query().Get("last_event_id"); lastIDStr != "" {
		if memBus, ok := s.service.Bus().(*bus.MemoryBus); ok {
			var lastID uint64
			fmt.Sscanf(lastIDStr, "%d", &lastID)
			for _, ev := range memBus.Replay(lastID) {
				if ev.SessionID == "" || ev.SessionID == sessionID {
					data, _ := json.Marshal(ev)
					conn.WriteMessage(websocket.TextMessage, data)
				}
			}
		}
	}

	// Subscribe to bus events for this session.
	ch, unsub := s.service.Bus().Subscribe()
	defer unsub()

	// Write loop: send bus events to client.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go s.wsWriteLoop(ctx, conn, ch, sessionID)

	// Read loop: receive client commands.
	s.wsReadLoop(ctx, conn, sessionID)
}

// wsWriteLoop sends bus events to the WebSocket client.
func (s *Server) wsWriteLoop(ctx context.Context, conn *websocket.Conn, ch <-chan bus.Event, sessionID string) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			// Only send events for this session.
			if ev.SessionID != "" && ev.SessionID != sessionID {
				continue
			}
			data, err := json.Marshal(ev)
			if err != nil {
				s.logger.Error("marshal event", "error", err)
				continue
			}
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				s.logger.Debug("websocket write error", "error", err)
				return
			}
		case <-ticker.C:
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// wsReadLoop processes incoming WebSocket messages.
func (s *Server) wsReadLoop(ctx context.Context, conn *websocket.Conn, sessionID string) {
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				s.logger.Debug("websocket read error", "error", err)
			}
			return
		}

		var msg wsMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			s.logger.Error("invalid websocket message", "error", err)
			continue
		}

		s.handleWSMessage(ctx, sessionID, msg)
	}
}

// handleWSMessage dispatches a client WebSocket command to the service.
func (s *Server) handleWSMessage(ctx context.Context, sessionID string, msg wsMessage) {
	switch bus.EventType(msg.Type) {
	case bus.EventMessageSend:
		var input service.MessageInput
		if err := json.Unmarshal(msg.Data, &input); err != nil {
			s.logger.Error("invalid message.send payload", "error", err)
			return
		}
		go func() {
			if err := s.service.SendMessage(ctx, sessionID, input); err != nil {
				s.logger.Error("send message failed", "session", sessionID, "error", err)
				// Publish error event.
				errData, _ := json.Marshal(map[string]string{"error": err.Error()})
				s.service.Bus().Publish(bus.Event{
					Type:      bus.EventTurnError,
					SessionID: sessionID,
					Data:      errData,
				})
			}
		}()
	case bus.EventPermissionRes:
		var resp struct {
			RequestID string `json:"request_id"`
			Allowed   bool   `json:"allowed"`
		}
		if err := json.Unmarshal(msg.Data, &resp); err != nil {
			s.logger.Error("invalid permission.respond payload", "error", err)
			return
		}
		if err := s.service.RespondPermission(ctx, resp.RequestID, resp.Allowed); err != nil {
			s.logger.Error("respond permission failed", "error", err)
		}
	case bus.EventTurnCancel:
		if err := s.service.CancelTurn(ctx, sessionID); err != nil {
			s.logger.Error("cancel turn failed", "session", sessionID, "error", err)
		}
	default:
		s.logger.Warn("unknown websocket message type", "type", msg.Type)
	}
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
