package server

import (
	"context"
	"crypto/subtle"
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

	"github.com/qiangli/ycode/internal/buildinfo"
	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/memwatch"
	"github.com/qiangli/ycode/internal/service"
	"github.com/qiangli/ycode/internal/web"
	"github.com/qiangli/ycode/pkg/ycode/actor"
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
	hub     *Hub
	groups  *service.GroupManager
	mux     *http.ServeMux
	server  *http.Server
	logger  *slog.Logger

	// WebSocket connection tracking.
	wsMu         sync.Mutex
	wsConns      map[*websocket.Conn]struct{}
	lastActivity time.Time // last connect/disconnect/message time

	upgrader websocket.Upgrader

	// Lazily-initialized embedding provider for /api/embed* endpoints.
	// Process-wide today (env-var driven); held on Server to keep tests
	// hermetic and to allow per-server overrides if a future deployment
	// wants tenant-specific embedders.
	embedOnce sync.Once
	embedProv embeddingProvider

	// OTEL instrumentation (optional).
	otelCfg     *OTELConfig
	otelMetrics *otelMetrics

	// Stops the memwatch sampler goroutine on Stop().
	memwatchCancel context.CancelFunc
	tracer         trace.Tracer
}

// embeddingProvider matches embedding.Provider but is kept local so the
// server file does not import internal/runtime/embedding at the type level.
type embeddingProvider interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Dimensions() int
}

// New creates a new API server.
func New(cfg Config, svc service.Service) *Server {
	hub := NewHub()
	s := &Server{
		config:       cfg,
		service:      svc,
		hub:          hub,
		groups:       service.NewGroupManager(),
		mux:          http.NewServeMux(),
		logger:       slog.Default(),
		wsConns:      make(map[*websocket.Conn]struct{}),
		lastActivity: time.Now(),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // allow all origins for local dev
			},
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
	}
	// Start the hub and wire bus events into it.
	go hub.Run()
	go s.busToHub()
	s.registerRoutes()
	return s
}

// Hub returns the server's connection hub.
func (s *Server) Hub() *Hub { return s.hub }

