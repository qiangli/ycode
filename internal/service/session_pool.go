package service

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// AppFactory creates a new AppBackend for a given project directory.
type AppFactory func(workDir string) (AppBackend, error)

// ManagedSession wraps a session with its associated App backend.
type ManagedSession struct {
	ID         string
	App        AppBackend
	WorkDir    string
	CreatedAt  time.Time
	LastActive time.Time
}

// SessionPool manages multiple AppBackend instances, one per active session.
// It maps session IDs to ManagedSessions and indexes by working directory
// so that clients from the same project reuse the same session.
type SessionPool struct {
	mu       sync.RWMutex
	sessions map[string]*ManagedSession // sessionID → managed session
	dirIndex map[string]string          // workDir → sessionID
	factory  AppFactory
}

// NewSessionPool creates a session pool with the given app factory.
func NewSessionPool(factory AppFactory) *SessionPool {
	return &SessionPool{
		sessions: make(map[string]*ManagedSession),
		dirIndex: make(map[string]string),
		factory:  factory,
	}
}

// SeedSession registers an existing AppBackend (e.g., the primary server app)
// into the pool without going through the factory.
func (p *SessionPool) SeedSession(app AppBackend) {
	p.mu.Lock()
	defer p.mu.Unlock()
	sessionID := app.SessionID()
	workDir := app.WorkDir()
	ms := &ManagedSession{
		ID:         sessionID,
		App:        app,
		WorkDir:    workDir,
		CreatedAt:  time.Now(),
		LastActive: time.Now(),
	}
	p.sessions[sessionID] = ms
	if workDir != "" {
		p.dirIndex[workDir] = sessionID
	}
}

// Get returns the ManagedSession for the given sessionID, or nil if not found.
func (p *SessionPool) Get(sessionID string) *ManagedSession {
	p.mu.RLock()
	defer p.mu.RUnlock()
	ms := p.sessions[sessionID]
	if ms != nil {
		ms.LastActive = time.Now()
	}
	return ms
}

// GetByWorkDir returns the ManagedSession for the given workDir, or nil if none exists.
func (p *SessionPool) GetByWorkDir(workDir string) *ManagedSession {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if sid, ok := p.dirIndex[workDir]; ok {
		ms := p.sessions[sid]
		if ms != nil {
			ms.LastActive = time.Now()
		}
		return ms
	}
	return nil
}

// GetOrCreate returns an existing session for the workDir, or creates a new one.
func (p *SessionPool) GetOrCreate(workDir string) (*ManagedSession, error) {
	// Fast path: check if session exists.
	if ms := p.GetByWorkDir(workDir); ms != nil {
		return ms, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check under write lock.
	if sid, ok := p.dirIndex[workDir]; ok {
		if ms, ok := p.sessions[sid]; ok {
			ms.LastActive = time.Now()
			return ms, nil
		}
	}

	// Create new App for this workDir.
	app, err := p.factory(workDir)
	if err != nil {
		return nil, fmt.Errorf("create app for %s: %w", workDir, err)
	}

	sessionID := app.SessionID()
	ms := &ManagedSession{
		ID:         sessionID,
		App:        app,
		WorkDir:    workDir,
		CreatedAt:  time.Now(),
		LastActive: time.Now(),
	}
	p.sessions[sessionID] = ms
	p.dirIndex[workDir] = sessionID

	slog.Info("session pool: created session",
		"session_id", sessionID,
		"work_dir", workDir,
		"pool_size", len(p.sessions),
	)
	return ms, nil
}

// Remove closes and removes a session from the pool.
func (p *SessionPool) Remove(sessionID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	ms, ok := p.sessions[sessionID]
	if !ok {
		return nil
	}
	delete(p.sessions, sessionID)
	delete(p.dirIndex, ms.WorkDir)
	return ms.App.Close()
}

// List returns info for all active sessions.
func (p *SessionPool) List() []SessionInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	infos := make([]SessionInfo, 0, len(p.sessions))
	for _, ms := range p.sessions {
		infos = append(infos, SessionInfo{
			ID:           ms.ID,
			WorkDir:      ms.WorkDir,
			CreatedAt:    ms.CreatedAt.Format(time.RFC3339),
			MessageCount: ms.App.MessageCount(),
		})
	}
	return infos
}

// Count returns the number of active sessions.
func (p *SessionPool) Count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.sessions)
}

// Close shuts down all sessions in the pool.
func (p *SessionPool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, ms := range p.sessions {
		if err := ms.App.Close(); err != nil {
			slog.Warn("session pool: close error", "session_id", id, "error", err)
		}
	}
	p.sessions = make(map[string]*ManagedSession)
	p.dirIndex = make(map[string]string)
	return nil
}
