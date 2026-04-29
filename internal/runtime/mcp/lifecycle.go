package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// ServerStatus represents the health of an MCP server connection.
type ServerStatus string

const (
	StatusHealthy  ServerStatus = "healthy"
	StatusDegraded ServerStatus = "degraded"
	StatusFailed   ServerStatus = "failed"
	StatusUnknown  ServerStatus = "unknown"
)

// ServerHealth tracks the lifecycle state of an MCP server.
type ServerHealth struct {
	Name        string       `json:"name"`
	Status      ServerStatus `json:"status"`
	LastChecked time.Time    `json:"last_checked"`
	LastError   string       `json:"last_error,omitempty"`
	ToolCount   int          `json:"tool_count"`
	Latency     string       `json:"latency,omitempty"`
}

// LifecycleManager validates and monitors MCP server connections.
type LifecycleManager struct {
	registry *Registry
	health   map[string]*ServerHealth
	mu       sync.RWMutex
	logger   *slog.Logger
}

// NewLifecycleManager creates a lifecycle manager.
func NewLifecycleManager(registry *Registry) *LifecycleManager {
	return &LifecycleManager{
		registry: registry,
		health:   make(map[string]*ServerHealth),
		logger:   slog.Default(),
	}
}

// Validate checks all configured servers and reports their status.
func (lm *LifecycleManager) Validate(ctx context.Context) []ServerHealth {
	lm.registry.mu.RLock()
	names := make([]string, 0, len(lm.registry.clients))
	for name := range lm.registry.clients {
		names = append(names, name)
	}
	lm.registry.mu.RUnlock()

	var results []ServerHealth
	for _, name := range names {
		health := lm.checkServer(ctx, name)
		results = append(results, health)
	}

	return results
}

// checkServer validates a single server connection.
func (lm *LifecycleManager) checkServer(ctx context.Context, name string) ServerHealth {
	health := ServerHealth{
		Name:        name,
		Status:      StatusUnknown,
		LastChecked: time.Now(),
	}

	client, ok := lm.registry.Get(name)
	if !ok {
		health.Status = StatusFailed
		health.LastError = "server not registered"
		lm.updateHealth(name, &health)
		return health
	}

	start := time.Now()
	if err := client.Connect(ctx); err != nil {
		health.Status = StatusDegraded
		health.LastError = err.Error()
	} else {
		health.Status = StatusHealthy
		health.ToolCount = len(client.ListTools())
	}
	health.Latency = time.Since(start).String()

	lm.updateHealth(name, &health)
	return health
}

func (lm *LifecycleManager) updateHealth(name string, health *ServerHealth) {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	lm.health[name] = health
}

// GetHealth returns the health status of a server.
func (lm *LifecycleManager) GetHealth(name string) (*ServerHealth, bool) {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	h, ok := lm.health[name]
	return h, ok
}

// StartAll connects to all registered MCP servers in parallel.
func (lm *LifecycleManager) StartAll(ctx context.Context) []ServerHealth {
	lm.registry.mu.RLock()
	names := make([]string, 0, len(lm.registry.clients))
	for name := range lm.registry.clients {
		names = append(names, name)
	}
	lm.registry.mu.RUnlock()

	type result struct {
		health ServerHealth
	}
	results := make(chan result, len(names))

	var wg sync.WaitGroup
	for _, name := range names {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			h := lm.checkServer(ctx, n)
			results <- result{health: h}
		}(name)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var healths []ServerHealth
	for r := range results {
		healths = append(healths, r.health)
	}
	return healths
}

// Report returns a formatted health report.
func (lm *LifecycleManager) Report() string {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	if len(lm.health) == 0 {
		return "No MCP servers configured."
	}

	var b strings.Builder
	fmt.Fprintf(&b, "MCP Server Status (%d servers):\n\n", len(lm.health))
	for _, h := range lm.health {
		status := string(h.Status)
		errMsg := ""
		if h.LastError != "" {
			errMsg = fmt.Sprintf(" (%s)", h.LastError)
		}
		fmt.Fprintf(&b, "  %s: %s, %d tools, latency: %s%s\n",
			h.Name, status, h.ToolCount, h.Latency, errMsg)
	}
	return b.String()
}
