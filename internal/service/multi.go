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
	CtxWorkDir ctxKey = "workDir"
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
	m.services[sessionID] = svc
	return svc, nil
}

// --- Session lifecycle ---

func (m *MultiService) CreateSession(ctx context.Context) (*SessionInfo, error) {
	workDir, _ := ctx.Value(CtxWorkDir).(string)
	if workDir == "" {
		return nil, fmt.Errorf("work_dir required to create a session")
	}

	ms, err := m.pool.GetOrCreate(workDir)
	if err != nil {
		return nil, err
	}
	return &SessionInfo{
		ID:           ms.ID,
		WorkDir:      ms.WorkDir,
		CreatedAt:    ms.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		MessageCount: ms.App.MessageCount(),
	}, nil
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
	sessions := m.pool.List()
	if len(sessions) == 0 {
		return &StatusInfo{Version: "dev"}, nil
	}
	// Return status from the first session (caller should specify via context in future).
	ms := m.pool.Get(sessions[0].ID)
	if ms == nil {
		return &StatusInfo{Version: "dev"}, nil
	}
	return &StatusInfo{
		Model:        ms.App.Model(),
		ProviderKind: ms.App.ProviderKind(),
		SessionID:    ms.ID,
		WorkDir:      ms.WorkDir,
		PlanMode:     ms.App.InPlanMode(),
		Version:      ms.App.Version(),
	}, nil
}

func (m *MultiService) ListModels(ctx context.Context) ([]api.ModelInfo, error) {
	// Models are global — use any session's config.
	sessions := m.pool.List()
	var aliases map[string]string
	if len(sessions) > 0 {
		ms := m.pool.Get(sessions[0].ID)
		if ms != nil {
			aliases = ms.App.Config().Aliases
		}
	}
	return api.DiscoverModels(ctx, aliases, m.ollamaLister), nil
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
