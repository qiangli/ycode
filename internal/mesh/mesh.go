package mesh

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/qiangli/ycode/internal/bus"
)

// Severity for diagnostic reports.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarn     Severity = "warn"
	SeverityCritical Severity = "critical"
)

// DiagCategory classifies what was detected.
type DiagCategory string

const (
	DiagToolDegradation DiagCategory = "tool_degradation"
	DiagLatencySpike    DiagCategory = "latency_spike"
	DiagErrorRate       DiagCategory = "error_rate"
	DiagTokenWaste      DiagCategory = "token_waste"
	DiagContextOverflow DiagCategory = "context_overflow"
	DiagMemoryMiss      DiagCategory = "memory_miss"
)

// Evidence is a piece of supporting data in a diagnostic report.
type Evidence struct {
	Source string `json:"source"` // "metrics", "traces", "logs", "bus_event"
	Data   string `json:"data"`
}

// DiagnosticReport is produced by the Diagnoser agent.
type DiagnosticReport struct {
	ID        string       `json:"id"`
	Severity  Severity     `json:"severity"`
	Category  DiagCategory `json:"category"`
	Summary   string       `json:"summary"`
	Evidence  []Evidence   `json:"evidence"`
	Timestamp time.Time    `json:"timestamp"`
	SessionID string       `json:"session_id,omitempty"`
	ToolName  string       `json:"tool_name,omitempty"`
}

// MeshAgent is the interface all background agents implement.
type MeshAgent interface {
	Name() string
	Start(ctx context.Context) error
	Stop()
	Healthy() bool
}

// MeshConfig configures the agent mesh.
type MeshConfig struct {
	Enabled         bool          `json:"mesh_enabled"`
	Mode            string        `json:"mesh_mode"` // "cli" or "server"
	DiagInterval    time.Duration `json:"diag_interval"`
	MaxFixAttempts  int           `json:"max_fix_attempts"`
	ResearchLimit   int           `json:"research_limit_per_10m"`
	TrainingEnabled bool          `json:"training_enabled"`
	TrainingCron    string        `json:"training_cron"`
}

// DefaultMeshConfig returns sensible defaults.
func DefaultMeshConfig() *MeshConfig {
	return &MeshConfig{
		Enabled:        false,
		Mode:           "cli",
		DiagInterval:   2 * time.Minute,
		MaxFixAttempts: 5,
		ResearchLimit:  3,
	}
}

// Mesh orchestrates all background agents.
type Mesh struct {
	config  *MeshConfig
	b       bus.Bus
	agents  []MeshAgent
	logger  *slog.Logger
	mu      sync.Mutex
	running bool
}

// New creates a new mesh.
func New(cfg *MeshConfig, b bus.Bus) *Mesh {
	if cfg == nil {
		cfg = DefaultMeshConfig()
	}
	return &Mesh{
		config: cfg,
		b:      b,
		logger: slog.Default(),
	}
}

// Register adds an agent to the mesh.
func (m *Mesh) Register(agent MeshAgent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.agents = append(m.agents, agent)
}

// Start launches all registered agents.
func (m *Mesh) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		return nil
	}
	m.logger.Info("mesh: starting", "agent_count", len(m.agents), "mode", m.config.Mode)
	for _, agent := range m.agents {
		if err := agent.Start(ctx); err != nil {
			m.logger.Error("mesh: agent failed to start", "agent", agent.Name(), "error", err)
			continue
		}
		m.logger.Info("mesh: agent started", "agent", agent.Name())
	}
	m.running = true
	return nil
}

// Stop shuts down all agents.
func (m *Mesh) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, agent := range m.agents {
		agent.Stop()
		m.logger.Info("mesh: agent stopped", "agent", agent.Name())
	}
	m.running = false
}

// Status returns the health of each agent.
func (m *Mesh) Status() map[string]bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	status := make(map[string]bool, len(m.agents))
	for _, agent := range m.agents {
		status[agent.Name()] = agent.Healthy()
	}
	return status
}
