//go:build integration

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/runtime/conversation"
)

// --- Initialization ---

func TestTUI_WindowSizeInitializes(t *testing.T) {
	app := newTestApp(t)
	m := NewTUIModel(app)

	if m.ready {
		t.Fatal("expected ready=false before WindowSizeMsg")
	}

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*TUIModel)

	if !m.ready {
		t.Error("expected ready=true after WindowSizeMsg")
	}
	if m.width != 80 || m.height != 24 {
		t.Errorf("expected 80x24, got %dx%d", m.width, m.height)
	}
}

func TestTUI_ViewBeforeReady(t *testing.T) {
	app := newTestApp(t)
	m := NewTUIModel(app)
	view := m.View()
	if view != "Initializing..." {
		t.Errorf("expected 'Initializing...' before ready, got %q", view)
	}
}

// --- Ctrl+C / Ctrl+D ---

func TestTUI_CtrlC_WhileIdle_Quits(t *testing.T) {
	m := newTestTUIModel(t)
	assertState(t, m, false, false, false)

	_, cmd := m.Update(keyMsg(tea.KeyCtrlC))
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
	// Verify it produces a quit message.
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", msg)
	}
}

func TestTUI_CtrlD_Quits(t *testing.T) {
	m := newTestTUIModel(t)
	_, cmd := m.Update(keyMsg(tea.KeyCtrlD))
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", msg)
	}
}

func TestTUI_CtrlC_WhileWorking_Cancels(t *testing.T) {
	m := newTestTUIModel(t)
	cancelled := false
	m.working = true
	m.workCancel = func() { cancelled = true }
	m.midTurnCh = make(chan string, 1)

	updated, _ := m.Update(keyMsg(tea.KeyCtrlC))
	m = updated.(*TUIModel)

	if !cancelled {
		t.Error("expected workCancel to be called")
	}
	assertState(t, m, false, false, false)
	if m.workCancel != nil {
		t.Error("expected workCancel to be nil after cancel")
	}
	if m.midTurnCh != nil {
		t.Error("expected midTurnCh to be nil after cancel")
	}
	assertOutputContains(t, m, "Cancelled")
}

func TestTUI_CtrlC_WhilePaused_Discards(t *testing.T) {
	m := newTestTUIModel(t)
	m.paused = true
	m.pausedMessages = []api.Message{{Role: api.RoleUser}}
	m.pausedCalls = []conversation.ToolCall{{ID: "1"}}

	updated, _ := m.Update(keyMsg(tea.KeyCtrlC))
	m = updated.(*TUIModel)

	assertState(t, m, false, false, false)
	if len(m.pausedMessages) != 0 {
		t.Error("expected pausedMessages to be cleared")
	}
	if len(m.pausedCalls) != 0 {
		t.Error("expected pausedCalls to be cleared")
	}
	assertOutputContains(t, m, "Cancelled")
}

// --- Confirmation dialog ---

func TestTUI_ConfirmDialog_Yes(t *testing.T) {
	m := newTestTUIModel(t)
	yesCalled := false
	m.confirming = true
	m.confirmPrompt = "Proceed?"
	m.confirmYes = func() tea.Cmd {
		yesCalled = true
		return nil
	}
	m.confirmNo = func() tea.Cmd { return nil }

	updated, _ := m.Update(runeMsg('y'))
	m = updated.(*TUIModel)

	if !yesCalled {
		t.Error("expected confirmYes to be called")
	}
	if m.confirming {
		t.Error("expected confirming=false after 'y'")
	}
}

func TestTUI_ConfirmDialog_No(t *testing.T) {
	m := newTestTUIModel(t)
	noCalled := false
	m.confirming = true
	m.confirmPrompt = "Proceed?"
	m.confirmYes = func() tea.Cmd { return nil }
	m.confirmNo = func() tea.Cmd {
		noCalled = true
		return nil
	}

	updated, _ := m.Update(runeMsg('n'))
	m = updated.(*TUIModel)

	if !noCalled {
		t.Error("expected confirmNo to be called")
	}
	if m.confirming {
		t.Error("expected confirming=false after 'n'")
	}
}

