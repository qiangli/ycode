package cli

// Hermetic reproduction of the captured headless runaway, and the controls
// proving the convergence policy does not cut productive work short.
//
// The incident shape: a headless `ycode prompt` run issued VARIED read/search
// tool calls every turn — never repeating an exact signature, so both loop
// detectors stayed silent — never wrote, never tested, never answered, and
// walked toward the 100-iteration backstop until the outer process killed it
// with nothing committed. Advisory convergence prompts and injected steering
// were not consumed. These tests reproduce that trajectory with a scripted
// provider and assert the new contract:
//
//  1. bounded convergence far below the backstop (grace, then hard stop),
//  2. a grace turn that is honored ends the run successfully,
//  3. measurable progress resets the budget (long productive runs finish),
//  4. injected steering is consumed at the next turn boundary and is
//     acknowledged on the --events wire,
//  5. disabling the policy restores the plain iteration backstop.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/commands"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/conversation"
	"github.com/qiangli/ycode/internal/runtime/permission"
	"github.com/qiangli/ycode/internal/runtime/prompt"
	"github.com/qiangli/ycode/internal/runtime/session"
	"github.com/qiangli/ycode/internal/runtime/usage"
	"github.com/qiangli/ycode/internal/tools"
	"github.com/qiangli/ycode/internal/wireevents"
)

// recordingProvider scripts each turn as a function of the request number AND
// the full request, and retains every request so tests can assert what the
// model actually saw (grace instructions, consumed steering).
type recordingProvider struct {
	mu       sync.Mutex
	requests []*api.Request
	turn     func(n int, req *api.Request) []*api.StreamEvent
	// gate, when non-nil for request n, delays that turn's completion until
	// the channel is closed — the hook tests use to inject steering while a
	// turn is verifiably still in flight.
	gate func(n int) <-chan struct{}
}

func (p *recordingProvider) Kind() api.ProviderKind { return api.ProviderAnthropic }

func (p *recordingProvider) Send(_ context.Context, req *api.Request) (<-chan *api.StreamEvent, <-chan error) {
	p.mu.Lock()
	n := len(p.requests)
	p.requests = append(p.requests, req)
	p.mu.Unlock()

	evs := p.turn(n, req)
	events := make(chan *api.StreamEvent, len(evs))
	for _, ev := range evs {
		events <- ev
	}
	errc := make(chan error)
	close(errc)
	var g <-chan struct{}
	if p.gate != nil {
		g = p.gate(n)
	}
	go func() {
		if g != nil {
			<-g
		}
		close(events)
	}()
	return events, errc
}

func (p *recordingProvider) requestCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.requests)
}

// requestContains reports whether request n carries a user text block
// containing substr.
func (p *recordingProvider) requestContains(n int, substr string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if n >= len(p.requests) {
		return false
	}
	for _, m := range p.requests[n].Messages {
		if m.Role != api.RoleUser {
			continue
		}
		for _, b := range m.Content {
			if b.Type == api.ContentTypeText && strings.Contains(b.Text, substr) {
				return true
			}
		}
	}
	return false
}

func (p *recordingProvider) anyRequestContains(substr string) bool {
	p.mu.Lock()
	n := len(p.requests)
	p.mu.Unlock()
	for i := 0; i < n; i++ {
		if p.requestContains(i, substr) {
			return true
		}
	}
	return false
}

// exploreCall fabricates one tool-only turn: a read tool with an input that
// VARIES every turn — the exact shape that evades the signature-based loop
// detectors. No prose, so the response-similarity detector sees nothing either.
func exploreCall(n int) []*api.StreamEvent {
	return turnEvents("", "read_file", fmt.Sprintf(`{"path":"internal/pkg%d/file%d.go"}`, n%7, n))
}

