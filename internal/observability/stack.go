package observability

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"sync"

	"github.com/qiangli/ycode/internal/runtime/config"
)

// StackManager orchestrates all embedded observability components and the reverse proxy.
type StackManager struct {
	cfg     *config.ObservabilityConfig
	dataDir string // ~/.ycode/observability/

	mu         sync.Mutex
	components []Component
	proxy      *ProxyServer
	started    bool
}

// NewStackManager creates a stack manager.
func NewStackManager(cfg *config.ObservabilityConfig, dataDir string) *StackManager {
	return &StackManager{
		cfg:     cfg,
		dataDir: dataDir,
	}
}

// AddComponent registers a component to be managed by the stack.
// Components are started in the order they are added (dependency ordering).
func (s *StackManager) AddComponent(c Component) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.components = append(s.components, c)
}

// Start launches all components (each in its own goroutine) and the reverse proxy.
func (s *StackManager) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return fmt.Errorf("stack already started")
	}

	slog.Info("observability: starting stack")

	// Start components in dependency order. Each Start() is non-blocking
	// (launches work in a goroutine internally).
	for _, c := range s.components {
		if err := c.Start(ctx); err != nil {
			slog.Warn("observability: component start failed", "component", c.Name(), "error", err)
			// Continue starting other components — non-fatal.
		}
	}

	// Start reverse proxy.
	proxyPort := s.cfg.ProxyPort
	if proxyPort == 0 {
		proxyPort = 58080
	}
	bindAddr := s.cfg.ProxyBindAddr
	if bindAddr == "" {
		bindAddr = "127.0.0.1"
	}
	s.proxy = NewProxyServer(bindAddr, proxyPort)
	s.registerRoutes()
	if err := s.proxy.Start(ctx); err != nil {
		return fmt.Errorf("start proxy: %w", err)
	}

	s.started = true
	slog.Info("observability: stack started", "proxy", s.proxy.Addr())
	return nil
}

// Stop gracefully shuts down all components and the proxy (reverse order).
func (s *StackManager) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return nil
	}

	slog.Info("observability: stopping stack")

	if s.proxy != nil {
		_ = s.proxy.Stop(ctx)
	}

	// Stop in reverse order.
	for i := len(s.components) - 1; i >= 0; i-- {
		c := s.components[i]
		if err := c.Stop(ctx); err != nil {
			slog.Warn("observability: component stop failed", "component", c.Name(), "error", err)
		}
	}

	s.started = false
	return nil
}

// Status returns the health status of each component.
func (s *StackManager) Status() []ComponentStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	var statuses []ComponentStatus
	for _, c := range s.components {
		statuses = append(statuses, ComponentStatus{
			Name:    c.Name(),
			Healthy: c.Healthy(),
		})
	}
	return statuses
}

// Healthy returns true if all components report healthy.
func (s *StackManager) Healthy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, c := range s.components {
		if !c.Healthy() {
			return false
		}
	}
	return s.started
}

// ProxyAddr returns the proxy listen address (e.g. "127.0.0.1:58080").
func (s *StackManager) ProxyAddr() string {
	if s.proxy != nil {
		return s.proxy.Addr()
	}
	return ""
}

// registerRoutes mounts each component's HTTP handler on the proxy mux.
func (s *StackManager) registerRoutes() {
	// Predefined path mappings for well-known components.
	pathMap := map[string]string{
		"otel-collector": "/collector/",
		"prometheus":     "/prometheus/",
		"alertmanager":   "/alerts/",
		"perses":         "/dashboard/",
		"victoria-logs":  "/logs/",
		"jaeger":         "/traces/",
	}

	for _, c := range s.components {
		path, ok := pathMap[c.Name()]
		if !ok {
			path = "/" + c.Name() + "/"
		}

		// Components with their own HTTP handler get mounted in-process.
		if handler := c.HTTPHandler(); handler != nil {
			s.proxy.AddHandler(path, handler)
			continue
		}

		// Components running as external processes with a port get reverse-proxied.
		type portProvider interface{ Port() int }
		if pp, ok := c.(portProvider); ok && pp.Port() > 0 {
			backend, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", pp.Port()))
			s.proxy.AddRoute(path, backend)
		}
		// Jaeger uses QueryPort for UI.
		type queryPortProvider interface{ QueryPort() int }
		if qp, ok := c.(queryPortProvider); ok && qp.QueryPort() > 0 {
			backend, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", qp.QueryPort()))
			s.proxy.AddRoute(path, backend)
		}
	}
}
