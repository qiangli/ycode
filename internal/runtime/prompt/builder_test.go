package prompt

import (
	"strings"
	"testing"
)

func TestSystemPromptBuilder_BasicAssembly(t *testing.T) {
	builder := NewBuilder()
	builder.AddStaticSection("intro", "You are a helpful assistant.")
	builder.AddStaticSection("system", "Use tools wisely.")

	result := builder.Build()
	if result == "" {
		t.Fatal("build should produce output")
	}
	if !strings.Contains(result, "You are a helpful assistant.") {
		t.Error("should contain intro section")
	}
	if !strings.Contains(result, "Use tools wisely.") {
		t.Error("should contain system section")
	}
}

func TestSystemPromptBuilder_StaticBeforeDynamic(t *testing.T) {
	builder := NewBuilder()
	builder.AddStaticSection("static", "Static content.")
	builder.AddDynamicSection("dynamic", "Dynamic content.")

	result := builder.Build()

	// Static should come before boundary.
	boundaryIdx := strings.Index(result, DynamicBoundary)
	staticIdx := strings.Index(result, "Static content.")
	dynamicIdx := strings.Index(result, "Dynamic content.")

	if boundaryIdx < 0 {
		t.Fatal("should contain dynamic boundary marker")
	}
	if staticIdx > boundaryIdx {
		t.Error("static content should come before boundary")
	}
	if dynamicIdx < boundaryIdx {
		t.Error("dynamic content should come after boundary")
	}
}

func TestSystemPromptBuilder_Empty(t *testing.T) {
	builder := NewBuilder()
	result := builder.Build()
	// Empty builder still has the boundary marker.
	if !strings.Contains(result, DynamicBoundary) {
		t.Error("should contain boundary even when empty")
	}
}

func TestSystemPromptBuilder_EmptyContentSkipped(t *testing.T) {
	builder := NewBuilder()
	builder.AddStaticSection("empty", "")
	builder.AddStaticSection("notempty", "content")

	result := builder.Build()
	if strings.Count(result, "content") != 1 {
		t.Error("should only have one content section")
	}
}

func TestBuildDefault_IncludesDiagnosticsWhenPresent(t *testing.T) {
	ctx := &ProjectContext{
		WorkDir:  "/tmp/test",
		Platform: "linux",
		Diagnostics: &DiagnosticsInfo{
			DegradedTools: []DegradedTool{
				{Name: "bash", SuccessRate: 0.3, TotalCalls: 10, FailureCount: 7},
			},
		},
	}

	result := BuildDefault(ctx, "build", true, nil)

	if !strings.Contains(result, "Runtime diagnostics") {
		t.Error("should include diagnostics section when degraded tools present")
	}
	if !strings.Contains(result, "bash") {
		t.Error("should include degraded tool name")
	}
}

func TestBuildDefault_OmitsDiagnosticsWhenNil(t *testing.T) {
	ctx := &ProjectContext{
		WorkDir:  "/tmp/test",
		Platform: "linux",
	}

	result := BuildDefault(ctx, "build", true, nil)

	if strings.Contains(result, "Runtime diagnostics") {
		t.Error("should NOT include diagnostics section when nil")
	}
}

func TestBuildDefault_OmitsDiagnosticsWhenEmpty(t *testing.T) {
	ctx := &ProjectContext{
		WorkDir:     "/tmp/test",
		Platform:    "linux",
		Diagnostics: &DiagnosticsInfo{},
	}

	result := BuildDefault(ctx, "build", true, nil)

	if strings.Contains(result, "Runtime diagnostics") {
		t.Error("should NOT include diagnostics section when empty (no actionable items)")
	}
}

