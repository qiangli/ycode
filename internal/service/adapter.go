package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/qiangli/ycode/internal/bus"
)

// ExternalAdapter bridges an external messaging platform (Slack, Discord, etc.)
// to ycode coding sessions. Each adapter translates between the platform's wire
// protocol and the ycode bus event protocol.
type ExternalAdapter interface {
	// ID returns the adapter's platform identifier (e.g., "slack", "discord").
	ID() string

	// Start begins listening for messages from the external platform.
	// The handler is called for each inbound message.
	Start(ctx context.Context, handler InboundHandler) error

	// Send delivers a ycode event to the external platform.
	Send(ctx context.Context, externalRef string, event bus.Event) error

	// Stop gracefully shuts down the adapter.
	Stop(ctx context.Context) error
}

// InboundHandler processes messages arriving from external platforms.
type InboundHandler interface {
	// OnMessage handles an incoming message, creating or reusing a session.
	// externalRef is a platform-specific identifier (e.g., "slack:W123:C456").
	// channelKind identifies the platform (e.g., "slack", "discord").
	OnMessage(ctx context.Context, externalRef, channelKind, text string) error
}

// ExternalSessionMap maps external platform references to ycode session IDs.
// Thread-safe for concurrent access from adapter goroutines.
type ExternalSessionMap struct {
	mu      sync.RWMutex
	mapping map[string]string // "slack:W123:C456" → sessionID
	reverse map[string]string // sessionID → externalRef (for outbound routing)
}

// NewExternalSessionMap creates a new session mapping.
func NewExternalSessionMap() *ExternalSessionMap {
	return &ExternalSessionMap{
		mapping: make(map[string]string),
		reverse: make(map[string]string),
	}
}

// Get returns the sessionID for an external reference, or "" if not mapped.
func (m *ExternalSessionMap) Get(externalRef string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.mapping[externalRef]
}

// GetExternal returns the external reference for a sessionID, or "" if not mapped.
func (m *ExternalSessionMap) GetExternal(sessionID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.reverse[sessionID]
}

// Set maps an external reference to a session ID (bidirectional).
func (m *ExternalSessionMap) Set(externalRef, sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mapping[externalRef] = sessionID
	m.reverse[sessionID] = externalRef
}

// Remove deletes a mapping by external reference.
func (m *ExternalSessionMap) Remove(externalRef string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sid, ok := m.mapping[externalRef]; ok {
		delete(m.reverse, sid)
	}
	delete(m.mapping, externalRef)
}

// AdapterManager manages external adapters and routes messages between them
// and the MultiService.
type AdapterManager struct {
	svc        *MultiService
	pool       *SessionPool
	sessionMap *ExternalSessionMap
	adapters   map[string]ExternalAdapter
	mu         sync.RWMutex
}

// NewAdapterManager creates a manager for external adapters.
func NewAdapterManager(svc *MultiService, pool *SessionPool) *AdapterManager {
	return &AdapterManager{
		svc:        svc,
		pool:       pool,
		sessionMap: NewExternalSessionMap(),
		adapters:   make(map[string]ExternalAdapter),
	}
}

// Register adds an adapter. Call before Start.
func (am *AdapterManager) Register(adapter ExternalAdapter) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.adapters[adapter.ID()] = adapter
}

// Start launches all registered adapters.
func (am *AdapterManager) Start(ctx context.Context) error {
	am.mu.RLock()
	defer am.mu.RUnlock()
	for _, adapter := range am.adapters {
		if err := adapter.Start(ctx, am); err != nil {
			slog.Error("adapter start failed", "adapter", adapter.ID(), "error", err)
			return fmt.Errorf("start adapter %s: %w", adapter.ID(), err)
		}
		slog.Info("adapter started", "adapter", adapter.ID())
	}
	return nil
}

// Stop shuts down all adapters.
func (am *AdapterManager) Stop(ctx context.Context) {
	am.mu.RLock()
	defer am.mu.RUnlock()
	for _, adapter := range am.adapters {
		if err := adapter.Stop(ctx); err != nil {
			slog.Warn("adapter stop error", "adapter", adapter.ID(), "error", err)
		}
	}
}

// OnMessage implements InboundHandler — routes external messages to ycode sessions.
func (am *AdapterManager) OnMessage(ctx context.Context, externalRef, channelKind, text string) error {
	// Look up or create session for this external reference.
	sessionID := am.sessionMap.Get(externalRef)
	if sessionID == "" {
		// Create a new session. Use the external ref as a synthetic workDir
		// so each external conversation gets its own isolated session.
		workDir := fmt.Sprintf("/external/%s/%s", channelKind, externalRef)
		ctx = context.WithValue(ctx, CtxWorkDir, workDir)
		info, err := am.svc.CreateSession(ctx)
		if err != nil {
			return fmt.Errorf("create session for %s: %w", externalRef, err)
		}
		sessionID = info.ID
		am.sessionMap.Set(externalRef, sessionID)
		slog.Info("adapter: created session",
			"external_ref", externalRef,
			"channel", channelKind,
			"session_id", sessionID,
		)
	}

	// Send the message to the session.
	return am.svc.SendMessage(ctx, sessionID, MessageInput{Text: text})
}

// ForwardEvents subscribes to a session's events and forwards them to the
// appropriate adapter. Call this after session creation.
func (am *AdapterManager) ForwardEvents(ctx context.Context, sessionID string) {
	externalRef := am.sessionMap.GetExternal(sessionID)
	if externalRef == "" {
		return
	}

	// Determine which adapter handles this ref.
	// The channelKind is encoded in the externalRef mapping path.
	am.mu.RLock()
	defer am.mu.RUnlock()

	ch, unsub := am.svc.Bus().Subscribe(bus.EventTextDelta, bus.EventTurnComplete, bus.EventCommandComplete)
	go func() {
		defer unsub()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				if ev.SessionID != sessionID {
					continue
				}
				// Forward to all adapters (only the matching one will have the ref).
				for _, adapter := range am.adapters {
					_ = adapter.Send(ctx, externalRef, ev)
				}
			}
		}
	}()
}

// SessionMap returns the external session mapping (for testing/inspection).
func (am *AdapterManager) SessionMap() *ExternalSessionMap {
	return am.sessionMap
}
