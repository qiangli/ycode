//go:build !windows

package ycodecli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/creack/pty/v2"

	"github.com/qiangli/ycode/internal/api"
	harnesscli "github.com/qiangli/ycode/internal/harness/cli"
	public "github.com/qiangli/ycode/pkg/ycode"
)

// tuiProvider answers every turn with a fixed text; a request that mentions
// "slow" blocks until its turn is cancelled, so ESC has something to stop.
type tuiProvider struct{ calls atomic.Int32 }

func (*tuiProvider) Kind() api.ProviderKind { return api.ProviderOpenAI }
func (p *tuiProvider) Send(ctx context.Context, request *api.Request) (<-chan *api.StreamEvent, <-chan error) {
	n := p.calls.Add(1)
	events := make(chan *api.StreamEvent, 3)
	errs := make(chan error, 1)
	raw, _ := json.Marshal(request.Messages)
	var last []byte
	if len(request.Messages) > 0 {
		last, _ = json.Marshal(request.Messages[len(request.Messages)-1])
	}
	go func() {
		defer close(events)
		defer close(errs)
		if bytes.Contains(raw, []byte("slow")) && !bytes.Contains(raw, []byte("steer me")) {
			<-ctx.Done()
			errs <- ctx.Err()
			return
		}
		stop, _ := json.Marshal(map[string]string{"stop_reason": api.StopReasonEndTurn})
		if bytes.Contains(last, []byte("stream please")) {
			// The first line must reach the screen while the provider is
			// still answering: the rest waits for the test to see it.
			first, _ := json.Marshal(map[string]string{"type": "text_delta", "text": "streamed-line-one\n"})
			events <- &api.StreamEvent{Type: "content_block_delta", Delta: first}
			gate := os.Getenv("YCODE_TUI_STREAM_GATE")
			for gate != "" {
				if _, err := os.Stat(gate); err == nil {
					break
				}
				select {
				case <-ctx.Done():
					errs <- ctx.Err()
					return
				case <-time.After(20 * time.Millisecond):
				}
			}
			rest, _ := json.Marshal(map[string]string{"type": "text_delta", "text": "streamed-line-two"})
			events <- &api.StreamEvent{Type: "content_block_delta", Delta: rest}
			events <- &api.StreamEvent{Type: "message_delta", Delta: stop}
			return
		}
		if bytes.Contains(last, []byte("rules?")) {
			// Reports whether the compiled context carried the repository
			// instruction file /init seeds (its template holds the marker).
			text, _ := json.Marshal(map[string]string{"type": "text_delta", "text": fmt.Sprintf("instructions-in-context=%v", strings.Contains(request.System, "GENIE-MARKER-387") || bytes.Contains(raw, []byte("GENIE-MARKER-387")))})
			events <- &api.StreamEvent{Type: "content_block_delta", Delta: text}
			events <- &api.StreamEvent{Type: "message_delta", Delta: stop}
			return
		}
		if bytes.Contains(last, []byte("what did I say?")) {
			// Reports whether anything of a cleared conversation reached
			// this request, and which model the session's selection chose.
			text, _ := json.Marshal(map[string]string{"type": "text_delta", "text": fmt.Sprintf("old-in-request=%v model=%s", bytes.Contains(raw, []byte("CLEAR-OLD-387")) || strings.Contains(request.System, "CLEAR-OLD-387"), request.Model)})
			events <- &api.StreamEvent{Type: "content_block_delta", Delta: text}
			events <- &api.StreamEvent{Type: "message_delta", Delta: stop}
			return
		}
		if bytes.Contains(last, []byte("approve-me")) {
			// An exact workspace overwrite matches the compiled destructive
			// rule: the engine suspends on hitl.waiting until the human answers.
			toolStop, _ := json.Marshal(map[string]string{"stop_reason": api.StopReasonToolUse})
			events <- &api.StreamEvent{Type: "content_block_start", ContentBlock: &api.ContentBlock{Type: api.ContentTypeToolUse, ID: "hitl-call", Name: "bashy", Input: json.RawMessage(`{"script":"printf approved-write > hitl.txt"}`)}}
			events <- &api.StreamEvent{Type: "content_block_stop"}
			events <- &api.StreamEvent{Type: "message_delta", Delta: toolStop}
			return
		}
		text, _ := json.Marshal(map[string]string{"type": "text_delta", "text": fmt.Sprintf("stub-answer-%d", n)})
		events <- &api.StreamEvent{Type: "content_block_delta", Delta: text}
		events <- &api.StreamEvent{Type: "message_delta", Delta: stop}
		events <- &api.StreamEvent{Type: "message_stop"}
	}()
	return events, errs
}

