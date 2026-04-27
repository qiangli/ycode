package swarm

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// AgentSpec describes an available specialist agent.
type AgentSpec struct {
	Name        string
	Role        string
	Description string
}

// ManagerConfig configures the hierarchical manager.
type ManagerConfig struct {
	Agents         []AgentSpec
	MaxDelegations int // max tasks to delegate (default 5)
}

// HierarchicalManager decomposes tasks and delegates to specialist agents.
type HierarchicalManager struct {
	config ManagerConfig
	// DecomposeFunc asks an LLM to break a task into subtasks for agents.
	DecomposeFunc func(ctx context.Context, prompt string) (string, error)
	// DelegateFunc spawns an agent with a task and returns its result.
	DelegateFunc func(ctx context.Context, agentName, task string) (string, error)
}

// NewHierarchicalManager creates a manager.
func NewHierarchicalManager(cfg ManagerConfig) *HierarchicalManager {
	if cfg.MaxDelegations <= 0 {
		cfg.MaxDelegations = 5
	}
	return &HierarchicalManager{config: cfg}
}

// Run decomposes the task, delegates to agents, and synthesizes results.
func (m *HierarchicalManager) Run(ctx context.Context, task string) (string, error) {
	tracer := otel.Tracer("ycode.swarm")
	ctx, span := tracer.Start(ctx, "ycode.swarm.manager.run",
		trace.WithAttributes(
			attribute.Int("manager.agent_count", len(m.config.Agents)),
			attribute.String("manager.task_preview", truncateForSpan(task, 200)),
		))
	defer span.End()

	if m.DecomposeFunc == nil || m.DelegateFunc == nil {
		err := fmt.Errorf("DecomposeFunc and DelegateFunc must be set")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}

	// Step 1: Decompose the task.
	decomposePrompt := FormatDecompositionPrompt(task, m.config.Agents)
	plan, err := m.DecomposeFunc(ctx, decomposePrompt)
	if err != nil {
		err = fmt.Errorf("decompose: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}

	// Step 2: Parse delegation plan and execute.
	// For now, the plan is free-form text. A production version would parse structured JSON.
	// We delegate the entire plan to the first available agent as a simplified implementation.
	var results []string
	for _, agent := range m.config.Agents {
		slog.Info("swarm.manager.delegate",
			"agent", agent.Name,
			"role", agent.Role,
		)
		result, err := m.DelegateFunc(ctx, agent.Name, fmt.Sprintf("Your role: %s\n\nPlan:\n%s\n\nOriginal task: %s", agent.Role, plan, task))
		if err != nil {
			results = append(results, fmt.Sprintf("[%s] Error: %v", agent.Name, err))
		} else {
			results = append(results, fmt.Sprintf("[%s] %s", agent.Name, result))
		}
	}

	span.SetAttributes(
		attribute.Int("manager.results_count", len(results)),
	)

	// Step 3: Synthesize.
	return fmt.Sprintf("## Manager Synthesis\n\n### Task\n%s\n\n### Agent Results\n%s", task, strings.Join(results, "\n\n")), nil
}

// FormatDecompositionPrompt creates the prompt for task decomposition.
func FormatDecompositionPrompt(task string, agents []AgentSpec) string {
	var b strings.Builder
	b.WriteString("You are a manager agent. Decompose this task and create a plan.\n\n")
	fmt.Fprintf(&b, "Task: %s\n\n", task)
	b.WriteString("Available specialist agents:\n")
	for _, a := range agents {
		fmt.Fprintf(&b, "- %s (%s): %s\n", a.Name, a.Role, a.Description)
	}
	b.WriteString("\nCreate a brief plan assigning subtasks to agents.")
	return b.String()
}
