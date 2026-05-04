//go:build integration

package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/service"
)

// --- Command streaming (server → client via bus events) ---

// TestTUI_CommandProgress_GraphBuildingShown verifies that progress messages
// from graph building and repo map generation appear in the TUI output.
func TestTUI_CommandProgress_GraphBuildingShown(t *testing.T) {
	m := newTestTUIModel(t)

	progressMsgs := []string{
		"⧗ Analyzing project structure...",
		"⧗ Building code knowledge graph...",
		"✓ Code graph built",
		"⧗ Generating symbol map...",
		"✓ Symbol map generated",
		"⧗ Generating AGENTS.md via LLM...",
	}

	for _, msg := range progressMsgs {
		data, _ := json.Marshal(map[string]string{"message": msg})
		updated, _ := m.Update(busEventMsg{Event: bus.Event{
			Type: bus.EventCommandProgress,
			Data: data,
		}})
		m = updated.(*TUIModel)
	}

	output := modelOutput(m)
	for _, msg := range progressMsgs {
		if !strings.Contains(output, msg) {
			t.Errorf("output missing progress message %q;\ngot: %s", msg, output)
		}
	}
}

// TestTUI_CommandDelta_LLMStreamShown verifies that streaming LLM text deltas
// from command execution appear in the TUI output.
func TestTUI_CommandDelta_LLMStreamShown(t *testing.T) {
	m := newTestTUIModel(t)

	deltas := []string{"# AGENTS", ".md\n\n", "Build commands:\n", "```bash\nmake build\n```"}
	for _, text := range deltas {
		data, _ := json.Marshal(map[string]string{"text": text})
		updated, _ := m.Update(busEventMsg{Event: bus.Event{
			Type: bus.EventCommandDelta,
			Data: data,
		}})
		m = updated.(*TUIModel)
	}

	output := modelOutput(m)
	if !strings.Contains(output, "AGENTS") {
		t.Errorf("expected LLM delta text in output;\ngot: %s", output)
	}
	if !strings.Contains(output, "make build") {
		t.Errorf("expected LLM delta text in output;\ngot: %s", output)
	}
}

// TestTUI_ToolUseStart_ShowsDetail verifies that tool use events display
// tool name and input summary (e.g., file path, command, pattern).
func TestTUI_ToolUseStart_ShowsDetail(t *testing.T) {
	tests := []struct {
		tool  string
		input map[string]any
		want  string
	}{
		{
			tool:  "Read",
			input: map[string]any{"file_path": "/src/main.go"},
			want:  "/src/main.go",
		},
		{
			tool:  "Bash",
			input: map[string]any{"command": "go test ./..."},
			want:  "go test ./...",
		},
		{
			tool:  "Grep",
			input: map[string]any{"pattern": "handleInput", "path": "internal/cli/"},
			want:  `"handleInput" in internal/cli/`,
		},
		{
			tool:  "Agent",
			input: map[string]any{"description": "Explore codebase"},
			want:  "Explore codebase",
		},
	}

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			m := newTestTUIModel(t)
			data, _ := json.Marshal(map[string]any{
				"tool":  tt.tool,
				"input": tt.input,
			})
			updated, _ := m.Update(busEventMsg{Event: bus.Event{
				Type: bus.EventToolUseStart,
				Data: data,
			}})
			m = updated.(*TUIModel)

			output := modelOutput(m)
			if !strings.Contains(output, tt.tool) {
				t.Errorf("expected tool name %q in output;\ngot: %s", tt.tool, output)
			}
			if !strings.Contains(output, tt.want) {
				t.Errorf("expected detail %q in output;\ngot: %s", tt.want, output)
			}
		})
	}
}

// TestTUI_ToolProgress_ShowsIndexTotal verifies that parallel tool progress
// displays [index/total] when multiple tools run.
func TestTUI_ToolProgress_ShowsIndexTotal(t *testing.T) {
	m := newTestTUIModel(t)

	data, _ := json.Marshal(map[string]any{
		"tool":   "Read",
		"status": "running",
		"index":  0,
		"total":  3,
	})
	updated, _ := m.Update(busEventMsg{Event: bus.Event{
		Type: bus.EventToolProgress,
		Data: data,
	}})
	m = updated.(*TUIModel)

	output := modelOutput(m)
	if !strings.Contains(output, "[1/3]") {
		t.Errorf("expected [1/3] in tool progress;\ngot: %s", output)
	}
}

