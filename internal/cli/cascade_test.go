package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/commands"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/prompt"
	"github.com/qiangli/ycode/internal/runtime/session"
	"github.com/qiangli/ycode/internal/runtime/usage"
	"github.com/qiangli/ycode/internal/tools"
)

// scriptedProvider is a Provider whose every Send replies with a canned turn
// and records the model each request asked for — the observable that proves
// (or disproves) an escalation actually changed what is on the wire.
type scriptedProvider struct {
	kind api.ProviderKind

	mu     sync.Mutex
	models []string // req.Model per request, in order
	turn   func(n int) []*api.StreamEvent
}

func (p *scriptedProvider) Send(_ context.Context, req *api.Request) (<-chan *api.StreamEvent, <-chan error) {
	p.mu.Lock()
	n := len(p.models)
	p.models = append(p.models, req.Model)
	p.mu.Unlock()

	evs := p.turn(n)
	events := make(chan *api.StreamEvent, len(evs))
	for _, ev := range evs {
		events <- ev
	}
	close(events)
	errc := make(chan error)
	close(errc)
	return events, errc
}

func (p *scriptedProvider) Kind() api.ProviderKind { return p.kind }

func (p *scriptedProvider) requestedModels() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.models...)
}

// turnEvents fabricates one streamed assistant turn: text, optionally one tool
// call, then the stop reason.
func turnEvents(text, toolName, toolInput string) []*api.StreamEvent {
	evs := []*api.StreamEvent{
		{Type: "message_start", Message: &api.Response{Usage: api.Usage{InputTokens: 100}}},
		{Type: "content_block_delta", Delta: json.RawMessage(fmt.Sprintf(`{"type":"text_delta","text":%q}`, text))},
	}
	stop := "end_turn"
	if toolName != "" {
		stop = "tool_use"
		evs = append(evs,
			&api.StreamEvent{Type: "content_block_start", ContentBlock: &api.ContentBlock{
				Type: api.ContentTypeToolUse, ID: "tc", Name: toolName, Input: json.RawMessage(toolInput),
			}},
			&api.StreamEvent{Type: "content_block_stop"},
		)
	}
	evs = append(evs, &api.StreamEvent{
		Type:  "message_delta",
		Usage: &api.Usage{OutputTokens: 20},
		Delta: json.RawMessage(`{"stop_reason":"` + stop + `"}`),
	})
	return evs
}

// loopingProvider replies with the SAME prose every turn plus a tool call whose
// input varies — the exact shape of a stuck model: the response loop detector
// fires, the tool loop detector does not.
func loopingProvider() *scriptedProvider {
	return &scriptedProvider{
		kind: api.ProviderOpenAI,
		turn: func(n int) []*api.StreamEvent {
			return turnEvents(
				"I will try the same approach again to fix this.",
				"note", fmt.Sprintf(`{"n":%d}`, n),
			)
		},
	}
}