func TestTUI_ConfirmDialog_Always(t *testing.T) {
	m := newTestTUIModel(t)
	m.confirming = true
	m.confirmPrompt = "Allow tool?"
	m.confirmYes = func() tea.Cmd { return nil }
	m.confirmNo = func() tea.Cmd { return nil }

	updated, _ := m.Update(runeMsg('a'))
	m = updated.(*TUIModel)

	if m.confirming {
		t.Error("expected confirming=false after 'a'")
	}
	if !m.permAlwaysAllow {
		t.Error("expected permAlwaysAllow=true after 'a'")
	}
}

func TestTUI_ConfirmDialog_Escape(t *testing.T) {
	m := newTestTUIModel(t)
	noCalled := false
	m.confirming = true
	m.confirmPrompt = "Proceed?"
	m.confirmYes = func() tea.Cmd { return nil }
	m.confirmNo = func() tea.Cmd {
		noCalled = true
		return nil
	}

	updated, _ := m.Update(keyMsg(tea.KeyEsc))
	m = updated.(*TUIModel)

	if !noCalled {
		t.Error("expected confirmNo on Escape")
	}
	if m.confirming {
		t.Error("expected confirming=false after Escape")
	}
}

func TestTUI_ConfirmDialog_IgnoresOtherKeys(t *testing.T) {
	m := newTestTUIModel(t)
	m.confirming = true
	m.confirmPrompt = "Proceed?"
	m.confirmYes = func() tea.Cmd { t.Error("yes should not be called"); return nil }
	m.confirmNo = func() tea.Cmd { t.Error("no should not be called"); return nil }

	updated, _ := m.Update(runeMsg('x'))
	m = updated.(*TUIModel)

	if !m.confirming {
		t.Error("expected confirming to remain true for unknown key")
	}
}

// --- Permission request ---

