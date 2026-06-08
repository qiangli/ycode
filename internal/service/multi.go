package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/runtime/config"
)

// ctxKey is used for context-based parameter passing.
type ctxKey string

const (
	// CtxWorkDir is the context key for the project working directory.
	// Explicit override — when present, bypasses the workspace policy.
	CtxWorkDir ctxKey = "workDir"
	// CtxSessionOptions carries per-session overrides into CreateSession.
	CtxSessionOptions ctxKey = "sessionOptions"
	// CtxWorkspaceID names an existing per-session workspace to
	// reattach a new session to. Bypasses workspace allocation; the
	// resolver verifies the dir still exists on disk.
	CtxWorkspaceID ctxKey = "workspaceID"
	// CtxWorkspaceOwner is the resolved bearer-token email (or "local"
	// when unauthenticated). The resolver scopes workspaces by owner
	// so two users on the same `ycode serve` can't see each other's
	// directories.
	CtxWorkspaceOwner ctxKey = "workspaceOwner"
)

// MultiService implements Service with support for multiple concurrent sessions.
// Each session has its own AppBackend (with isolated config, tools, conversation)
// backed by a SessionPool. The bus is shared across all sessions.
type MultiService struct {
	pool *SessionPool
	b    bus.Bus

	// Per-session LocalService cache — each wraps the session's AppBackend.
	svcMu    sync.RWMutex
	services map[string]*LocalService // sessionID → LocalService

	// OllamaLister queries locally available Ollama models (optional).
	ollamaLister api.OllamaLister

	// CloudboxLister queries the cloudbox-pooled /v1/models gateway (optional).
	// When set, /api/models returns ONLY the cloudbox-pooled list — env-detected,
	// builtin, config, and Ollama models are intentionally excluded from the
	// serve-side surface.
	cloudboxLister api.CloudboxLister

	// WorkspaceResolver decides what workDir a new session attaches to
	// when the client doesn't pin one. Nil-safe: when unset, CreateSession
	// falls back to its pre-policy behavior (CtxWorkDir required from
	// the caller).
	resolver *WorkspaceResolver
}

// NewMultiService creates a multi-session service backed by a session pool.
func NewMultiService(pool *SessionPool, b bus.Bus) *MultiService {
	return &MultiService{
		pool:     pool,
		b:        b,
		services: make(map[string]*LocalService),
	}
}

// SetOllamaLister sets the callback for discovering local Ollama models.
func (m *MultiService) SetOllamaLister(lister api.OllamaLister) {
	m.ollamaLister = lister
}

// SetCloudboxLister sets the callback for discovering cloudbox-pooled models.
// When set, ListModels returns ONLY the cloudbox-pooled list.
func (m *MultiService) SetCloudboxLister(lister api.CloudboxLister) {
	m.cloudboxLister = lister
}

// SetWorkspaceResolver installs the workspace policy resolver. When
// set, CreateSession routes through it for callers that didn't pin a
// work_dir explicitly. Read by Resolver() so the HTTP layer can wire
// the /api/workspaces management surface to the same instance.
func (m *MultiService) SetWorkspaceResolver(r *WorkspaceResolver) {
	m.resolver = r
}

// Resolver returns the installed WorkspaceResolver, or nil. The HTTP
// server's /api/workspaces handlers use this to surface list/create/
// delete operations against the same on-disk tree that CreateSession
// allocates into.
func (m *MultiService) Resolver() *WorkspaceResolver {
	return m.resolver
}

func (m *MultiService) Bus() bus.Bus { return m.b }

// localService returns (or creates) a cached LocalService for the given session.
func (m *MultiService) localService(sessionID string) (*LocalService, error) {
	m.svcMu.RLock()
	svc, ok := m.services[sessionID]
	m.svcMu.RUnlock()
	if ok {
		return svc, nil
	}

	ms := m.pool.Get(sessionID)
	if ms == nil {
		return nil, fmt.Errorf("session %q not found", sessionID)
	}

	m.svcMu.Lock()
	defer m.svcMu.Unlock()
	// Double-check under write lock.
	if svc, ok := m.services[sessionID]; ok {
		return svc, nil
	}
	svc = NewLocalService(ms.App, m.b)
	svc.SetOllamaLister(m.ollamaLister)
	svc.SetCloudboxLister(m.cloudboxLister)
	m.services[sessionID] = svc
	return svc, nil
}

