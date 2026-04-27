package swarm

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/qiangli/ycode/internal/runtime/agentdef"
	"github.com/qiangli/ycode/internal/tools"
)

// Orchestrator manages multi-agent workflows with handoff support and flow execution.
type Orchestrator struct {
	agentDefs  *agentdef.Registry
	spawner    func(ctx context.Context, manifest *tools.AgentManifest) (string, error)
	contextVar *ContextVars
	logger     *slog.Logger
	router     *Router

	// handoffHistory tracks the agent chain to detect cycles.
	handoffHistory []string
	maxHandoffs    int
}

// OrchestratorConfig holds configuration for the orchestrator.
type OrchestratorConfig struct {
	AgentDefs   *agentdef.Registry
	Spawner     func(ctx context.Context, manifest *tools.AgentManifest) (string, error)
	Logger      *slog.Logger
	MaxHandoffs int // maximum handoff chain length (default 10)
}

// NewOrchestrator creates a new swarm orchestrator.
func NewOrchestrator(cfg *OrchestratorConfig) *Orchestrator {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	maxHandoffs := cfg.MaxHandoffs
	if maxHandoffs <= 0 {
		maxHandoffs = 10
	}
	return &Orchestrator{
		agentDefs:   cfg.AgentDefs,
		spawner:     cfg.Spawner,
		contextVar:  NewContextVars(),
		logger:      logger,
		maxHandoffs: maxHandoffs,
	}
}

// Run executes an agent and handles any handoff chain.
// If the agent's result contains a handoff signal, the orchestrator spawns the target
// agent with the updated context variables and continues until no more handoffs occur.
func (o *Orchestrator) Run(ctx context.Context, agentName, prompt string) (string, error) {
	currentAgent := agentName
	currentPrompt := prompt

	for i := 0; i < o.maxHandoffs; i++ {
		// Cycle detection.
		if o.hasCycle(currentAgent) {
			return "", fmt.Errorf("handoff cycle detected: %v -> %s", o.handoffHistory, currentAgent)
		}
		o.handoffHistory = append(o.handoffHistory, currentAgent)

		o.logger.Info("orchestrator: running agent",
			"agent", currentAgent,
			"handoff_depth", i,
		)

		// Resolve agent definition.
		def, ok := o.agentDefs.Lookup(currentAgent)
		if !ok {
			return "", fmt.Errorf("agent %q not found in registry", currentAgent)
		}

		// Inject context variables into the prompt.
		contextSection := o.contextVar.FormatForPrompt()
		fullPrompt := currentPrompt
		if contextSection != "" {
			fullPrompt = contextSection + "\n\n" + currentPrompt
		}

		// Spawn the agent.
		manifest := &tools.AgentManifest{
			Type:        tools.AgentType(currentAgent),
			Description: def.Description,
			Prompt:      fullPrompt,
			CustomDef:   def,
		}

		result, err := o.spawner(ctx, manifest)
		if err != nil {
			return "", fmt.Errorf("agent %q failed: %w", currentAgent, err)
		}

		// Check for handoff in the result.
		hr, isHandoff := DetectHandoff(result)
		if !isHandoff {
			return result, nil
		}

		// Process handoff.
		o.logger.Info("orchestrator: handoff detected",
			"from", currentAgent,
			"to", hr.TargetAgent,
		)

		// Merge context variables.
		if len(hr.ContextVars) > 0 {
			o.contextVar.Merge(hr.ContextVars)
		}

		// Prepare for next agent.
		currentAgent = hr.TargetAgent
		if hr.Message != "" {
			currentPrompt = hr.Message
		} else {
			currentPrompt = fmt.Sprintf("Continuing from agent %q. Previous result:\n%s", o.handoffHistory[len(o.handoffHistory)-1], result)
		}
	}

	return "", fmt.Errorf("maximum handoff chain length (%d) exceeded", o.maxHandoffs)
}

// hasCycle checks if the agent has already appeared in the handoff chain.
func (o *Orchestrator) hasCycle(agent string) bool {
	for _, a := range o.handoffHistory {
		if a == agent {
			return true
		}
	}
	return false
}

// RunFlow executes a flow-based workflow using the orchestrator's agent definitions.
func (o *Orchestrator) RunFlow(ctx context.Context, def *agentdef.AgentDefinition, input string) (string, error) {
	if len(def.Entrypoint) == 0 {
		// No entrypoint = direct agent execution.
		return o.Run(ctx, def.Name, input)
	}

	// Build actions from entrypoint names.
	actions := make([]agentdef.Action, len(def.Entrypoint))
	for i, name := range def.Entrypoint {
		agentName := name // capture
		actions[i] = func(ctx context.Context, actionInput string) (string, error) {
			return o.Run(ctx, agentName, actionInput)
		}
	}

	flow := def.Flow
	if flow == "" {
		flow = agentdef.FlowSequence
	}

	executor := agentdef.NewFlowExecutor(flow, actions)
	if def.MaxIter > 0 {
		executor.SetMaxIterations(def.MaxIter)
	}

	return executor.Run(ctx, input)
}

// SetContextVar sets a context variable for the orchestrator.
func (o *Orchestrator) SetContextVar(key, value string) {
	o.contextVar.Set(key, value)
}

// GetContextVars returns a snapshot of the current context variables.
func (o *Orchestrator) GetContextVars() map[string]string {
	return o.contextVar.Snapshot()
}

// SetRouter sets the AI router for fallback agent selection.
func (o *Orchestrator) SetRouter(r *Router) {
	o.router = r
}

// RunHierarchical decomposes a task and delegates to specialist agents.
func (o *Orchestrator) RunHierarchical(ctx context.Context, task string) (string, error) {
	var agents []AgentSpec
	for _, name := range o.agentDefs.Names() {
		def, ok := o.agentDefs.Lookup(name)
		if !ok {
			continue
		}
		agents = append(agents, AgentSpec{
			Name:        name,
			Role:        def.Mode,
			Description: def.Description,
		})
	}
	mgr := NewHierarchicalManager(ManagerConfig{Agents: agents})
	// Wire the delegate function to use the existing spawner.
	mgr.DelegateFunc = func(ctx context.Context, agentName, task string) (string, error) {
		return o.spawnAgent(ctx, agentName, task)
	}
	return mgr.Run(ctx, task)
}

// spawnAgent spawns an agent by name with the given task prompt.
func (o *Orchestrator) spawnAgent(ctx context.Context, agentName, task string) (string, error) {
	def, ok := o.agentDefs.Lookup(agentName)
	if !ok {
		return "", fmt.Errorf("agent %q not found in registry", agentName)
	}
	manifest := &tools.AgentManifest{
		Type:        tools.AgentType(agentName),
		Description: def.Description,
		Prompt:      task,
		CustomDef:   def,
	}
	return o.spawner(ctx, manifest)
}
