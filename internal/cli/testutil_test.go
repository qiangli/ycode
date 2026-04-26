package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/commands"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/session"
	"github.com/qiangli/ycode/internal/runtime/usage"
)

// newTestApp creates a minimal App suitable for testing.
// No provider, no tool registry — just enough to construct a TUIModel.
func newTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	sess := &session.Session{
		ID:        "test-session",
		CreatedAt: time.Now(),
		Dir:       dir,
	}
	cfg := &config.Config{
		Model:          "test-model",
		PermissionMode: "ask",
	}
	renderer, err := NewRenderer("")
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	return &App{
		config:       cfg,
		session:      sess,
		commands:     commands.NewRegistry(),
		renderer:     renderer,
		version:      "test",
		workDir:      dir,
		usageTracker: usage.NewTracker(),
		sessionStart: time.Now(),
		stdout:       os.Stdout,
	}
}

// newTestTUIModel creates a TUIModel and initializes it with a WindowSizeMsg
// so that m.ready is true and the viewport is usable.
func newTestTUIModel(t *testing.T) *TUIModel {
	t.Helper()
	app := newTestApp(t)
	m := NewTUIModel(app)
	// Initialize: send WindowSizeMsg to set up viewport.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return updated.(*TUIModel)
}

// fakeAgentClient implements agentClient for testing.
type fakeAgentClient struct {
	sendFunc   func(ctx context.Context, sessionID string, input bus.MessageInput) error
	cancelFunc func(ctx context.Context, sessionID string) error
	eventsCh   chan bus.Event
}

func (f *fakeAgentClient) SendMessage(ctx context.Context, sessionID string, input bus.MessageInput) error {
	if f.sendFunc != nil {
		return f.sendFunc(ctx, sessionID, input)
	}
	return nil
}

func (f *fakeAgentClient) CancelTurn(ctx context.Context, sessionID string) error {
	if f.cancelFunc != nil {
		return f.cancelFunc(ctx, sessionID)
	}
	return nil
}

func (f *fakeAgentClient) Events(ctx context.Context, filter ...bus.EventType) (<-chan bus.Event, error) {
	if f.eventsCh != nil {
		return f.eventsCh, nil
	}
	ch := make(chan bus.Event)
	close(ch)
	return ch, nil
}

// sendKeys applies a sequence of key messages to a tea.Model, returning
// the final model. Commands are discarded.
func sendKeys(m tea.Model, keys ...tea.KeyMsg) tea.Model {
	for _, k := range keys {
		m, _ = m.Update(k)
	}
	return m
}

// keyMsg creates a tea.KeyMsg for a special key type.
func keyMsg(k tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: k}
}

// runeMsg creates a tea.KeyMsg for a rune character.
func runeMsg(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

// runesMsg creates a sequence of tea.KeyMsg for a string of runes.
func runesMsg(s string) []tea.KeyMsg {
	msgs := make([]tea.KeyMsg, len(s))
	for i, r := range s {
		msgs[i] = runeMsg(r)
	}
	return msgs
}

// modelOutput returns the stripped text content from the TUIModel's output buffer.
func modelOutput(m *TUIModel) string {
	return ansi.Strip(m.output.String())
}

// assertOutputContains checks that the model's output contains the given substring.
func assertOutputContains(t *testing.T, m *TUIModel, want string) {
	t.Helper()
	got := modelOutput(m)
	if !strings.Contains(got, want) {
		t.Errorf("output does not contain %q;\ngot: %s", want, got)
	}
}

// assertOutputNotContains checks that the model's output does NOT contain the given substring.
func assertOutputNotContains(t *testing.T, m *TUIModel, unwanted string) {
	t.Helper()
	got := modelOutput(m)
	if strings.Contains(got, unwanted) {
		t.Errorf("output unexpectedly contains %q;\ngot: %s", unwanted, got)
	}
}

// assertState checks the TUIModel's state machine flags.
func assertState(t *testing.T, m *TUIModel, working, paused, confirming bool) {
	t.Helper()
	if m.working != working {
		t.Errorf("working: got %v, want %v", m.working, working)
	}
	if m.paused != paused {
		t.Errorf("paused: got %v, want %v", m.paused, paused)
	}
	if m.confirming != confirming {
		t.Errorf("confirming: got %v, want %v", m.confirming, confirming)
	}
}

// assertGolden compares got against a golden file. When UPDATE_GOLDEN=1 is set,
// the golden file is created/updated instead.
func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".golden")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir golden: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with UPDATE_GOLDEN=1 to create)", path, err)
	}
	if got != string(want) {
		t.Errorf("golden mismatch for %s:\n--- want ---\n%s\n--- got ---\n%s", name, want, got)
	}
}