// busToHub subscribes to all bus events and dispatches them through the hub.
func (s *Server) busToHub() {
	ch, unsub := s.service.Bus().Subscribe()
	defer unsub()
	for ev := range ch {
		s.hub.Dispatch(ev)
	}
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

	// Self-instrumentation: once a minute, sample own RSS/heap plus
	// bus and WebSocket counters. Quiet (Debug) when healthy; Warn/
	// Error when the process balloons — the post-mortem trail the
	// 2026-06 OOM incidents lacked.
	mwCtx, cancel := context.WithCancel(context.Background())
	s.memwatchCancel = cancel
	var lastPublished uint64
	memwatch.Start(mwCtx, "ycode-serve", s.logger, func() []any {
		attrs := []any{"ws_conns", s.ConnCount()}
		if mb, ok := s.service.Bus().(*bus.MemoryBus); ok {
			cur := mb.Published()
			attrs = append(attrs, "bus_events_1m", cur-lastPublished, "bus_events_total", cur)
			lastPublished = cur
		}
		return attrs
	})
	return nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop(ctx context.Context) error {
	if s.memwatchCancel != nil {
		s.memwatchCancel()
	}
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

// ConnCount returns the number of active WebSocket connections.
func (s *Server) ConnCount() int {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	return len(s.wsConns)
}

// LastActivity returns the time of the last client connect/disconnect/message.
func (s *Server) LastActivity() time.Time {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	return s.lastActivity
}

// Addr returns the listen address. Only valid after Start.
func (s *Server) Addr() string {
	return fmt.Sprintf("%s:%d", s.config.Hostname, s.config.Port)
}

// registerRoutes sets up all HTTP and WebSocket routes.
func (s *Server) registerRoutes() {
	// REST endpoints.
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /version", s.handleVersion)
	s.mux.HandleFunc("GET /api/version", s.handleVersion)
	s.mux.HandleFunc("GET /api/config", s.authMiddleware(s.handleGetConfig))
	s.mux.HandleFunc("PUT /api/config/model", s.authMiddleware(s.handleSwitchModel))
	s.mux.HandleFunc("GET /api/sessions", s.authMiddleware(s.handleListSessions))
	s.mux.HandleFunc("POST /api/sessions", s.authMiddleware(s.handleCreateSession))
	s.mux.HandleFunc("GET /api/sessions/{id}", s.authMiddleware(s.handleGetSession))
	s.mux.HandleFunc("GET /api/sessions/{id}/messages", s.authMiddleware(s.handleGetMessages))
	s.mux.HandleFunc("POST /api/commands/{name}", s.authMiddleware(s.handleCommand))
	s.mux.HandleFunc("GET /api/models", s.authMiddleware(s.handleListModels))
	s.mux.HandleFunc("GET /api/status", s.authMiddleware(s.handleGetStatus))

	// Stateless one-shot endpoints (no session, no agent loop).
	s.mux.HandleFunc("POST /api/extract", s.authMiddleware(s.handleExtract))
	s.mux.HandleFunc("POST /api/embed", s.authMiddleware(s.handleEmbed))
	s.mux.HandleFunc("POST /api/embed/batch", s.authMiddleware(s.handleEmbedBatch))
	s.mux.HandleFunc("GET /api/embed/dimensions", s.authMiddleware(s.handleEmbedDimensions))

	// Group endpoints (team agent coordination).
	s.mux.HandleFunc("GET /api/groups", s.authMiddleware(s.handleListGroups))
	s.mux.HandleFunc("POST /api/groups", s.authMiddleware(s.handleCreateGroup))
	s.mux.HandleFunc("GET /api/groups/{id}", s.authMiddleware(s.handleGetGroup))
	s.mux.HandleFunc("DELETE /api/groups/{id}", s.authMiddleware(s.handleDeleteGroup))
	s.mux.HandleFunc("PUT /api/groups/{id}/sessions/{sid}", s.authMiddleware(s.handleAddSessionToGroup))
	s.mux.HandleFunc("DELETE /api/groups/{id}/sessions/{sid}", s.authMiddleware(s.handleRemoveSessionFromGroup))

	// Workspace management — list, create, and delete per-session
	// workspace directories under ~/.agents/ycode/workspaces/<owner>/.
	// All scoped to the bearer-token owner; the resolver enforces
	// path-traversal safety.
	s.mux.HandleFunc("GET /api/workspaces", s.authMiddleware(s.handleListWorkspaces))
	s.mux.HandleFunc("POST /api/workspaces", s.authMiddleware(s.handleCreateWorkspace))
	s.mux.HandleFunc("DELETE /api/workspaces/{id}", s.authMiddleware(s.handleDeleteWorkspace))

	// WebSocket endpoint. Uses authMiddlewareWS so the upgrade can also
	// authenticate via the ?token= query parameter, since browsers cannot
	// set arbitrary headers on a WebSocket upgrade request.
	s.mux.HandleFunc("GET /api/sessions/{id}/ws", s.authMiddlewareWS(s.handleWebSocket))

	// Web UI (embedded SPA). HandlerWithToken inlines the bearer into
	// the served index.html so the canvas + chat surfaces can open
	// the authed WebSocket without the user pasting ?token= manually.
	// When config.Token is empty, this is identical to web.Handler().
	s.mux.Handle("/", web.HandlerWithToken(s.config.Token))
}

// authMiddleware wraps a REST handler with Bearer token authentication and
// stamps an actor.User onto the request context decoded from the X-Actor-*
// headers.
//
// Auth modes:
//   - Server.config.Token == ""  → permissive; identity headers are still
//     decoded (useful for local dev / single-tenant TUI).
//   - Server.config.Token != ""  → require Authorization: Bearer <token>;
//     reject with 401 on mismatch. Identity headers are decoded only after
//     the bearer check passes, so end-user identity cannot be spoofed by an
//     unauthenticated caller.
//
// Header convention (consumed when present):
//
//	X-Actor-User:  <stable-id>
//	X-Actor-Email: <email>
//	X-Actor-Roles: <comma-separated>
//	X-Actor-Extra-<key>: <value>   (zero or more; key is title-cased)
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.checkBearer(r, "") {
			writeError(w, http.StatusUnauthorized, fmt.Errorf("invalid or missing bearer token"))
			return
		}
		if u, ok := decodeActorHeaders(r); ok {
			r = r.WithContext(actor.WithUser(r.Context(), u))
		}
		next(w, r)
	}
}

// authMiddlewareWS is the WebSocket-aware variant of authMiddleware. It
// accepts the bearer token via the Authorization header OR via a ?token=
// query parameter (since browsers cannot set headers on the upgrade
// request).
func (s *Server) authMiddlewareWS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		queryToken := r.URL.Query().Get("token")
		if !s.checkBearer(r, queryToken) {
			writeError(w, http.StatusUnauthorized, fmt.Errorf("invalid or missing bearer token"))
			return
		}
		if u, ok := decodeActorHeaders(r); ok {
			r = r.WithContext(actor.WithUser(r.Context(), u))
		}
		next(w, r)
	}
}

