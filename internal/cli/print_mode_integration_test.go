package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/prompt"
	"github.com/qiangli/ycode/internal/runtime/session"
	"github.com/qiangli/ycode/internal/tools"
)

// mockTextProvider is an api.Provider that returns a single text response.
type mockTextProvider struct {
	reply string
}

func (m *mockTextProvider) Kind() api.ProviderKind { return api.ProviderAnthropic }

func (m *mockTextProvider) Send(_ context.Context, _ *api.Request) (<-chan *api.StreamEvent, <-chan error) {
	events := make(chan *api.StreamEvent, 4)
	errCh := make(chan error, 1)

	delta, _ := json.Marshal(struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}{Type: "text_delta", Text: m.reply})

	events <- &api.StreamEvent{Type: "content_block_delta", Delta: delta}
	events <- &api.StreamEvent{Type: "message_delta", Delta: json.RawMessage(`{"stop_reason":"end_turn"}`)}
	close(events)
	close(errCh)
	return events, errCh
}

// newPrintModeTestApp builds an App wired to in-memory stdout/stderr buffers
// and a mock provider that replies with the given text.
func newPrintModeTestApp(t *testing.T, reply string, printMode bool) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	sess, err := session.New(dir)
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Model = "claude-3-5-sonnet"

	registry := tools.NewRegistry()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	app, err := NewApp(cfg, &mockTextProvider{reply: reply}, sess, AppOptions{
		WorkDir:      dir,
		ToolRegistry: registry,
		PromptCtx: &prompt.ProjectContext{
			WorkDir:  dir,
			Platform: "linux",
		},
	})
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	app.stdout = stdout
	app.stderr = stderr
	app.SetPrintMode(printMode)
	return app, stdout, stderr
}

// TestIntegration_PrintMode_OnlyAnswerOnStdout verifies Gate 1: with --print,
// ycode emits only the model's answer on stdout, routes chrome/diagnostics to
// stderr, and exits 0.
func TestIntegration_PrintMode_OnlyAnswerOnStdout(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in -short")
	}

	const answer = "42"
	app, stdout, stderr := newPrintModeTestApp(t, answer, true)
	defer app.Close()

	err := app.RunPrompt(context.Background(), "what is the answer")
	if err != nil {
		t.Fatalf("RunPrompt returned error: %v", err)
	}

	gotStdout := stdout.String()
	gotStderr := stderr.String()

	if gotStdout != answer {
		t.Errorf("stdout = %q, want %q", gotStdout, answer)
	}

	// Chrome must not leak into stdout.
	for _, unwanted := range []string{"Session Summary", "Duration:", "tokens", "⚙", "⟳", "⚠", "✘"} {
		if strings.Contains(gotStdout, unwanted) {
			t.Errorf("stdout unexpectedly contains chrome %q; stdout = %q", unwanted, gotStdout)
		}
	}

	// Session summary should have gone to stderr.
	if !strings.Contains(gotStderr, "Session Summary") {
		t.Errorf("stderr missing session summary; stderr = %q", gotStderr)
	}

	// Stderr may also include token metrics/duration.
	if !strings.Contains(gotStderr, "tokens") && !strings.Contains(gotStderr, "Duration:") {
		t.Logf("stderr did not contain expected diagnostics: %q", gotStderr)
	}
}

// TestIntegration_PrintMode_NoMarkdownWrapperOrPreamble verifies there is no
// markdown fence, no preamble, and no session summary in stdout.
func TestIntegration_PrintMode_NoMarkdownWrapperOrPreamble(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in -short")
	}

	const answer = "The answer is 42."
	app, stdout, _ := newPrintModeTestApp(t, answer, true)
	defer app.Close()

	err := app.RunPrompt(context.Background(), "what is the answer")
	if err != nil {
		t.Fatalf("RunPrompt returned error: %v", err)
	}

	got := stdout.String()

	// No markdown wrapper.
	if strings.HasPrefix(got, "```") || strings.HasSuffix(strings.TrimSpace(got), "```") {
		t.Errorf("stdout looks wrapped in markdown: %q", got)
	}

	// No session summary.
	if strings.Contains(got, "Session Summary") {
		t.Errorf("stdout contains session summary: %q", got)
	}

	// No preamble phrases.
	for _, preamble := range []string{
		"Here is the answer",
		"Answer:",
		"Result:",
		"Output:",
	} {
		if strings.Contains(got, preamble) {
			t.Errorf("stdout contains preamble %q: %q", preamble, got)
		}
	}

	if got != answer {
		t.Errorf("stdout = %q, want %q", got, answer)
	}
}

// TestIntegration_NonPrintMode_KeepsChromeOnStdout ensures the default path is
// unaffected: chrome remains on stdout when print mode is disabled.
func TestIntegration_NonPrintMode_KeepsChromeOnStdout(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in -short")
	}

	const answer = "42"
	app, stdout, stderr := newPrintModeTestApp(t, answer, false)
	defer app.Close()

	err := app.RunPrompt(context.Background(), "what is the answer")
	if err != nil {
		t.Fatalf("RunPrompt returned error: %v", err)
	}

	gotStdout := stdout.String()
	gotStderr := stderr.String()

	if !strings.Contains(gotStdout, answer) {
		t.Errorf("stdout missing answer; stdout = %q", gotStdout)
	}
	if !strings.Contains(gotStdout, "Session Summary") {
		t.Errorf("stdout missing session summary in non-print mode; stdout = %q", gotStdout)
	}
	if gotStderr != "" {
		t.Errorf("unexpected stderr output in non-print mode: %q", gotStderr)
	}
}