// --- Session lifecycle ---

func (m *MultiService) CreateSession(ctx context.Context) (*SessionInfo, error) {
	workDir, _ := ctx.Value(CtxWorkDir).(string)
	wsID, _ := ctx.Value(CtxWorkspaceID).(string)
	owner, _ := ctx.Value(CtxWorkspaceOwner).(string)
	opts, _ := ctx.Value(CtxSessionOptions).(SessionOptions)

	// When a workspace resolver is installed, route the caller through
	// it so the active --workspace-policy decides what dir the new
	// session attaches to. Falls back to the pre-policy contract
	// (CtxWorkDir required) when no resolver is set so embedded
	// (non-serve) callers keep working.
	if m.resolver != nil {
		ws, err := m.resolver.Resolve(ResolveHint{
			ExplicitWorkDir: workDir,
			WorkspaceID:     wsID,
			Owner:           owner,
		})
		if err != nil {
			return nil, fmt.Errorf("resolve workspace: %w", err)
		}
		workDir = ws.Path
	} else if workDir == "" {
		return nil, fmt.Errorf("work_dir required to create a session")
	}

	ms, err := m.pool.GetOrCreate(workDir)
	if err != nil {
		return nil, err
	}
	// Per-session overrides are stored on the ManagedSession so each
	// SendMessage can read them. Today multiple connections to the same
	// workDir share an App, so a later session's Options overwrite an
	// earlier one's — documented as a Path-1 limitation until G-I lands
	// (decoupled memex/conversation namespaces).
	if !opts.IsZero() {
		ms.Options = opts
	}
	return &SessionInfo{
		ID:           ms.ID,
		WorkDir:      ms.WorkDir,
		CreatedAt:    ms.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		MessageCount: ms.App.MessageCount(),
		Options:      optionsForResponse(ms.Options),
	}, nil
}

// optionsForResponse returns the options for echoing in a SessionInfo, or
// nil when no overrides are set so the JSON response omits the field.
func optionsForResponse(o SessionOptions) *SessionOptions {
	if o.IsZero() {
		return nil
	}
	copy := o
	return &copy
}

func (m *MultiService) GetSession(ctx context.Context, id string) (*SessionInfo, error) {
	ms := m.pool.Get(id)
	if ms == nil {
		return nil, fmt.Errorf("session %q not found", id)
	}
	return &SessionInfo{
		ID:           ms.ID,
		WorkDir:      ms.WorkDir,
		CreatedAt:    ms.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		MessageCount: ms.App.MessageCount(),
		Options:      optionsForResponse(ms.Options),
	}, nil
}

func (m *MultiService) ListSessions(ctx context.Context) ([]SessionInfo, error) {
	return m.pool.List(), nil
}

func (m *MultiService) GetMessages(ctx context.Context, sessionID string) ([]json.RawMessage, error) {
	svc, err := m.localService(sessionID)
	if err != nil {
		return nil, err
	}
	return svc.GetMessages(ctx, sessionID)
}

// --- Conversation (delegated to per-session LocalService) ---

func (m *MultiService) SendMessage(ctx context.Context, sessionID string, input MessageInput) error {
	svc, err := m.localService(sessionID)
	if err != nil {
		return err
	}
	// Surface the session's Options to the LocalService via the context,
	// so SendMessage can apply per-session overrides (G-G model swap, …)
	// before driving the agentic loop.
	if ms := m.pool.Get(sessionID); ms != nil && !ms.Options.IsZero() {
		ctx = context.WithValue(ctx, CtxSessionOptions, ms.Options)
	}
	return svc.SendMessage(ctx, sessionID, input)
}

func (m *MultiService) CancelTurn(ctx context.Context, sessionID string) error {
	svc, err := m.localService(sessionID)
	if err != nil {
		return err
	}
	return svc.CancelTurn(ctx, sessionID)
}

func (m *MultiService) RespondPermission(ctx context.Context, requestID string, allowed bool) error {
	// Permission responses are global — try all services.
	m.svcMu.RLock()
	defer m.svcMu.RUnlock()
	for _, svc := range m.services {
		if err := svc.RespondPermission(ctx, requestID, allowed); err == nil {
			return nil
		}
	}
	return fmt.Errorf("no pending permission request %q", requestID)
}