// newConvergenceTestApp builds a hermetic headless App: scripted provider,
// real registry with read/write tools, print mode, in-memory output.
func newConvergenceTestApp(t *testing.T, provider api.Provider, cfg *config.Config) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	renderer, err := NewRenderer("")
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	reg := tools.NewRegistry()
	for _, spec := range []*tools.ToolSpec{
		{Name: "read_file", Description: "read a file", RequiredMode: permission.ReadOnly},
		{Name: "grep_search", Description: "search file contents", RequiredMode: permission.ReadOnly},
		{Name: "write_file", Description: "write a file", RequiredMode: permission.WorkspaceWrite},
		{Name: "bash", Description: "run a command", RequiredMode: permission.DangerFullAccess},
	} {
		spec.InputSchema = json.RawMessage(`{"type":"object"}`)
		spec.AlwaysAvailable = true
		spec.Handler = func(context.Context, json.RawMessage) (string, error) { return "ok", nil }
		if err := reg.Register(spec); err != nil {
			t.Fatalf("register %s: %v", spec.Name, err)
		}
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app := &App{
		config:       cfg,
		provider:     provider,
		providerKind: "anthropic",
		session:      &session.Session{ID: "convergence-test", CreatedAt: time.Now(), Dir: dir},
		commands:     commands.NewRegistry(),
		renderer:     renderer,
		toolRegistry: reg,
		promptCtx:    &prompt.ProjectContext{WorkDir: dir},
		version:      "test",
		workDir:      dir,
		usageTracker: usage.NewTracker(),
		sessionStart: time.Now(),
		stdout:       stdout,
		stderr:       stderr,
	}
	app.SetPrintMode(true)
	return app, stdout, stderr
}

// The runaway: varied exploration forever. The policy must grace once, then
// stop non-success — well below the iteration backstop.
func TestConvergence_RunawayExplorationGracesThenStops(t *testing.T) {
	provider := &recordingProvider{
		turn: func(n int, _ *api.Request) []*api.StreamEvent { return exploreCall(n) },
	}
	app, stdout, stderr := newConvergenceTestApp(t, provider, &config.Config{
		Model:             "test-model",
		PermissionMode:    "ask",
		MaxToolIterations: 50,
		ConvergenceBudget: 3,
	})

	err := app.RunPrompt(context.Background(), "investigate the flaky retry logic")
	if err == nil {
		t.Fatal("runaway exploration returned nil — a stopped run must not read as success")
	}
	if !strings.Contains(err.Error(), "convergence") {
		t.Errorf("error = %v, want a convergence-policy stop", err)
	}

	// Budget 3 → three exploration turns, a fourth that triggers grace, and a
	// fifth that violates it. Five provider round-trips, not fifty.
	if got := provider.requestCount(); got != 5 {
		t.Errorf("provider requests = %d, want 5 (grace-then-stop), backstop was 50", got)
	}
	// The grace instruction reached the wire before the stop.
	if !provider.requestContains(4, "FINAL turn") {
		t.Errorf("final request did not carry the grace instruction")
	}
	// The truncation is stated on stdout, where a machine caller reads it.
	if !strings.Contains(stdout.String(), "[TRUNCATED:") {
		t.Errorf("stdout missing truncation notice: %q", stdout.String())
	}
	// And announced as chrome.
	if !strings.Contains(stderr.String(), "Convergence") {
		t.Errorf("stderr missing convergence chrome: %q", stderr.String())
	}
}

// A model that honors the grace instruction — answers in text — ends the run
// successfully. Grace is an off-ramp, not a punishment.
func TestConvergence_GraceHonoredEndsSuccessfully(t *testing.T) {
	const answer = "The retry logic drops the jitter seed; that is the bug."
	provider := &recordingProvider{}
	provider.turn = func(n int, req *api.Request) []*api.StreamEvent {
		if provider.requestContains(n, "FINAL turn") {
			return turnEvents(answer, "", "")
		}
		return exploreCall(n)
	}
	app, stdout, _ := newConvergenceTestApp(t, provider, &config.Config{
		Model:             "test-model",
		PermissionMode:    "ask",
		MaxToolIterations: 50,
		ConvergenceBudget: 3,
	})

	if err := app.RunPrompt(context.Background(), "investigate the flaky retry logic"); err != nil {
		t.Fatalf("RunPrompt: %v (a honored grace turn must end the run successfully)", err)
	}
	if !strings.Contains(stdout.String(), answer) {
		t.Errorf("stdout missing the final answer: %q", stdout.String())
	}
	if got := provider.requestCount(); got != 5 {
		t.Errorf("provider requests = %d, want 5", got)
	}
}