// checkBearer validates the request's bearer token against s.config.Token.
// When Token is empty (dev mode), the check passes unconditionally. When a
// fallback token is supplied (e.g. from a query parameter for WebSockets),
// it is accepted in addition to the Authorization header.
func (s *Server) checkBearer(r *http.Request, fallback string) bool {
	if s.config.Token == "" {
		return true
	}
	expected := []byte(s.config.Token)
	if got := bearerFromAuthHeader(r); got != "" {
		if subtle.ConstantTimeCompare([]byte(got), expected) == 1 {
			return true
		}
	}
	if fallback != "" && subtle.ConstantTimeCompare([]byte(fallback), expected) == 1 {
		return true
	}
	return false
}

// bearerFromAuthHeader extracts the token from "Authorization: Bearer …".
// Returns "" if the header is absent or malformed.
func bearerFromAuthHeader(r *http.Request) string {
	v := r.Header.Get("Authorization")
	if v == "" {
		return ""
	}
	const prefix = "Bearer "
	if len(v) < len(prefix) || !strings.EqualFold(v[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(v[len(prefix):])
}

// decodeActorHeaders pulls X-Actor-User / X-Actor-Email / X-Actor-Roles /
// X-Actor-Extra-* headers off the request and returns an actor.User. The
// boolean is false (and the User is zero) when no X-Actor-User is present —
// the caller skips actor.WithUser in that case.
func decodeActorHeaders(r *http.Request) (actor.User, bool) {
	id := strings.TrimSpace(r.Header.Get("X-Actor-User"))
	if id == "" {
		return actor.User{}, false
	}
	u := actor.User{
		ID:    id,
		Email: strings.TrimSpace(r.Header.Get("X-Actor-Email")),
	}
	if rolesHdr := r.Header.Get("X-Actor-Roles"); rolesHdr != "" {
		for role := range strings.SplitSeq(rolesHdr, ",") {
			if role = strings.TrimSpace(role); role != "" {
				u.Roles = append(u.Roles, role)
			}
		}
	}
	const extraPrefix = "X-Actor-Extra-"
	for name, vals := range r.Header {
		if !strings.HasPrefix(name, extraPrefix) || len(vals) == 0 {
			continue
		}
		if u.Extra == nil {
			u.Extra = make(map[string]string)
		}
		key := strings.ToLower(name[len(extraPrefix):])
		u.Extra[key] = vals[0]
	}
	return u, true
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

// handleVersion reports the binary's identity (ldflags tag + commit,
// VCS details when embedded). Unauthenticated by design — same tier
// as /api/health; it identifies the build, not the user's data.
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, buildinfo.Get())
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
	// Extract workDir + session_options + workspace hints from request
	// body or header for multi-project / multi-tenant support.
	//
	// Resolution priority (highest first):
	//   1. work_dir / X-Work-Dir       — explicit absolute path
	//   2. workspace_id                — reattach an existing per-session
	//                                    workspace by ID
	//   3. nothing                     — fall through to the workspace
	//                                    policy (per-session by default)
	//
	// The workspace owner is the authenticated user's email when bearer
	// auth is on, else "local" — sanitized inside the resolver. See
	// service.WorkspaceResolver.
	ctx := r.Context()
	var body struct {
		WorkDir     string                  `json:"work_dir"`
		WorkspaceID string                  `json:"workspace_id,omitempty"`
		Options     *service.SessionOptions `json:"session_options,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.WorkDir == "" {
		body.WorkDir = r.Header.Get("X-Work-Dir")
	}
	if body.WorkDir != "" {
		ctx = context.WithValue(ctx, service.CtxWorkDir, body.WorkDir)
	}
	if body.WorkspaceID != "" {
		ctx = context.WithValue(ctx, service.CtxWorkspaceID, body.WorkspaceID)
	}
	ctx = context.WithValue(ctx, service.CtxWorkspaceOwner, callerOwner(r))
	if body.Options != nil && !body.Options.IsZero() {
		ctx = context.WithValue(ctx, service.CtxSessionOptions, *body.Options)
	}
	info, err := s.service.CreateSession(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, info)
}

// callerOwner derives the workspace-owner identity for the current
// request. Today the auth chain stamps the authenticated email on
// the request via the actor middleware; absent that, the resolver
// maps "" to "local" so unauthenticated dev deployments still get a
// stable, scoped owner directory.
func callerOwner(r *http.Request) string {
	// Prefer the actor-middleware-stamped header if present (set by
	// the bearer/cookie auth chains upstream of this handler).
	if v := r.Header.Get("X-Actor-Email"); v != "" {
		return v
	}
	return ""
}

// resolverOrError returns the WorkspaceResolver attached to the
// underlying MultiService, writing a 503 to the response when no
// resolver is configured (an embedded / single-session setup).
func (s *Server) resolverOrError(w http.ResponseWriter) *service.WorkspaceResolver {
	ms, ok := s.service.(*service.MultiService)
	if !ok || ms.Resolver() == nil {
		writeError(w, http.StatusServiceUnavailable,
			fmt.Errorf("workspaces not enabled on this server"))
		return nil
	}
	return ms.Resolver()
}

// workspaceJSON is the wire shape for workspace responses. Mirrors the
// service.Workspace struct but flattens CreatedAt to RFC3339 so the
// JSON consumer doesn't have to know Go's time encoding.
type workspaceJSON struct {
	ID        string `json:"id"`
	Owner     string `json:"owner"`
	Path      string `json:"path"`
	CreatedAt string `json:"created_at"`
}

func toWorkspaceJSON(ws service.Workspace) workspaceJSON {
	return workspaceJSON{
		ID:        ws.ID,
		Owner:     ws.Owner,
		Path:      ws.Path,
		CreatedAt: ws.CreatedAt.Format(time.RFC3339),
	}
}

// handleListWorkspaces returns the per-session workspaces existing on
// disk for the calling owner. Sorted newest-first by mod time so the
// dropdown's first entry is the most-recently-used.
func (s *Server) handleListWorkspaces(w http.ResponseWriter, r *http.Request) {
	resolver := s.resolverOrError(w)
	if resolver == nil {
		return
	}
	owner := callerOwner(r)
	list, err := resolver.List(owner)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]workspaceJSON, 0, len(list))
	for _, ws := range list {
		out = append(out, toWorkspaceJSON(ws))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"workspaces": out,
		"policy":     string(resolver.Policy()),
	})
}

// handleCreateWorkspace allocates a new empty workspace and returns
// its descriptor. Useful for the "+ new workspace" affordance in the
// UI when the user wants to prepare a workspace before attaching a
// session to it.
func (s *Server) handleCreateWorkspace(w http.ResponseWriter, r *http.Request) {
	resolver := s.resolverOrError(w)
	if resolver == nil {
		return
	}
	owner := callerOwner(r)
	ws, err := resolver.CreateNew(owner)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, toWorkspaceJSON(ws))
}

// handleDeleteWorkspace RemoveAlls the workspace directory. Sessions
// currently attached to the deleted workspace are NOT proactively
// closed — the next message they send will fail (filesystem missing)
// and the client can recover by attaching to a fresh workspace. A
// proactive-close pass is a follow-up.
func (s *Server) handleDeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	resolver := s.resolverOrError(w)
	if resolver == nil {
		return
	}
	owner := callerOwner(r)
	id := r.PathValue("id")
	if err := resolver.Delete(owner, id); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
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

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.service.ListModels(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, models)
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
	sessionID := r.PathValue("id")
	clientID := ClientID(r.URL.Query().Get("client_id"))
	workDir := r.URL.Query().Get("work_dir")
	clientKind := ClientKind(r.URL.Query().Get("client_kind"))
	if clientKind == "" {
		clientKind = ClientTUI
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("websocket upgrade failed", "error", err)
		return
	}

	// Track connection.
	s.wsMu.Lock()
	s.wsConns[conn] = struct{}{}
	s.lastActivity = time.Now()
	s.wsMu.Unlock()
	s.trackWSConnect()

	// Register client with hub for session-aware event routing.
	// If no client_id provided, generate one from the connection pointer.
	if clientID == "" {
		clientID = ClientID(fmt.Sprintf("conn-%p", conn))
	}
	client := &Client{
		ID:        clientID,
		Kind:      clientKind,
		SessionID: sessionID,
		WorkDir:   workDir,
		Send:      make(chan bus.Event, 256),
		JoinedAt:  time.Now(),
	}
	s.hub.Register(client)

	defer func() {
		s.hub.Unregister(clientID)
		s.wsMu.Lock()
		delete(s.wsConns, conn)
		s.lastActivity = time.Now()
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

	// Write loop: drain client.Send channel to WebSocket.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go s.clientWritePump(ctx, conn, client)

	// Read loop: receive client commands.
	s.wsReadLoop(ctx, conn, sessionID)
}

// clientWritePump drains events from the client's Send channel to the WebSocket.
func (s *Server) clientWritePump(ctx context.Context, conn *websocket.Conn, client *Client) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-client.Send:
			if !ok {
				return // channel closed by hub on unregister
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
