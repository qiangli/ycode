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