// TestTUI_StatusBar_ShowsModelFromServer verifies that the status bar displays
// the model name synced from the server via serverStatusMsg.
func TestTUI_StatusBar_ShowsModelFromServer(t *testing.T) {
	m := newTestTUIModel(t)

	// Simulate server status sync (fires from Init in thin-client mode).
	updated, _ := m.Update(serverStatusMsg{info: &service.StatusInfo{
		Model:        "claude-sonnet-4-20250514",
		ProviderKind: "anthropic",
	}})
	m = updated.(*TUIModel)

	if m.app.Model() != "claude-sonnet-4-20250514" {
		t.Errorf("app model not synced; got %q", m.app.Model())
	}
	if m.app.ProviderKind() != "anthropic" {
		t.Errorf("app provider not synced; got %q", m.app.ProviderKind())
	}

	view := m.View()
	stripped := stripANSI(view)
	if !strings.Contains(stripped, "claude-sonnet-4-20250514") {
		t.Errorf("expected model name in status bar;\ngot: %s", stripped)
	}
	if !strings.Contains(stripped, "anthropic") {
		t.Errorf("expected provider kind in status bar;\ngot: %s", stripped)
	}
}

// TestTUI_UsageUpdate_StatusBarRefreshes verifies that EventUsageUpdate
// increments the client's usage tracker and triggers a status bar repaint
// with token counts and estimated cost.
func TestTUI_UsageUpdate_StatusBarRefreshes(t *testing.T) {
	m := newTestTUIModel(t)

	// Send a usage update with model info.
	data, _ := json.Marshal(map[string]any{
		"input_tokens":  float64(1500),
		"output_tokens": float64(500),
		"model":         "claude-sonnet-4-20250514",
	})
	updated, cmd := m.Update(busEventMsg{Event: bus.Event{
		Type: bus.EventUsageUpdate,
		Data: data,
	}})
	m = updated.(*TUIModel)

	// Tracker should be updated.
	if m.app.usageTracker.InputTokens != 1500 {
		t.Errorf("input tokens: got %d, want 1500", m.app.usageTracker.InputTokens)
	}
	if m.app.usageTracker.OutputTokens != 500 {
		t.Errorf("output tokens: got %d, want 500", m.app.usageTracker.OutputTokens)
	}
	if !m.app.usageTracker.HasRequests() {
		t.Error("expected HasRequests()=true after usage update")
	}
	if m.app.usageTracker.Model != "claude-sonnet-4-20250514" {
		t.Errorf("tracker model: got %q, want claude-sonnet-4-20250514", m.app.usageTracker.Model)
	}

	// Should return a repaint command so the status bar re-renders.
	if cmd == nil {
		t.Fatal("expected repaint command from usage update")
	}

	// Execute the repaint and verify the view contains token info.
	msg := cmd()
	if _, ok := msg.(repaintMsg); !ok {
		t.Errorf("expected repaintMsg, got %T", msg)
	}

	view := m.View()
	stripped := stripANSI(view)
	if !strings.Contains(stripped, "tokens") {
		t.Errorf("expected token count in status bar after usage update;\ngot: %s", stripped)
	}
	if !strings.Contains(stripped, "$") {
		t.Errorf("expected cost estimate in status bar after usage update;\ngot: %s", stripped)
	}
}

// TestTUI_UsageUpdate_IncrementalUpdates verifies that multiple usage events
// accumulate correctly in the tracker.
func TestTUI_UsageUpdate_IncrementalUpdates(t *testing.T) {
	m := newTestTUIModel(t)

	// First LLM call.
	data1, _ := json.Marshal(map[string]any{
		"input_tokens":  float64(1000),
		"output_tokens": float64(200),
	})
	updated, _ := m.Update(busEventMsg{Event: bus.Event{
		Type: bus.EventUsageUpdate,
		Data: data1,
	}})
	m = updated.(*TUIModel)

	// Second LLM call.
	data2, _ := json.Marshal(map[string]any{
		"input_tokens":  float64(800),
		"output_tokens": float64(300),
	})
	updated, _ = m.Update(busEventMsg{Event: bus.Event{
		Type: bus.EventUsageUpdate,
		Data: data2,
	}})
	m = updated.(*TUIModel)

	if m.app.usageTracker.InputTokens != 1800 {
		t.Errorf("accumulated input tokens: got %d, want 1800", m.app.usageTracker.InputTokens)
	}
	if m.app.usageTracker.OutputTokens != 500 {
		t.Errorf("accumulated output tokens: got %d, want 500", m.app.usageTracker.OutputTokens)
	}
	if m.app.usageTracker.TotalRequests != 2 {
		t.Errorf("total requests: got %d, want 2", m.app.usageTracker.TotalRequests)
	}
}

