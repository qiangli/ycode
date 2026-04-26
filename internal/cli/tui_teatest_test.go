//go:build integration

package cli

import (
	"bytes"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

// teatest-based tests run the full bubbletea program lifecycle:
// Init → Update → View, with real terminal output capture.
//
// NOTE: These tests should be run WITHOUT -race because bubbletea v1.3.10
// has an upstream data race in standardRenderer between setWindowTitle and
// flush (muesli/ansi compressor.Writer is not goroutine-safe). The race is
// not in ycode code. Direct Update() tests in tui_integration_test.go are
// race-safe and cover the same logic paths.

func TestTeatest_InitAndQuit(t *testing.T) {
	app := newTestApp(t)
	m := NewTUIModel(app)

	tm := teatest.NewTestModel(t, m,
		teatest.WithInitialTermSize(80, 24),
	)

	// Wait for the welcome text to appear in the output.
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("ycode"))
	}, teatest.WithDuration(3*time.Second))

	// Send Ctrl+D to quit.
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlD})

	// Get final model and verify it was ready.
	finalModel := tm.FinalModel(t, teatest.WithFinalTimeout(3*time.Second))
	if finalM, ok := finalModel.(*TUIModel); ok {
		if !finalM.ready {
			t.Error("expected final model to be ready")
		}
	}
}

func TestTeatest_TypeAndQuit(t *testing.T) {
	app := newTestApp(t)
	m := NewTUIModel(app)

	tm := teatest.NewTestModel(t, m,
		teatest.WithInitialTermSize(80, 24),
	)

	// Wait for ready state.
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("ycode"))
	}, teatest.WithDuration(3*time.Second))

	// Type /quit and Enter to exit.
	tm.Type("/quit")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

func TestTeatest_DebugCommand(t *testing.T) {
	app := newTestApp(t)
	m := NewTUIModel(app)

	tm := teatest.NewTestModel(t, m,
		teatest.WithInitialTermSize(80, 24),
	)

	// Wait for initialization.
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("ycode"))
	}, teatest.WithDuration(3*time.Second))

	// Type /debug and press Enter.
	tm.Type("/debug")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// Wait for debug output to appear. WaitFor accumulates reads, so we
	// check for a substring that appears in the debug state dump.
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("working:"))
	}, teatest.WithDuration(3*time.Second))

	// Quit.
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlD})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

func TestTeatest_CommandPaletteFlow(t *testing.T) {
	app := newTestApp(t)
	m := NewTUIModel(app)

	tm := teatest.NewTestModel(t, m,
		teatest.WithInitialTermSize(80, 24),
	)

	// Wait for initialization.
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("ycode"))
	}, teatest.WithDuration(3*time.Second))

	// Open command palette with Ctrl+K.
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlK})
	time.Sleep(100 * time.Millisecond)

	// Close with Escape.
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})

	// Quit.
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlD})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

func TestTeatest_StreamDeltaRendering(t *testing.T) {
	app := newTestApp(t)
	m := NewTUIModel(app)

	tm := teatest.NewTestModel(t, m,
		teatest.WithInitialTermSize(80, 24),
	)

	// Wait for initialization.
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("ycode"))
	}, teatest.WithDuration(3*time.Second))

	// Simulate streaming text from LLM.
	tm.Send(streamDeltaMsg{EventType: "text.delta", Text: "Hello from the LLM!"})

	// Wait for text to appear.
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("Hello from the LLM!"))
	}, teatest.WithDuration(3*time.Second))

	// Quit.
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlD})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

func TestTeatest_ConfirmationFlow(t *testing.T) {
	app := newTestApp(t)
	m := NewTUIModel(app)

	tm := teatest.NewTestModel(t, m,
		teatest.WithInitialTermSize(80, 24),
	)

	// Wait for initialization.
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("ycode"))
	}, teatest.WithDuration(3*time.Second))

	// Send a permission request.
	replyCh := make(chan bool, 1)
	tm.Send(permissionRequestMsg{
		ToolName: "bash",
		ReplyCh:  replyCh,
	})

	time.Sleep(100 * time.Millisecond)

	// Approve with 'y'.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	// Wait for approval.
	select {
	case approved := <-replyCh:
		if !approved {
			t.Error("expected approval")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for approval")
	}

	// Quit.
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlD})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

func TestTeatest_ProgressMsg(t *testing.T) {
	app := newTestApp(t)
	m := NewTUIModel(app)

	tm := teatest.NewTestModel(t, m,
		teatest.WithInitialTermSize(80, 24),
	)

	// Wait for initialization.
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("ycode"))
	}, teatest.WithDuration(3*time.Second))

	// Send a progress message.
	tm.Send(progressMsg{message: "Build successful!"})

	// Wait for it to render.
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("Build successful!"))
	}, teatest.WithDuration(3*time.Second))

	// Quit.
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlD})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

func TestTeatest_HistoryNavigation(t *testing.T) {
	app := newTestApp(t)
	m := NewTUIModel(app)
	// Pre-populate history.
	m.history.Append("first message")
	m.history.Append("second message")

	tm := teatest.NewTestModel(t, m,
		teatest.WithInitialTermSize(80, 24),
	)

	// Wait for initialization.
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("ycode"))
	}, teatest.WithDuration(3*time.Second))

	// Press Up to recall history.
	tm.Send(tea.KeyMsg{Type: tea.KeyUp})
	time.Sleep(100 * time.Millisecond)

	// Press Up again.
	tm.Send(tea.KeyMsg{Type: tea.KeyUp})
	time.Sleep(100 * time.Millisecond)

	// Press Down.
	tm.Send(tea.KeyMsg{Type: tea.KeyDown})
	time.Sleep(100 * time.Millisecond)

	// Quit.
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlD})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}