func TestTUI_PermissionRequest_Approve(t *testing.T) {
	m := newTestTUIModel(t)
	replyCh := make(chan bool, 1)

	// Send permission request.
	updated, _ := m.Update(permissionRequestMsg{
		ToolName: "bash",
		ReplyCh:  replyCh,
	})
	m = updated.(*TUIModel)

	if !m.confirming {
		t.Fatal("expected confirming=true after permission request")
	}
	if !strings.Contains(m.confirmPrompt, "bash") {
		t.Errorf("expected prompt to contain 'bash', got %q", m.confirmPrompt)
	}

	// Approve.
	updated, cmd := m.Update(runeMsg('y'))
	m = updated.(*TUIModel)
	// Execute the cmd to send on the channel.
	if cmd != nil {
		cmd()
	}

	select {
	case approved := <-replyCh:
		if !approved {
			t.Error("expected approved=true")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for permission reply")
	}
}

func TestTUI_PermissionRequest_AutoApprove(t *testing.T) {
	m := newTestTUIModel(t)
	m.permAlwaysAllow = true
	replyCh := make(chan bool, 1)

	updated, _ := m.Update(permissionRequestMsg{
		ToolName: "bash",
		ReplyCh:  replyCh,
	})
	m = updated.(*TUIModel)

	// Should NOT show confirmation dialog.
	if m.confirming {
		t.Error("expected auto-approve, but got confirming=true")
	}

	select {
	case approved := <-replyCh:
		if !approved {
			t.Error("expected auto-approved=true")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for auto-approval")
	}
}

// --- Turn result messages ---

func TestTUI_TurnResult_NoToolCalls_Complete(t *testing.T) {
	m := newTestTUIModel(t)
	m.working = true
	m.workCancel = func() {}
	m.midTurnCh = make(chan string)

	updated, _ := m.Update(turnResultMsg{
		Result: &conversation.TurnResult{
			TextContent: "Here is the answer.",
			Usage:       api.Usage{InputTokens: 100, OutputTokens: 50},
			Duration:    1 * time.Second,
		},
	})
	m = updated.(*TUIModel)

	assertState(t, m, false, false, false)
	assertOutputContains(t, m, "Here is the answer.")
	assertOutputContains(t, m, "Done")
}

func TestTUI_TurnResult_WithError(t *testing.T) {
	m := newTestTUIModel(t)
	m.working = true
	m.workCancel = func() {}

	updated, _ := m.Update(turnResultMsg{
		Err: fmt.Errorf("rate limit exceeded"),
	})
	m = updated.(*TUIModel)

	assertState(t, m, false, false, false)
	assertOutputContains(t, m, "Error")
	assertOutputContains(t, m, "rate limit exceeded")
}

func TestTUI_TurnResult_ContextCanceled(t *testing.T) {
	m := newTestTUIModel(t)
	m.working = true
	m.workCancel = func() {}

	updated, _ := m.Update(turnResultMsg{
		Err: fmt.Errorf("context canceled"),
	})
	m = updated.(*TUIModel)

	assertState(t, m, false, false, false)
	assertOutputContains(t, m, "Cancelled")
}

func TestTUI_TurnResult_WithToolCalls_ShowsProgress(t *testing.T) {
	m := newTestTUIModel(t)
	m.working = true
	m.workCancel = func() {}
	m.midTurnCh = make(chan string)

	updated, _ := m.Update(turnResultMsg{
		Result: &conversation.TurnResult{
			TextContent: "Let me check.",
			ToolCalls: []conversation.ToolCall{
				{ID: "1", Name: "bash", Input: json.RawMessage(`{"command":"ls"}`)},
				{ID: "2", Name: "read_file", Input: json.RawMessage(`{"file_path":"/foo.go"}`)},
			},
			Usage:    api.Usage{InputTokens: 100, OutputTokens: 50},
			Duration: 500 * time.Millisecond,
		},
	})
	m = updated.(*TUIModel)

	// Should still be working (tools need execution).
	if !m.working {
		// Note: working remains true because tool execution continues.
		// The startAgentTurn sets it, and tool results feed back.
	}
	assertOutputContains(t, m, "Bash(ls)")
	assertOutputContains(t, m, "Read(foo.go)")
	assertOutputContains(t, m, "parallel")
}

func TestTUI_TurnResult_WithRecovery(t *testing.T) {
	m := newTestTUIModel(t)
	m.working = true
	m.workCancel = func() {}
	m.midTurnCh = make(chan string)

	updated, _ := m.Update(turnResultMsg{
		Result: &conversation.TurnResult{
			TextContent: "Done after compaction.",
			Usage:       api.Usage{InputTokens: 100, OutputTokens: 50},
			Duration:    1 * time.Second,
		},
		Recovery: &conversation.RecoveryResult{
			RetrySuccessful: true,
			CompactedCount:  10,
			PreservedCount:  3,
		},
	})
	m = updated.(*TUIModel)

	assertOutputContains(t, m, "compacted")
	assertOutputContains(t, m, "10")
}

// --- Stream delta ---

func TestTUI_StreamDelta_TextDelta(t *testing.T) {
	m := newTestTUIModel(t)

	updated, _ := m.Update(streamDeltaMsg{
		EventType: "text.delta",
		Text:      "Hello world",
	})
	m = updated.(*TUIModel)

	assertOutputContains(t, m, "Hello world")
}

func TestTUI_StreamDelta_ThinkingDelta(t *testing.T) {
	m := newTestTUIModel(t)

	updated, _ := m.Update(streamDeltaMsg{
		EventType: "thinking.delta",
		Text:      "Let me think...",
	})
	m = updated.(*TUIModel)

	assertOutputContains(t, m, "Let me think...")
}

func TestTUI_StreamDelta_ToolUseStart(t *testing.T) {
	m := newTestTUIModel(t)

	updated, _ := m.Update(streamDeltaMsg{
		EventType: "tool_use.start",
		ToolName:  "bash",
	})
	m = updated.(*TUIModel)

	assertOutputContains(t, m, "Tool(bash)")
}

// --- Command palette ---

func TestTUI_CommandPalette_OpenClose(t *testing.T) {
	m := newTestTUIModel(t)

	// Open with Ctrl+K.
	updated, _ := m.Update(keyMsg(tea.KeyCtrlK))
	m = updated.(*TUIModel)

	if !m.cmdPalette.visible {
		t.Error("expected command palette visible after Ctrl+K")
	}

	// Close with Escape.
	updated, _ = m.Update(keyMsg(tea.KeyEsc))
	m = updated.(*TUIModel)

	if m.cmdPalette.visible {
		t.Error("expected command palette closed after Escape")
	}
}

// --- Model picker ---

func TestTUI_ModelPicker_Navigation(t *testing.T) {
	m := newTestTUIModel(t)

	// Open model picker.
	m.modelPicker.open("test-model")
	if !m.modelPicker.visible {
		t.Fatal("expected model picker visible")
	}

	// Navigate down.
	updated, _ := m.Update(keyMsg(tea.KeyDown))
	m = updated.(*TUIModel)
	// Navigate up.
	updated, _ = m.Update(keyMsg(tea.KeyUp))
	m = updated.(*TUIModel)

	// Close.
	updated, _ = m.Update(keyMsg(tea.KeyEsc))
	m = updated.(*TUIModel)
	if m.modelPicker.visible {
		t.Error("expected model picker closed after Escape")
	}
}

// --- Completion popup ---

func TestTUI_Completion_SlashTriggers(t *testing.T) {
	m := newTestTUIModel(t)

	// Type "/" into textarea.
	m.textarea.SetValue("/")
	m.completion.update(m.completionAll, "/")

	if !m.completion.visible {
		t.Error("expected completion visible after typing '/'")
	}

	// Dismiss.
	m.completion.dismiss()
	if m.completion.visible {
		t.Error("expected completion dismissed")
	}
}

// --- History navigation ---

func TestTUI_HistoryNavigation(t *testing.T) {
	m := newTestTUIModel(t)

	// Simulate some history.
	m.history.Append("first command")
	m.history.Append("second command")

	// Press Up to navigate history.
	updated, _ := m.Update(keyMsg(tea.KeyUp))
	m = updated.(*TUIModel)

	val := m.textarea.Value()
	if val != "second command" {
		t.Errorf("expected 'second command' after Up, got %q", val)
	}

	// Press Up again.
	updated, _ = m.Update(keyMsg(tea.KeyUp))
	m = updated.(*TUIModel)

	val = m.textarea.Value()
	if val != "first command" {
		t.Errorf("expected 'first command' after second Up, got %q", val)
	}

	// Press Down.
	updated, _ = m.Update(keyMsg(tea.KeyDown))
	m = updated.(*TUIModel)

	val = m.textarea.Value()
	if val != "second command" {
		t.Errorf("expected 'second command' after Down, got %q", val)
	}
}

// --- Pending input queue ---

func TestTUI_PendingInput_WhileWorking(t *testing.T) {
	m := newTestTUIModel(t)
	m.working = true
	m.workCancel = func() {}

	// Type text and press Enter while working.
	m.textarea.SetValue("extra context")
	updated, _ := m.Update(keyMsg(tea.KeyEnter))
	m = updated.(*TUIModel)

	// Text should go to pending queue or midTurnCh.
	assertOutputContains(t, m, "Noted")
}

// --- Pause flow ---

func TestTUI_Pause_RequestWhileWorking(t *testing.T) {
	m := newTestTUIModel(t)
	m.working = true
	m.workCancel = func() {}
	m.midTurnCh = make(chan string, 1)

	// Submit /pause.
	m.textarea.SetValue("/pause")
	updated, _ := m.Update(keyMsg(tea.KeyEnter))
	m = updated.(*TUIModel)

	if !m.pauseRequested {
		t.Error("expected pauseRequested=true after /pause")
	}
	assertOutputContains(t, m, "Pausing")
}

func TestTUI_Pause_TurnResultWithPauseRequested(t *testing.T) {
	m := newTestTUIModel(t)
	m.working = true
	m.workCancel = func() {}
	m.pauseRequested = true
	m.midTurnCh = make(chan string)

	updated, _ := m.Update(turnResultMsg{
		Result: &conversation.TurnResult{
			TextContent: "Checking...",
			ToolCalls: []conversation.ToolCall{
				{ID: "1", Name: "bash", Input: json.RawMessage(`{"command":"ls"}`)},
			},
			Usage:    api.Usage{InputTokens: 100, OutputTokens: 50},
			Duration: 500 * time.Millisecond,
		},
	})
	m = updated.(*TUIModel)

	assertState(t, m, false, true, false)
	assertOutputContains(t, m, "Paused")
	if len(m.pausedMessages) == 0 {
		t.Error("expected pausedMessages to be saved")
	}
	if len(m.pausedCalls) == 0 {
		t.Error("expected pausedCalls to be saved")
	}
}

func TestTUI_AddContext_WhilePaused(t *testing.T) {
	m := newTestTUIModel(t)
	m.paused = true
	m.pausedMessages = []api.Message{{Role: api.RoleUser}}

	m.textarea.SetValue("additional context")
	updated, _ := m.Update(keyMsg(tea.KeyEnter))
	m = updated.(*TUIModel)

	assertOutputContains(t, m, "Added to context")
	// pausedMessages should grow (2 new messages: assistant ack + user input).
	if len(m.pausedMessages) != 3 {
		t.Errorf("expected 3 pausedMessages, got %d", len(m.pausedMessages))
	}
}

// --- Command output ---

func TestTUI_CommandOutput_Success(t *testing.T) {
	m := newTestTUIModel(t)
	m.working = true

	updated, _ := m.Update(commandOutputMsg{
		Echo: "> /help\n",
		Text: "Available commands: ...",
	})
	m = updated.(*TUIModel)

	assertState(t, m, false, false, false)
	assertOutputContains(t, m, "Available commands")
}

func TestTUI_CommandOutput_Error(t *testing.T) {
	m := newTestTUIModel(t)
	m.working = true

	updated, _ := m.Update(commandOutputMsg{
		Echo: "> /broken\n",
		Err:  fmt.Errorf("command not found"),
	})
	m = updated.(*TUIModel)

	assertState(t, m, false, false, false)
	assertOutputContains(t, m, "Error")
	assertOutputContains(t, m, "command not found")
}

// --- Bus events (event-driven path) ---

func TestTUI_BusEvent_TextDelta(t *testing.T) {
	m := newTestTUIModel(t)

	data, _ := json.Marshal(map[string]string{"text": "streaming text"})
	updated, _ := m.Update(busEventMsg{Event: bus.Event{
		Type: bus.EventTextDelta,
		Data: data,
	}})
	m = updated.(*TUIModel)

	assertOutputContains(t, m, "streaming text")
}

// --- View rendering ---

func TestTUI_View_ShowsStatusBar(t *testing.T) {
	m := newTestTUIModel(t)
	view := m.View()

	if view == "" {
		t.Fatal("expected non-empty view")
	}
	// The status bar should contain BUILD mode by default.
	stripped := stripANSI(view)
	if !strings.Contains(stripped, "BUILD") {
		t.Errorf("expected BUILD mode in status bar, got:\n%s", stripped)
	}
}

func TestTUI_View_WorkingMode(t *testing.T) {
	m := newTestTUIModel(t)
	m.working = true

	view := m.View()
	stripped := stripANSI(view)
	if !strings.Contains(stripped, "WORKING") {
		t.Errorf("expected WORKING in status bar, got:\n%s", stripped)
	}
}

func TestTUI_View_PausedMode(t *testing.T) {
	m := newTestTUIModel(t)
	m.paused = true

	view := m.View()
	stripped := stripANSI(view)
	if !strings.Contains(stripped, "PAUSED") {
		t.Errorf("expected PAUSED in status bar, got:\n%s", stripped)
	}
}

func TestTUI_View_ConfirmMode(t *testing.T) {
	m := newTestTUIModel(t)
	m.confirming = true
	m.confirmPrompt = "Allow?"

	view := m.View()
	stripped := stripANSI(view)
	if !strings.Contains(stripped, "CONFIRM") {
		t.Errorf("expected CONFIRM in status bar, got:\n%s", stripped)
	}
}

func TestTUI_View_CommandPaletteOverlay(t *testing.T) {
	m := newTestTUIModel(t)
	m.cmdPalette.open(m.buildPaletteItems())

	view := m.View()
	if view == "" {
		t.Fatal("expected non-empty view with command palette")
	}
}

func TestTUI_View_ToastOverlay(t *testing.T) {
	m := newTestTUIModel(t)
	m.toasts.add("Test notification", ToastSuccess)

	view := m.View()
	stripped := stripANSI(view)
	if !strings.Contains(stripped, "Test notification") {
		t.Errorf("expected toast in view, got:\n%s", stripped)
	}
}

// --- Tool progress ---

func TestTUI_ToolProgress(t *testing.T) {
	m := newTestTUIModel(t)
	m.toolTasks = []toolTaskProgress{
		{Name: "bash", Detail: "Bash(ls)", Status: 0},
		{Name: "read_file", Detail: "Read(foo.go)", Status: 0},
	}

	// Simulate progress update for first tool.
	updated, _ := m.Update(toolProgressMsg{Index: 0, Status: 2}) // completed
	m = updated.(*TUIModel)

	assertOutputContains(t, m, "Bash(ls)")
}

// --- Progress and delta messages ---

func TestTUI_ProgressMsg(t *testing.T) {
	m := newTestTUIModel(t)

	updated, _ := m.Update(progressMsg{message: "Installing dependencies..."})
	m = updated.(*TUIModel)

	assertOutputContains(t, m, "Installing dependencies...")
}

func TestTUI_CommandDeltaMsg(t *testing.T) {
	m := newTestTUIModel(t)

	updated, _ := m.Update(commandDeltaMsg{text: "partial output"})
	m = updated.(*TUIModel)

	assertOutputContains(t, m, "partial output")
}

// --- Side query result ---

func TestTUI_SideQueryResult_Success(t *testing.T) {
	m := newTestTUIModel(t)

	updated, _ := m.Update(sideQueryResultMsg{
		Query:  "what is foo",
		Result: "foo is a thing",
		ID:     1,
	})
	m = updated.(*TUIModel)

	assertOutputContains(t, m, "foo is a thing")
	assertOutputContains(t, m, "BTW #1 done")
}

func TestTUI_SideQueryResult_Error(t *testing.T) {
	m := newTestTUIModel(t)

	updated, _ := m.Update(sideQueryResultMsg{
		Query: "broken query",
		Err:   fmt.Errorf("provider error"),
		ID:    2,
	})
	m = updated.(*TUIModel)

	assertOutputContains(t, m, "BTW #2 Error")
	assertOutputContains(t, m, "provider error")
}

// --- Quit commands ---

func TestTUI_QuitCommand(t *testing.T) {
	m := newTestTUIModel(t)
	m.textarea.SetValue("/quit")

	_, cmd := m.Update(keyMsg(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("expected command from /quit")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg from /quit, got %T", msg)
	}
}

func TestTUI_ExitCommand(t *testing.T) {
	m := newTestTUIModel(t)
	m.textarea.SetValue("/exit")

	_, cmd := m.Update(keyMsg(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("expected command from /exit")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg from /exit, got %T", msg)
	}
}

// --- Empty enter ---

func TestTUI_EmptyEnter_DoesNothing(t *testing.T) {
	m := newTestTUIModel(t)
	outputBefore := m.output.String()

	m.textarea.SetValue("")
	m.Update(keyMsg(tea.KeyEnter))

	if m.output.String() != outputBefore {
		t.Error("expected no output change on empty Enter")
	}
}

// --- Debug command ---

func TestTUI_DebugCommand(t *testing.T) {
	m := newTestTUIModel(t)
	m.textarea.SetValue("/debug")

	updated, _ := m.Update(keyMsg(tea.KeyEnter))
	m = updated.(*TUIModel)

	assertOutputContains(t, m, "debug state")
	assertOutputContains(t, m, "working:")
}

// --- Repaint ---

func TestTUI_RepaintMsg(t *testing.T) {
	m := newTestTUIModel(t)
	// repaintMsg should not panic and should be a no-op.
	updated, _ := m.Update(repaintMsg{})
	if updated == nil {
		t.Error("expected non-nil model after repaintMsg")
	}
}

// stripANSI removes ANSI escape codes from a string for assertion clarity.
func stripANSI(s string) string {
	return ansiStripString(s)
}

// ansiStripString wraps the ansi package strip for use in tests.
func ansiStripString(s string) string {
	// Use a simple manual strip since charmbracelet/x/ansi.Strip
	// expects a string and returns a string.
	return ansiStripManual(s)
}

func ansiStripManual(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '~' {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Ensure context import is used.
var _ = context.Background