// The productive control: a long run whose exploration is regularly punctuated
// by writes never sees the grace instruction, even though its total turn count
// dwarfs the exploration budget.
func TestConvergence_ProgressResetsBudget(t *testing.T) {
	const totalToolTurns = 12
	provider := &recordingProvider{
		turn: func(n int, _ *api.Request) []*api.StreamEvent {
			if n == totalToolTurns {
				return turnEvents("All fixed and verified.", "", "")
			}
			if n%3 == 2 {
				return turnEvents("", "write_file", fmt.Sprintf(`{"path":"fix%d.go","content":"x"}`, n))
			}
			return exploreCall(n)
		},
	}
	app, _, _ := newConvergenceTestApp(t, provider, &config.Config{
		Model:             "test-model",
		PermissionMode:    "ask",
		MaxToolIterations: 50,
		ConvergenceBudget: 2, // tighter than any explore streak in the script
	})

	if err := app.RunPrompt(context.Background(), "fix the retry logic"); err != nil {
		t.Fatalf("RunPrompt: %v (a productive run must not be stopped)", err)
	}
	if got := provider.requestCount(); got != totalToolTurns+1 {
		t.Errorf("provider requests = %d, want %d", got, totalToolTurns+1)
	}
	if provider.anyRequestContains("FINAL turn") {
		t.Error("grace instruction was sent to a run that kept making progress")
	}
}

// Disabling the policy (negative budget) restores the plain iteration
// backstop: the run grinds to MaxToolIterations and fails there, exactly as
// before — proof the backstop behavior is preserved, not replaced.
func TestConvergence_DisabledFallsBackToIterationBackstop(t *testing.T) {
	provider := &recordingProvider{
		turn: func(n int, _ *api.Request) []*api.StreamEvent { return exploreCall(n) },
	}
	app, _, _ := newConvergenceTestApp(t, provider, &config.Config{
		Model:             "test-model",
		PermissionMode:    "ask",
		MaxToolIterations: 6,
		ConvergenceBudget: -1,
	})

	err := app.RunPrompt(context.Background(), "investigate")
	if err == nil || !strings.Contains(err.Error(), "stopped after 6 tool iterations") {
		t.Fatalf("err = %v, want the iteration-limit truncation", err)
	}
	if got := provider.requestCount(); got != 6 {
		t.Errorf("provider requests = %d, want 6", got)
	}
}