func TestBuildDefault_DiagnosticsAfterMemoryBeforePlan(t *testing.T) {
	ctx := &ProjectContext{
		WorkDir:  "/tmp/test",
		Platform: "linux",
		Diagnostics: &DiagnosticsInfo{
			ContextHealthPct:   80,
			ContextHealthLevel: "critical",
		},
	}

	result := BuildDefault(ctx, "plan", true, nil)

	diagIdx := strings.Index(result, "Runtime diagnostics")
	planIdx := strings.Index(result, "Plan Mode")

	if diagIdx < 0 {
		t.Fatal("should contain diagnostics section")
	}
	if planIdx < 0 {
		t.Fatal("should contain plan mode section")
	}
	if diagIdx > planIdx {
		t.Error("diagnostics should appear before plan mode section")
	}
}

func TestBuildDefault_ExploreModeSuppressesDiagnostics(t *testing.T) {
	ctx := &ProjectContext{
		WorkDir:  "/tmp/test",
		Platform: "linux",
		Diagnostics: &DiagnosticsInfo{
			DegradedTools: []DegradedTool{
				{Name: "bash", SuccessRate: 0.3, TotalCalls: 10, FailureCount: 7},
			},
		},
	}

	result := BuildDefault(ctx, "explore", true, nil)

	if strings.Contains(result, "Runtime diagnostics") {
		t.Error("explore mode should NOT include diagnostics (lean prompt)")
	}
}

// fakeBoard implements TodoBoardRenderer for prompt-builder tests without
// pulling in the todo package — keeps this test file dependency-free.
type fakeBoard struct{ md string }

func (f *fakeBoard) RenderMarkdown() string { return f.md }

func TestBuildDefault_TodosRenderedInDynamicRegion(t *testing.T) {
	ctx := &ProjectContext{
		WorkDir:   "/tmp/test",
		Platform:  "linux",
		TodoBoard: &fakeBoard{md: "## Task Board\n\n| id | [~] | 0 | - | Fix login |\n"},
	}

	result := BuildDefault(ctx, "build", true, nil)

	if !strings.Contains(result, "## Task Board") {
		t.Fatal("non-empty todo board should render into prompt")
	}

	// The whole point of the deepagents pattern: board must land AFTER the
	// dynamic boundary so it doesn't bust the prompt cache on every turn.
	boundaryIdx := strings.Index(result, DynamicBoundary)
	todosIdx := strings.Index(result, "## Task Board")
	if boundaryIdx < 0 {
		t.Fatal("missing dynamic boundary marker")
	}
	if todosIdx < boundaryIdx {
		t.Errorf("todos rendered at %d, before boundary at %d — must be in dynamic region", todosIdx, boundaryIdx)
	}
}

func TestBuildDefault_EmptyTodoBoardRendersNothing(t *testing.T) {
	ctx := &ProjectContext{
		WorkDir:   "/tmp/test",
		Platform:  "linux",
		TodoBoard: &fakeBoard{md: ""}, // todo.Board returns "" when empty
	}
	result := BuildDefault(ctx, "build", true, nil)
	if strings.Contains(result, "Task Board") {
		t.Error("empty board should not leak any 'Task Board' header into prompt")
	}
}

func TestBuildDefault_NilTodoBoardIsSafe(t *testing.T) {
	ctx := &ProjectContext{
		WorkDir:  "/tmp/test",
		Platform: "linux",
		// TodoBoard intentionally nil — stable-tier build, no wiring.
	}
	result := BuildDefault(ctx, "build", true, nil)
	if strings.Contains(result, "Task Board") {
		t.Error("nil board should not produce a Task Board section")
	}
}

func TestBuildDefault_ExploreModeOmitsTodos(t *testing.T) {
	// Explore subagents get a lean prompt — todos shouldn't bloat it.
	ctx := &ProjectContext{
		WorkDir:   "/tmp/test",
		Platform:  "linux",
		TodoBoard: &fakeBoard{md: "## Task Board\n\n| id | [~] | 0 | - | x |\n"},
	}
	result := BuildDefault(ctx, "explore", true, nil)
	if strings.Contains(result, "Task Board") {
		t.Error("explore mode should NOT render todo board")
	}
}