// TestTUIOverPTY drives the tui frontend behind a real pseudo-terminal with
// the stub model: every slash, a free-text turn, a literal command line, a
// mid-turn steer and ESC.
func TestTUIOverPTY(t *testing.T) {
	if os.Getenv("YCODE_TUI_PTY_HELPER") == "1" {
		tuiPTYHelper(t)
		return
	}
	home := t.TempDir()
	workdir := t.TempDir()
	gate := filepath.Join(t.TempDir(), "release")
	command := exec.Command(os.Args[0], "-test.run=^TestTUIOverPTY$")
	command.Dir = workdir
	// The workspace is the agent file's directory: a copy in a temp dir keeps
	// the approved write out of the source tree.
	canonical, err := os.ReadFile(harnessFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(workdir, "agent.yaml")
	if err := os.WriteFile(fixture, canonical, 0o600); err != nil {
		t.Fatal(err)
	}
	command.Env = append(os.Environ(), "YCODE_TUI_PTY_HELPER=1", "YCODE_TUI_FIXTURE="+fixture, "YCODE_TUI_STREAM_GATE="+gate, "HOME="+home, "BASHY_HOME=", "OPENAI_API_KEY=test-secret", "TERM=xterm-256color", SessionFileEnv+"=")
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Rows: 40, Cols: 140})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = terminal.Close()
		if command.Process != nil {
			_ = command.Process.Kill()
		}
	})
	screen := &ptyScreen{}
	go func() { _, _ = io.Copy(screen, terminal) }()
	expect := func(step, want string) {
		t.Helper()
		if !screen.waitFor(want, 15*time.Second) {
			t.Fatalf("%s: %q never appeared; screen:\n%s", step, want, screen.text())
		}
	}
	typeLine := func(line string) {
		t.Helper()
		if _, err := io.WriteString(terminal, line+"\r"); err != nil {
			t.Fatal(err)
		}
	}

	expect("greeting", "/help. Ctrl-D leaves.")
	typeLine("/help")
	expect("/help", "/resume [SESSION]")
	expect("/help maps to subcommands", "session rename SESSION TITLE")

	typeLine("/save")
	expect("/save before any turn", "nothing to save yet")

	typeLine("hello there")
	expect("free-text turn", "stub-answer-1")
	expect("turn end", "turn ended in")

	typeLine("echo literal-$((40+2))")
	expect("literal command line", "literal-42")

	// Streaming: the provider's first line is on screen while the turn is
	// still running; the rest follows once released.
	typeLine("stream please")
	expect("streamed first line", "● streamed-line-one")
	if strings.Contains(screen.text(), "streamed-line-two") {
		t.Fatalf("second line rendered before the provider sent it:\n%s", screen.text())
	}
	if err := os.WriteFile(gate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	expect("streamed rest", "streamed-line-two")
	if !screen.waitCount("turn ended in", 2, 15*time.Second) {
		t.Fatalf("the streamed turn never ended:\n%s", screen.text())
	}
	if n := strings.Count(screen.text(), "streamed-line-one"); n != 1 {
		t.Fatalf("streamed answer printed %d times, want once:\n%s", n, screen.text())
	}

	// In-turn confirm: a destructive write waits for y; nothing is written
	// before the human approves, and the approved write lands.
	typeLine("approve-me please")
	expect("hitl prompt", "approval needed (")
	expect("hitl status", "approve? y/n")
	if _, err := os.Stat(filepath.Join(workdir, "hitl.txt")); err == nil {
		t.Fatalf("destructive write ran before approval")
	}
	if _, err := io.WriteString(terminal, "y"); err != nil {
		t.Fatal(err)
	}
	if !screen.waitCount("turn ended in", 3, 15*time.Second) {
		t.Fatalf("the approved turn never ended:\n%s", screen.text())
	}
	if got, err := os.ReadFile(filepath.Join(workdir, "hitl.txt")); err != nil || string(got) != "approved-write" {
		t.Fatalf("approved write = %q, %v; screen:\n%s", got, err, screen.text())
	}

	typeLine("/save first work")
	expect("/save with a title", "saved: session")
	expect("/save renamed through the subcommand", "first work")

	// /model goes through the engine's session model API: only resources
	// in the default agent's declared route are selectable.
	typeLine("/model")
	expect("/model", "current: primary (")
	expect("/model lists the route", "/model NAME selects")
	typeLine("/model other")
	expect("/model rejects an undeclared resource", `undeclared model "other"`)
	typeLine("/model primary")
	expect("/model selects a declared resource", "now uses primary")

	// /plan toggles the durable plan mode; /plan TEXT runs a planning turn
	// that streams like any turn, and a tool call in it fails, never runs.
	typeLine("/plan")
	expect("/plan enters plan mode", "mode: plan")
	expect("status line shows plan mode", "plan mode")
	typeLine("approve-me while planning")
	expect("plan turn refuses tools", "planning forbids tool calls")
	if !screen.waitCount("turn ended in", 4, 15*time.Second) {
		t.Fatalf("the planning turn never ended:\n%s", screen.text())
	}
	if n := strings.Count(screen.text(), "approval needed ("); n != 1 {
		t.Fatalf("a planning turn asked for approval:\n%s", screen.text())
	}
	typeLine("/plan")
	expect("/plan returns to act", "mode: act")
	typeLine("/plan outline the work")
	expect("/plan TEXT re-enters plan mode", "› outline the work")
	if !screen.waitCount("mode: plan", 2, 15*time.Second) {
		t.Fatalf("/plan TEXT did not enter plan mode:\n%s", screen.text())
	}
	if !screen.waitCount("turn ended in", 5, 15*time.Second) {
		t.Fatalf("the /plan TEXT turn never ended:\n%s", screen.text())
	}
	typeLine("/plan")
	if !screen.waitCount("mode: act", 2, 15*time.Second) {
		t.Fatalf("/plan did not return to act:\n%s", screen.text())
	}

	typeLine("/init")
	expect("/init", "declares no `init` subcommand")

	typeLine("/resume")
	expect("/resume on the latest", "already on session")
	typeLine("/resume no-such-session")
	expect("/resume unknown", "no session")

	typeLine("/bogus")
	expect("unknown slash", "unknown slash /bogus")

	// Mid-turn: a line typed during a turn steers it; ESC stops the turn and
	// what was typed runs next.
	typeLine("please be slow")
	expect("slow turn running", "esc stops")
	typeLine("steer me")
	expect("steer", "↳ steer: steer me")
	if _, err := io.WriteString(terminal, "\x1b"); err != nil {
		t.Fatal(err)
	}
	expect("ESC", "interrupted; running what was typed during it")
	expect("follow-up turn", "› steer me")
	if !screen.waitCount("turn ended in", 6, 15*time.Second) {
		t.Fatalf("the follow-up turn never ended:\n%s", screen.text())
	}

	typeLine("/quit")
	expect("/quit", "stays resumable")
	expect("helper exit", "TUI-EXIT ok")
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("helper: %v\n%s", err, screen.text())
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("helper did not exit after /quit:\n%s", screen.text())
	}
}