// TestTUI_CommandComplete_ShowsSummary verifies that EventCommandComplete
// sets working=false, shows the result text, and displays a session summary
// with token usage and cost.
func TestTUI_CommandComplete_ShowsSummary(t *testing.T) {
	m := newTestTUIModel(t)
	m.working = true

	// Pre-populate usage so the summary has something to show.
	m.app.usageTracker.AddWithModel("claude-sonnet-4-20250514", 2000, 800, 0, 0)

	data, _ := json.Marshal(map[string]string{"result": "✓ Updated AGENTS.md"})
	updated, cmd := m.Update(busEventMsg{Event: bus.Event{
		Type: bus.EventCommandComplete,
		Data: data,
	}})
	m = updated.(*TUIModel)

	// Should clear working state.
	if m.working {
		t.Error("expected working=false after command complete")
	}

	// Output should contain the result and session summary.
	output := modelOutput(m)
	if !strings.Contains(output, "Updated AGENTS.md") {
		t.Errorf("expected result text in output;\ngot: %s", output)
	}
	if !strings.Contains(output, "Session:") {
		t.Errorf("expected session summary in output;\ngot: %s", output)
	}
	if !strings.Contains(output, "$") {
		t.Errorf("expected cost in session summary;\ngot: %s", output)
	}

	// Should return a command (repaint + alertDone).
	if cmd == nil {
		t.Error("expected non-nil command from command complete")
	}
}

// TestTUI_CommandError_ShowsError verifies that EventCommandError displays
// the error and clears working state.
func TestTUI_CommandError_ShowsError(t *testing.T) {
	m := newTestTUIModel(t)
	m.working = true

	data, _ := json.Marshal(map[string]string{"error": "provider timeout"})
	updated, _ := m.Update(busEventMsg{Event: bus.Event{
		Type: bus.EventCommandError,
		Data: data,
	}})
	m = updated.(*TUIModel)

	if m.working {
		t.Error("expected working=false after command error")
	}

	output := modelOutput(m)
	if !strings.Contains(output, "provider timeout") {
		t.Errorf("expected error message in output;\ngot: %s", output)
	}
}

// TestTUI_CommandStream_FullFlow simulates the complete lifecycle of a
// server-side /init command: progress → delta → usage → complete → summary.
func TestTUI_CommandStream_FullFlow(t *testing.T) {
	m := newTestTUIModel(t)
	m.working = true

	// Sync model from server.
	updated, _ := m.Update(serverStatusMsg{info: &service.StatusInfo{
		Model:        "claude-sonnet-4-20250514",
		ProviderKind: "anthropic",
	}})
	m = updated.(*TUIModel)

	events := []bus.Event{
		// Phase 1: scaffold progress.
		{Type: bus.EventCommandProgress, Data: mustTestJSON(t, map[string]string{
			"message": "⧗ Analyzing project structure...",
		})},
		{Type: bus.EventCommandProgress, Data: mustTestJSON(t, map[string]string{
			"message": "⧗ Building code knowledge graph...",
		})},
		{Type: bus.EventCommandProgress, Data: mustTestJSON(t, map[string]string{
			"message": "✓ Code graph built (via session manager)",
		})},
		{Type: bus.EventCommandProgress, Data: mustTestJSON(t, map[string]string{
			"message": "⧗ Generating AGENTS.md via LLM...",
		})},
		// Phase 2: LLM streaming.
		{Type: bus.EventCommandDelta, Data: mustTestJSON(t, map[string]string{
			"text": "# AGENTS.md\n\nBuild: `make build`\n",
		})},
		// Phase 3: usage update.
		{Type: bus.EventUsageUpdate, Data: mustTestJSON(t, map[string]any{
			"input_tokens":  float64(3000),
			"output_tokens": float64(1200),
			"model":         "claude-sonnet-4-20250514",
		})},
		// Phase 4: complete.
		{Type: bus.EventCommandComplete, Data: mustTestJSON(t, map[string]string{
			"result": "",
		})},
	}

	for _, ev := range events {
		updated, _ = m.Update(busEventMsg{Event: ev})
		m = updated.(*TUIModel)
	}

	output := modelOutput(m)

	// 1) Graph progress was shown.
	if !strings.Contains(output, "Building code knowledge graph") {
		t.Errorf("missing graph progress;\ngot: %s", output)
	}
	if !strings.Contains(output, "Code graph built") {
		t.Errorf("missing graph completion;\ngot: %s", output)
	}

	// 2) LLM content was streamed.
	if !strings.Contains(output, "AGENTS.md") {
		t.Errorf("missing LLM delta content;\ngot: %s", output)
	}

	// 3) Usage was tracked.
	if m.app.usageTracker.InputTokens != 3000 {
		t.Errorf("input tokens: got %d, want 3000", m.app.usageTracker.InputTokens)
	}

	// 4) Session summary shown at end.
	if !strings.Contains(output, "Session:") {
		t.Errorf("missing session summary;\ngot: %s", output)
	}
	if !strings.Contains(output, "$") {
		t.Errorf("missing cost in summary;\ngot: %s", output)
	}

	// 5) Model shown in status bar.
	view := m.View()
	stripped := stripANSI(view)
	if !strings.Contains(stripped, "claude-sonnet-4-20250514") {
		t.Errorf("model not in status bar;\ngot: %s", stripped)
	}

	// 6) Working state cleared.
	if m.working {
		t.Error("expected working=false after full flow")
	}
}

// mustTestJSON marshals v to json.RawMessage, failing the test on error.
func mustTestJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}
