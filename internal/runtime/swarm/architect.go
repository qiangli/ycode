package swarm

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/qiangli/ycode/internal/runtime/agentdef"
	"github.com/qiangli/ycode/internal/tools"
)

// ArchitectEditorConfig configures the two-model delegation pattern.
type ArchitectEditorConfig struct {
	ArchitectModel string   // cheap/fast model for planning
	EditorModel    string   // capable model for implementation
	ArchitectTools []string // read-only tools (default: plan mode tools)
	EditorTools    []string // full tools (default: build mode tools)
}

// RunArchitectEditor executes the architect-editor workflow:
// 1. Architect agent creates a structured plan using the cheap model.
// 2. Plan is validated (non-empty, contains actionable steps).
// 3. Editor agent implements the plan using the capable model.
func RunArchitectEditor(
	ctx context.Context,
	cfg *ArchitectEditorConfig,
	spawner func(ctx context.Context, manifest *tools.AgentManifest) (string, error),
	task string,
	logger *slog.Logger,
) (string, error) {
	if logger == nil {
		logger = slog.Default()
	}

	// Phase 1: Architect plans.
	logger.Info("architect-editor: planning phase", "model", cfg.ArchitectModel)

	architectDef := &agentdef.AgentDefinition{
		Name:        "architect",
		Instruction: architectPrompt(),
		Mode:        "plan",
		Model:       cfg.ArchitectModel,
		Tools:       cfg.ArchitectTools,
	}

	planResult, err := spawner(ctx, &tools.AgentManifest{
		Type:        "architect",
		Description: "Architect: create implementation plan",
		Prompt:      task,
		Model:       cfg.ArchitectModel,
		CustomDef:   architectDef,
	})
	if err != nil {
		return "", fmt.Errorf("architect phase failed: %w", err)
	}

	// Validate plan.
	if len(planResult) < 50 {
		return "", fmt.Errorf("architect produced insufficient plan (%d chars)", len(planResult))
	}

	logger.Info("architect-editor: plan created", "plan_length", len(planResult))

	// Phase 2: Editor implements.
	logger.Info("architect-editor: implementation phase", "model", cfg.EditorModel)

	editorDef := &agentdef.AgentDefinition{
		Name:        "editor",
		Instruction: editorPrompt(),
		Mode:        "build",
		Model:       cfg.EditorModel,
		Tools:       cfg.EditorTools,
	}

	editorPromptText := fmt.Sprintf("Implement the following plan:\n\n%s\n\nOriginal task: %s", planResult, task)

	result, err := spawner(ctx, &tools.AgentManifest{
		Type:        "editor",
		Description: "Editor: implement the architect's plan",
		Prompt:      editorPromptText,
		Model:       cfg.EditorModel,
		CustomDef:   editorDef,
	})
	if err != nil {
		return "", fmt.Errorf("editor phase failed: %w", err)
	}

	return result, nil
}

func architectPrompt() string {
	return `You are a software architect. Your role is to create detailed implementation plans.

Given a task, produce a structured plan that includes:
1. Files to create or modify (with paths)
2. Key changes in each file
3. Dependencies between changes
4. Testing strategy

Do NOT implement the changes. Only plan them. Be specific and actionable.`
}

func editorPrompt() string {
	return `You are a software editor/implementer. You receive a plan from an architect and implement it precisely.

Follow the plan step by step. Make the exact changes specified. Do not deviate from the plan unless you find a clear error in it (in which case, note the deviation).`
}