func tuiPTYHelper(t *testing.T) {
	provider := &tuiProvider{}
	fixture := os.Getenv("YCODE_TUI_FIXTURE")
	app, err := openHarnessApplication(fixture, public.WithHarnessProvider("openai", provider))
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	root, err := harnesscli.New(app.doc, func(ctx context.Context, inv harnesscli.Invocation, streams harnesscli.IO) error {
		if inv.Dispatch.Operation == "input" {
			return runCLIInput(ctx, app, inv, streams)
		}
		return dispatchCLI(ctx, inv, streams)
	}, harnesscli.Options{IsTerminal: true, LookupEnv: os.LookupEnv})
	if err != nil {
		t.Fatal(err)
	}
	root.SetArgs(append([]string{"--file", fixture}, strings.Fields(os.Getenv("YCODE_TUI_ARGS"))...))
	err = root.ExecuteContext(context.Background())
	status := "ok"
	if err != nil {
		status = err.Error()
	}
	fmt.Printf("\r\nTUI-EXIT %s calls=%d\r\n", status, provider.calls.Load())
	if err != nil {
		t.Fatal(err)
	}
}

// ptyScreen accumulates terminal output with escape sequences removed.
type ptyScreen struct {
	mu  sync.Mutex
	raw bytes.Buffer
}

func (s *ptyScreen) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.raw.Write(p)
}

var ansiSequence = regexp.MustCompile(`\x1b\[[0-9;?<>=]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[PX^_][^\x1b]*\x1b\\|\x1b[@-Z\\-_]`)

func (s *ptyScreen) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ansiSequence.ReplaceAllString(s.raw.String(), "")
}

func (s *ptyScreen) waitCount(want string, count int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Count(s.text(), want) >= count {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func (s *ptyScreen) waitFor(want string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(s.text(), want) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
