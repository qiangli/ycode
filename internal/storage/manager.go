package storage

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

// Manager orchestrates all storage backends with progressive initialization.
//
// Phase 1 (instant, <50ms): KV store opens for config/permission cache.
// Phase 2 (background, 1-2s): SQLite database opens with migrations.
// Phase 3 (lazy, on demand): Bleve search index and chromem-go vector store.
type Manager struct {
	mu sync.RWMutex

	dataDir string // e.g. ~/.ycode/projects/{hash}/

	kv     KVStore
	sql    SQLStore
	vector VectorStore
	search SearchIndex

	status Status

	// Factories for lazy initialization.
	sqlFactory    func(ctx context.Context) (SQLStore, error)
	vectorFactory func(ctx context.Context) (VectorStore, error)
	searchFactory func(ctx context.Context) (SearchIndex, error)

	// Background init signals.
	sqlReady    chan struct{}
	vectorReady chan struct{}
	searchReady chan struct{}
}

// Config holds factory functions for creating storage backends.
type Config struct {
	DataDir       string
	KVFactory     func(ctx context.Context) (KVStore, error)
	SQLFactory    func(ctx context.Context) (SQLStore, error)
	VectorFactory func(ctx context.Context) (VectorStore, error)
	SearchFactory func(ctx context.Context) (SearchIndex, error)
}

// NewManager creates a storage manager and immediately initializes the KV store (Phase 1).
func NewManager(ctx context.Context, cfg Config) (*Manager, error) {
	m := &Manager{
		dataDir:       cfg.DataDir,
		sqlFactory:    cfg.SQLFactory,
		vectorFactory: cfg.VectorFactory,
		searchFactory: cfg.SearchFactory,
		sqlReady:      make(chan struct{}),
		vectorReady:   make(chan struct{}),
		searchReady:   make(chan struct{}),
	}

	// Phase 1: KV store (instant).
	if cfg.KVFactory != nil {
		kv, err := cfg.KVFactory(ctx)
		if err != nil {
			return nil, fmt.Errorf("init kv store: %w", err)
		}
		m.kv = kv
		m.status.KV = BackendStatus{Phase: Phase1, Ready: true, Since: time.Now()}
	}

	return m, nil
}

// InitSQL initializes the SQLite database (Phase 2).
// Typically called in a background goroutine after startup.
func (m *Manager) InitSQL(ctx context.Context) error {
	if m.sqlFactory == nil {
		close(m.sqlReady)
		return nil
	}

	sql, err := m.sqlFactory(ctx)
	if err != nil {
		m.mu.Lock()
		m.status.SQL = BackendStatus{Phase: Phase2, Error: err.Error()}
		m.mu.Unlock()
		close(m.sqlReady)
		return fmt.Errorf("init sql store: %w", err)
	}

	if err := sql.Migrate(ctx); err != nil {
		sql.Close()
		m.mu.Lock()
		m.status.SQL = BackendStatus{Phase: Phase2, Error: err.Error()}
		m.mu.Unlock()
		close(m.sqlReady)
		return fmt.Errorf("run migrations: %w", err)
	}

	m.mu.Lock()
	m.sql = sql
	m.status.SQL = BackendStatus{Phase: Phase2, Ready: true, Since: time.Now()}
	m.mu.Unlock()
	close(m.sqlReady)
	return nil
}

// InitVector initializes the vector store (Phase 3).
func (m *Manager) InitVector(ctx context.Context) error {
	if m.vectorFactory == nil {
		close(m.vectorReady)
		return nil
	}

	vector, err := m.vectorFactory(ctx)
	if err != nil {
		m.mu.Lock()
		m.status.Vector = BackendStatus{Phase: Phase3, Error: err.Error()}
		m.mu.Unlock()
		close(m.vectorReady)
		return fmt.Errorf("init vector store: %w", err)
	}

	m.mu.Lock()
	m.vector = vector
	m.status.Vector = BackendStatus{Phase: Phase3, Ready: true, Since: time.Now()}
	m.mu.Unlock()
	close(m.vectorReady)
	return nil
}

// InitSearch initializes the search index (Phase 3).
func (m *Manager) InitSearch(ctx context.Context) error {
	if m.searchFactory == nil {
		close(m.searchReady)
		return nil
	}

	search, err := m.searchFactory(ctx)
	if err != nil {
		m.mu.Lock()
		m.status.Search = BackendStatus{Phase: Phase3, Error: err.Error()}
		m.mu.Unlock()
		close(m.searchReady)
		return fmt.Errorf("init search index: %w", err)
	}

	m.mu.Lock()
	m.search = search
	m.status.Search = BackendStatus{Phase: Phase3, Ready: true, Since: time.Now()}
	m.mu.Unlock()
	close(m.searchReady)
	return nil
}

// StartBackground launches Phase 2 and Phase 3 initialization in goroutines.
func (m *Manager) StartBackground(ctx context.Context) {
	go func() {
		_ = m.InitSQL(ctx)
	}()
	go func() {
		_ = m.InitVector(ctx)
	}()
	go func() {
		_ = m.InitSearch(ctx)
	}()
}

// KV returns the KV store. Panics if not initialized (should always be ready after NewManager).
func (m *Manager) KV() KVStore {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.kv
}

// SQL returns the SQL store, blocking until Phase 2 completes.
// Returns nil if SQL initialization failed or was not configured.
func (m *Manager) SQL(ctx context.Context) SQLStore {
	select {
	case <-m.sqlReady:
	case <-ctx.Done():
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sql
}

// Vector returns the vector store, blocking until Phase 3 completes.
// Returns nil if vector initialization failed or was not configured.
func (m *Manager) Vector(ctx context.Context) VectorStore {
	select {
	case <-m.vectorReady:
	case <-ctx.Done():
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.vector
}

// Search returns the search index, blocking until Phase 3 completes.
// Returns nil if search initialization failed or was not configured.
func (m *Manager) Search(ctx context.Context) SearchIndex {
	select {
	case <-m.searchReady:
	case <-ctx.Done():
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.search
}

// SQLReady returns a channel that closes when SQL is ready.
func (m *Manager) SQLReady() <-chan struct{} { return m.sqlReady }

// VectorReady returns a channel that closes when the vector store is ready.
func (m *Manager) VectorReady() <-chan struct{} { return m.vectorReady }

// SearchReady returns a channel that closes when the search index is ready.
func (m *Manager) SearchReady() <-chan struct{} { return m.searchReady }

// Status returns the current status of all backends.
func (m *Manager) Status() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

// DataDir returns the base data directory.
func (m *Manager) DataDir() string {
	return m.dataDir
}

// Close shuts down all storage backends.
func (m *Manager) Close() error {
	var closers []io.Closer

	m.mu.Lock()
	if m.search != nil {
		closers = append(closers, m.search)
	}
	if m.vector != nil {
		closers = append(closers, m.vector)
	}
	if m.sql != nil {
		closers = append(closers, m.sql)
	}
	if m.kv != nil {
		closers = append(closers, m.kv)
	}
	m.mu.Unlock()

	var firstErr error
	for _, c := range closers {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
