// Package agentpool tracks active subagents and provides per-agent metrics
// for progress reporting. It enables tree-style displays of concurrent
// agent activity (tool counts, token usage, duration, status).
package agentpool

import (
	"sync"
	"sync/atomic"
	"time"
)

// AgentStatus represents the lifecycle of a subagent.
type AgentStatus int

const (
	StatusSpawning AgentStatus = iota
	StatusRunning
	StatusCompleted
	StatusFailed
)

// String returns a human-readable status label.
func (s AgentStatus) String() string {
	switch s {
	case StatusSpawning:
		return "spawning"
	case StatusRunning:
		return "running"
	case StatusCompleted:
		return "completed"
	case StatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// AgentInfo holds metrics and state for one active subagent.
type AgentInfo struct {
	ID          string      `json:"id"`
	Type        string      `json:"type"`        // agent type (Explore, Plan, etc.)
	Description string      `json:"description"` // short task description
	Status      AgentStatus `json:"status"`
	ToolUses    int32       `json:"tool_uses"`    // total tool calls executed
	CurrentTool string      `json:"current_tool"` // tool currently being executed (empty if idle)
	StartedAt   time.Time   `json:"started_at"`
	CompletedAt time.Time   `json:"completed_at,omitempty"`

	// Token usage (updated per turn).
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

// Duration returns how long the agent has been running.
func (a *AgentInfo) Duration() time.Duration {
	end := a.CompletedAt
	if end.IsZero() {
		end = time.Now()
	}
	return end.Sub(a.StartedAt)
}

// Pool tracks all active subagents for progress reporting.
type Pool struct {
	mu     sync.RWMutex
	agents map[string]*AgentInfo
}

// New creates a new agent pool.
func New() *Pool {
	return &Pool{
		agents: make(map[string]*AgentInfo),
	}
}

// Register adds a new agent to the pool.
func (p *Pool) Register(id, agentType, description string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.agents[id] = &AgentInfo{
		ID:          id,
		Type:        agentType,
		Description: description,
		Status:      StatusSpawning,
		StartedAt:   time.Now(),
	}
}

// SetRunning marks an agent as actively running.
func (p *Pool) SetRunning(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if a, ok := p.agents[id]; ok {
		a.Status = StatusRunning
	}
}

// RecordToolUse increments the tool use counter and records the current tool.
func (p *Pool) RecordToolUse(id, toolName string) {
	p.mu.RLock()
	a, ok := p.agents[id]
	p.mu.RUnlock()
	if ok {
		atomic.AddInt32(&a.ToolUses, 1)
		p.mu.Lock()
		a.CurrentTool = toolName
		p.mu.Unlock()
	}
}

// ClearCurrentTool clears the current tool being executed.
func (p *Pool) ClearCurrentTool(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if a, ok := p.agents[id]; ok {
		a.CurrentTool = ""
	}
}

// RecordUsage adds token usage for an agent.
func (p *Pool) RecordUsage(id string, inputTokens, outputTokens int64) {
	p.mu.RLock()
	a, ok := p.agents[id]
	p.mu.RUnlock()
	if ok {
		atomic.AddInt64(&a.InputTokens, inputTokens)
		atomic.AddInt64(&a.OutputTokens, outputTokens)
	}
}

// Complete marks an agent as completed or failed and removes from active tracking.
func (p *Pool) Complete(id string, failed bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if a, ok := p.agents[id]; ok {
		if failed {
			a.Status = StatusFailed
		} else {
			a.Status = StatusCompleted
		}
		a.CompletedAt = time.Now()
		a.CurrentTool = ""
	}
}

// Remove removes an agent from the pool entirely.
func (p *Pool) Remove(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.agents, id)
}

// Get returns a snapshot of a single agent's info.
func (p *Pool) Get(id string) (AgentInfo, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	a, ok := p.agents[id]
	if !ok {
		return AgentInfo{}, false
	}
	return *a, true
}

// Active returns snapshots of all currently active (non-completed) agents.
func (p *Pool) Active() []AgentInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var result []AgentInfo
	for _, a := range p.agents {
		if a.Status == StatusSpawning || a.Status == StatusRunning {
			result = append(result, *a)
		}
	}
	return result
}

// All returns snapshots of all agents (active and completed).
func (p *Pool) All() []AgentInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]AgentInfo, 0, len(p.agents))
	for _, a := range p.agents {
		result = append(result, *a)
	}
	return result
}

// ActiveCount returns the number of currently active agents.
func (p *Pool) ActiveCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	count := 0
	for _, a := range p.agents {
		if a.Status == StatusSpawning || a.Status == StatusRunning {
			count++
		}
	}
	return count
}