// --- Config and state (session-scoped via context or first session) ---

func (m *MultiService) GetConfig(ctx context.Context) (*config.Config, error) {
	// Use the first available session's config.
	sessions := m.pool.List()
	if len(sessions) == 0 {
		return nil, fmt.Errorf("no active sessions")
	}
	ms := m.pool.Get(sessions[0].ID)
	if ms == nil {
		return nil, fmt.Errorf("session not found")
	}
	return ms.App.Config(), nil
}

func (m *MultiService) SwitchModel(ctx context.Context, model string) error {
	// Switch model on all active sessions.
	m.svcMu.RLock()
	defer m.svcMu.RUnlock()
	for _, svc := range m.services {
		if _, err := svc.app.SwitchModel(model); err != nil {
			return err
		}
	}
	return nil
}

func (m *MultiService) GetStatus(ctx context.Context) (*StatusInfo, error) {
	policy := ""
	if m.resolver != nil {
		policy = string(m.resolver.Policy())
	}
	sessions := m.pool.List()
	if len(sessions) == 0 {
		return &StatusInfo{Version: "dev", WorkspacePolicy: policy}, nil
	}
	// Return status from the first session (caller should specify via context in future).
	ms := m.pool.Get(sessions[0].ID)
	if ms == nil {
		return &StatusInfo{Version: "dev", WorkspacePolicy: policy}, nil
	}
	// Under per-session policy the seeded session is rooted at the
	// server's startup dir, NOT a per-session sandbox — so the web UI
	// must NOT auto-adopt it. Hide the session_id from /status under
	// that policy; the client then POSTs /api/sessions to allocate or
	// reattach to a real per-session workspace.
	info := &StatusInfo{
		Model:           ms.App.Model(),
		ProviderKind:    ms.App.ProviderKind(),
		PlanMode:        ms.App.InPlanMode(),
		Version:         ms.App.Version(),
		WorkspacePolicy: policy,
	}
	if policy != string(PolicyPerSession) {
		info.SessionID = ms.ID
		info.WorkDir = ms.WorkDir
	}
	return info, nil
}

func (m *MultiService) ListModels(ctx context.Context) ([]api.ModelInfo, error) {
	// `ycode serve` is wired cloudbox-first: the operator points at a
	// cloudbox deployment via DHNT_BASE_URL (+ DHNT_API_KEY) and the
	// pooled /v1/models list is the single source of truth here.
	// builtin / config / env / Ollama entries are deliberately omitted
	// so clients see exactly what the gateway will route — no surprise
	// "model exists locally but the gateway can't reach it" mismatches.
	return api.DiscoverCloudboxOnly(ctx, m.cloudboxLister), nil
}

func (m *MultiService) ExecuteCommand(ctx context.Context, name string, args string) (string, error) {
	// Execute on first session (REST endpoint — session context not available).
	sessions := m.pool.List()
	if len(sessions) == 0 {
		return "", fmt.Errorf("no active sessions")
	}
	ms := m.pool.Get(sessions[0].ID)
	if ms == nil {
		return "", fmt.Errorf("session not found")
	}
	return ms.App.ExecuteCommand(ctx, name, args)
}

// LookupApp returns (or creates) the App backing the given workDir. The
// workDir is required: an empty string fails. Used by stateless wire
// endpoints (/api/extract, /api/embed) that need access to the
// per-tenant provider/config without going through the agentic loop.
func (m *MultiService) LookupApp(ctx context.Context, workDir string) (AppBackend, error) {
	if workDir == "" {
		return nil, fmt.Errorf("work_dir required")
	}
	ms, err := m.pool.GetOrCreate(workDir)
	if err != nil {
		return nil, err
	}
	return ms.App, nil
}

// RemoveSession removes a session and its cached LocalService.
func (m *MultiService) RemoveSession(sessionID string) error {
	m.svcMu.Lock()
	delete(m.services, sessionID)
	m.svcMu.Unlock()
	return m.pool.Remove(sessionID)
}

// Close shuts down all sessions.
func (m *MultiService) Close() error {
	m.svcMu.Lock()
	m.services = make(map[string]*LocalService)
	m.svcMu.Unlock()
	return m.pool.Close()
}
