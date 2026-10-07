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
	go func() {
		defer close(events)
		defer close(errs)
		if bytes.Contains(raw, []byte("slow")) && !bytes.Contains(raw, []byte("steer me")) {
			<-ctx.Done()
			errs <- ctx.Err()
			return
		}
		text, _ := json.Marshal(map[string]string{"type": "text_delta", "text": fmt.Sprintf("stub-answer-%d", n)})
		stop, _ := json.Marshal(map[string]string{"stop_reason": api.StopReasonEndTurn})
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
	command := exec.Command(os.Args[0], "-test.run=^TestTUIOverPTY$")
	command.Env = append(os.Environ(), "YCODE_TUI_PTY_HELPER=1", "HOME="+home, "BASHY_HOME=", "OPENAI_API_KEY=test-secret", "TERM=xterm-256color", SessionFileEnv+"=")
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

	typeLine("/save first work")
	expect("/save with a title", "saved: session")
	expect("/save renamed through the subcommand", "first work")

	typeLine("/model")
	expect("/model", "current: ")
	typeLine("/model other")
	expect("/model switch is not faked", "never changed")

	typeLine("/plan")
	expect("/plan", "declares no `plan` subcommand")
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
	if !screen.waitCount("turn ended in", 2, 15*time.Second) {
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
	app, err := openHarnessApplication(harnessFixture(t), public.WithHarnessProvider("openai", provider))
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
	root.SetArgs([]string{"--file", harnessFixture(t)})
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