// Injected steering is consumed at the next turn boundary — the following
// provider request carries it — and each consumed line is acknowledged on the
// --events wire as steer.consumed. No mid-tool preemption is claimed: the
// in-flight turn finishes first.
func TestConvergence_SteeringConsumedAtTurnBoundary(t *testing.T) {
	const steerText = "stop exploring; focus only on the config loader"
	const answer = "STEERED: the config loader mis-merges overlays."

	turn0Gate := make(chan struct{})
	provider := &recordingProvider{}
	provider.turn = func(n int, req *api.Request) []*api.StreamEvent {
		if provider.requestContains(n, steerText) {
			return turnEvents(answer, "", "")
		}
		return exploreCall(n)
	}
	provider.gate = func(n int) <-chan struct{} {
		if n == 0 {
			return turn0Gate
		}
		return nil
	}

	app, stdout, _ := newConvergenceTestApp(t, provider, &config.Config{
		Model:             "test-model",
		PermissionMode:    "ask",
		MaxToolIterations: 50,
		ConvergenceBudget: 10,
	})
	eventsPath := filepath.Join(t.TempDir(), "events.ndjson")
	if err := app.SetEventsFile(eventsPath); err != nil {
		t.Fatalf("SetEventsFile: %v", err)
	}

	// Steering arrives over the same reader an orchestrator would write to:
	// the process's stdin.
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer pr.Close()

	done := make(chan error, 1)
	go func() { done <- app.RunPrompt(context.Background(), "investigate the retry logic") }()

	// Wait until turn 0 is verifiably in flight (its request is recorded and
	// its stream is gated open), THEN inject the steering — so consumption can
	// only happen at the boundary after that turn, never before it.
	deadline := time.Now().Add(5 * time.Second)
	for provider.requestCount() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("turn 0 never started")
		}
		time.Sleep(time.Millisecond)
	}
	app.StartSteerReader(pr)
	if _, err := pw.WriteString(steerText + "\n"); err != nil {
		t.Fatalf("write steer: %v", err)
	}
	pw.Close()
	for len(app.steerCh) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("steer line never reached the buffer")
		}
		time.Sleep(time.Millisecond)
	}
	close(turn0Gate)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunPrompt: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunPrompt did not finish")
	}

	// The turn AFTER the boundary saw the steering; the in-flight turn did not.
	if provider.requestContains(0, steerText) {
		t.Error("steering appeared in the in-flight request — that would be mid-turn preemption, which is not the contract")
	}
	if !provider.requestContains(1, steerText) {
		t.Error("steering was not consumed at the next turn boundary")
	}
	if !strings.Contains(stdout.String(), answer) {
		t.Errorf("run did not act on the steering; stdout = %q", stdout.String())
	}

	// The acknowledgement is on the wire.
	raw, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	var sawSteer bool
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var ev struct {
			Type string `json:"type"`
			Data struct {
				Text string `json:"text"`
				Turn int    `json:"turn"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("bad event line %q: %v", line, err)
		}
		if ev.Type == wireevents.SteerConsumed {
			sawSteer = true
			if ev.Data.Text != steerText {
				t.Errorf("steer.consumed text = %q, want %q", ev.Data.Text, steerText)
			}
			if ev.Data.Turn < 2 {
				t.Errorf("steer.consumed turn = %d, want the post-boundary turn (>= 2)", ev.Data.Turn)
			}
		}
	}
	if !sawSteer {
		t.Error("no steer.consumed event on the wire — the acknowledgement is the point")
	}
}

// Unit coverage for the per-call progress classifier.
func TestToolCallMakesProgress(t *testing.T) {
	reg := tools.NewRegistry()
	for _, spec := range []*tools.ToolSpec{
		{Name: "read_file", Description: "read", RequiredMode: permission.ReadOnly},
		{Name: "write_file", Description: "write", RequiredMode: permission.WorkspaceWrite},
	} {
		spec.InputSchema = json.RawMessage(`{"type":"object"}`)
		spec.Handler = func(context.Context, json.RawMessage) (string, error) { return "", nil }
		if err := reg.Register(spec); err != nil {
			t.Fatalf("register: %v", err)
		}
	}

	tests := []struct {
		name  string
		call  conversation.ToolCall
		want  bool
		about string
	}{
		{"read tool", conversation.ToolCall{Name: "read_file", Input: json.RawMessage(`{"path":"a.go"}`)}, false, "ReadOnly mode = exploration"},
		{"write tool", conversation.ToolCall{Name: "write_file", Input: json.RawMessage(`{"path":"a.go"}`)}, true, "WorkspaceWrite = progress"},
		{"unknown tool", conversation.ToolCall{Name: "mystery", Input: json.RawMessage(`{}`)}, true, "a guess must never terminate a run"},
		{"bash grep", conversation.ToolCall{Name: "bash", Input: json.RawMessage(`{"command":"grep -rn foo internal/"}`)}, false, "read-only pipeline = exploration"},
		{"bash test run", conversation.ToolCall{Name: "bash", Input: json.RawMessage(`{"command":"go test ./..."}`)}, true, "running the gate is progress"},
		{"bash write", conversation.ToolCall{Name: "bash", Input: json.RawMessage(`{"command":"git commit -m x"}`)}, true, "mutation is progress"},
		{"bash unparsable", conversation.ToolCall{Name: "bash", Input: json.RawMessage(`{`)}, true, "unparsable input must not terminate a run"},
	}
	for _, tt := range tests {
		if got := toolCallMakesProgress(reg, tt.call); got != tt.want {
			t.Errorf("%s: toolCallMakesProgress = %v, want %v (%s)", tt.name, got, tt.want, tt.about)
		}
	}
}