// newCascadeTestApp builds an App wired for a cascade run: a looping base
// provider, a two-rung ladder, and a stdout buffer to observe the chrome.
func newCascadeTestApp(t *testing.T, base api.Provider, ladder []string) (*App, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	renderer, err := NewRenderer("")
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	reg := tools.NewRegistry()
	if err := reg.Register(&tools.ToolSpec{
		Name:        "note",
		Description: "records a note",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Handler: func(context.Context, json.RawMessage) (string, error) {
			return "ok", nil
		},
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}
	out := &bytes.Buffer{}
	return &App{
		config: &config.Config{
			Model:             ladder[0],
			CascadeModels:     ladder,
			PermissionMode:    "ask",
			MaxToolIterations: 8,
		},
		provider:     base,
		providerKind: "openai",
		session: &session.Session{
			ID:        "cascade-test",
			CreatedAt: time.Now(),
			Dir:       dir,
		},
		commands:     commands.NewRegistry(),
		renderer:     renderer,
		toolRegistry: reg,
		promptCtx:    &prompt.ProjectContext{WorkDir: dir},
		version:      "test",
		workDir:      dir,
		usageTracker: usage.NewTracker(),
		sessionStart: time.Now(),
		stdout:       out,
		stderr:       out,
	}, out
}

// TestCascadeEscalatesOnLoop is the Fix-2 gate: a run whose base model loops is
// OBSERVED to escalate — the premium provider receives requests carrying the
// premium model id, and the switch is announced.
func TestCascadeEscalatesOnLoop(t *testing.T) {
	base := loopingProvider()
	premium := &scriptedProvider{
		kind: api.ProviderAnthropic,
		turn: func(int) []*api.StreamEvent {
			return turnEvents("Stepping back: the actual fix is X. Done.", "", "")
		},
	}
	app, out := newCascadeTestApp(t, base, []string{"base-model", "premium-model"})
	app.providerFactory = func(model string) (api.Provider, string, error) {
		if model != "premium-model" {
			return nil, "", fmt.Errorf("unexpected rung %q", model)
		}
		return premium, "anthropic", nil
	}

	if err := app.RunPrompt(context.Background(), "investigate the flaky retry logic"); err != nil {
		t.Fatalf("RunPrompt: %v", err)
	}

	// The base model served the first turns — and ONLY the first turns.
	for i, m := range base.requestedModels() {
		if m != "base-model" {
			t.Errorf("base provider request %d asked for %q", i, m)
		}
	}
	// GATE: after the loop was detected, the wire carries the premium model.
	pm := premium.requestedModels()
	if len(pm) == 0 {
		t.Fatal("premium provider never received a request; the cascade did not escalate")
	}
	for i, m := range pm {
		if m != "premium-model" {
			t.Errorf("premium provider request %d asked for %q, want premium-model", i, m)
		}
	}
	if !app.escalator.Escalated() || app.escalator.Model() != "premium-model" {
		t.Errorf("escalator state: escalated=%v model=%q", app.escalator.Escalated(), app.escalator.Model())
	}
	if app.escalator.Reason() != "loop" {
		t.Errorf("escalation reason = %q, want loop", app.escalator.Reason())
	}
	// The switch is announced, not silent.
	if !strings.Contains(out.String(), "Cascade escalation: base-model → premium-model") {
		t.Errorf("escalation not announced in output:\n%s", out.String())
	}
	// And the premium turns are priced as the premium model.
	if got := app.resolvedModel(); got != "premium-model" {
		t.Errorf("resolvedModel = %q, want premium-model", got)
	}
}

// TestCascadeEscalationPersistsAcrossPrompts: an interactive session rebuilds
// its conversation runtime per prompt; the NEXT prompt after an escalation must
// stay on the premium tier instead of quietly reverting to the base model id on
// the premium provider.
func TestCascadeEscalationPersistsAcrossPrompts(t *testing.T) {
	base := loopingProvider()
	premium := &scriptedProvider{
		kind: api.ProviderAnthropic,
		turn: func(int) []*api.StreamEvent {
			return turnEvents("Handled on the premium tier.", "", "")
		},
	}
	app, _ := newCascadeTestApp(t, base, []string{"base-model", "premium-model"})
	app.providerFactory = func(string) (api.Provider, string, error) { return premium, "anthropic", nil }

	if err := app.RunPrompt(context.Background(), "investigate the flaky retry logic"); err != nil {
		t.Fatalf("first RunPrompt: %v", err)
	}
	before := len(premium.requestedModels())
	if before == 0 {
		t.Fatal("first prompt never escalated; cannot test persistence")
	}

	if err := app.RunPrompt(context.Background(), "now fix the config loader too"); err != nil {
		t.Fatalf("second RunPrompt: %v", err)
	}
	pm := premium.requestedModels()
	if len(pm) <= before {
		t.Fatal("second prompt sent nothing to the premium provider; escalation was dropped on runtime rebuild")
	}
	for _, m := range pm[before:] {
		if m != "premium-model" {
			t.Errorf("post-escalation prompt asked premium provider for %q, want premium-model", m)
		}
	}
}

// TestCascadeAllTiersUnavailable: escalation is warranted but no rung is
// reachable — the run must SAY so loudly and terminate the loop, not silently
// grind on the base model and report success.
func TestCascadeAllTiersUnavailable(t *testing.T) {
	base := loopingProvider()
	app, out := newCascadeTestApp(t, base, []string{"base-model", "premium-model"})
	app.providerFactory = func(model string) (api.Provider, string, error) {
		return nil, "", fmt.Errorf("no credentials for %s", model)
	}

	if err := app.RunPrompt(context.Background(), "investigate the flaky retry logic"); err != nil {
		t.Fatalf("RunPrompt: %v", err)
	}

	if app.escalator.Escalated() {
		t.Error("escalated despite every tier being unavailable")
	}
	text := out.String()
	if !strings.Contains(text, "no escalation is available") {
		t.Errorf("unavailable tiers not surfaced in output:\n%s", text)
	}
	// The hard loop-break still fires — the run ends rather than spinning.
	if !strings.Contains(text, "Breaking loop") {
		t.Errorf("stuck run did not terminate via loop break:\n%s", text)
	}
}
