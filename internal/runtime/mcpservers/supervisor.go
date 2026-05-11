//go:build experimental

package mcpservers

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	telotel "github.com/qiangli/ycode/internal/telemetry/otel"
)

// Manager owns one or more backend Services and routes BrowserActions to
// the configured default. The same Manager instance is shared by every
// browser_* tool handler in a session.
type Manager struct {
	mu       sync.Mutex
	default_ string
	services map[string]Service
}

// NewManager returns an empty Manager. Add backends with Register, then
// SetDefault to pick which one BrowserAction calls go to.
func NewManager() *Manager {
	return &Manager{services: make(map[string]Service)}
}

// Register adds a backend. Registering the same name twice replaces the
// previous instance.
func (m *Manager) Register(svc Service) {
	if svc == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.services[svc.Name()] = svc
}

// SetDefault selects which backend Execute routes to. The chosen backend
// must already be registered.
func (m *Manager) SetDefault(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.services[name]; !ok {
		return fmt.Errorf("mcpservers: unknown backend %q", name)
	}
	m.default_ = name
	return nil
}

// Default returns the currently selected backend name (may be empty).
func (m *Manager) Default() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.default_
}

// Get returns a backend by name.
func (m *Manager) Get(name string) (Service, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	svc, ok := m.services[name]
	return svc, ok
}

// List returns all registered backend names. Order is not deterministic.
func (m *Manager) List() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.services))
	for name := range m.services {
		names = append(names, name)
	}
	return names
}

// Execute routes a BrowserAction to the default backend, lazily readying
// it on first use. A Pulse span wraps the whole call and emits metrics
// via finish(outcome, hints, err).
func (m *Manager) Execute(ctx context.Context, action BrowserAction) (*BrowserResult, error) {
	m.mu.Lock()
	name := m.default_
	svc := m.services[name]
	m.mu.Unlock()

	if svc == nil {
		return nil, fmt.Errorf("mcpservers: no default backend configured (registered: %v)", m.List())
	}

	ctx, finish := telotel.StartBrowserActionSpan(ctx, name, action.Type, action.URL, action.Selector)
	if err := svc.EnsureReady(ctx); err != nil {
		err = fmt.Errorf("mcpservers: %s: ready: %w", name, err)
		finish("ERROR", nil, err)
		return nil, err
	}
	res, err := svc.Execute(ctx, action)
	var (
		outcome string
		hints   []string
	)
	if res != nil {
		outcome = res.OutcomeClass
		hints = res.Hints
	}
	finish(outcome, hints, err)
	return res, err
}

// StopAll stops every registered backend. Errors are logged but not
// returned individually; the first error is propagated.
func (m *Manager) StopAll(ctx context.Context) error {
	m.mu.Lock()
	services := make([]Service, 0, len(m.services))
	for _, svc := range m.services {
		services = append(services, svc)
	}
	m.mu.Unlock()

	var firstErr error
	for _, svc := range services {
		if err := svc.Stop(ctx); err != nil {
			slog.Warn("mcpservers: stop failed", "backend", svc.Name(), "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}
