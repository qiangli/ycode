package ycodecli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/api"
	public "github.com/qiangli/ycode/pkg/ycode"
	"github.com/qiangli/yoke/pkg/llmbudget"
)

func TestTextAgentYcodeAndGenie(t *testing.T) {
	for _, name := range []string{"agent.yaml", "genie/agent.yaml"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("BASHY_HOME", t.TempDir())
			meter := filepath.Join(t.TempDir(), "meter.json")
			defer llmbudget.SetDefault(llmbudget.New(llmbudget.Config{StatePath: meter}))()
			path, _ := filepath.Abs(filepath.Join("..", "..", "examples", name))
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateTextAgent(path, data); err != nil {
				t.Fatal(err)
			}
			provider := &applicationProvider{}
			agent, err := OpenTextAgent(path, data, public.WithHarnessProvider("openai", provider))
			if err != nil {
				t.Fatal(err)
			}
			defer agent.Close()
			for i := 0; i < 2; i++ {
				answer, err := agent.Run(context.Background(), "recorded request")
				if err != nil || !strings.Contains(answer, "configured-output") {
					t.Fatalf("answer=%q err=%v", answer, err)
				}
			}
			if provider.calls.Load() != 2 {
				t.Fatalf("calls=%d", provider.calls.Load())
			}
			if err := agent.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := agent.Run(context.Background(), "after close"); err == nil {
				t.Fatal("closed session accepted run")
			}
			state, err := os.ReadFile(meter)
			if err != nil {
				t.Fatal(err)
			}
			var accounting llmbudget.State
			if err := json.Unmarshal(state, &accounting); err != nil {
				t.Fatal(err)
			}
			var tokens int64
			for _, model := range accounting.Models {
				tokens += model.DayTokens
			}
			if tokens == 0 {
				t.Fatal("missing usage accounting")
			}
			for _, invalid := range []string{string(data) + "\n---\nkind: Harness\n", strings.Replace(string(data), "spec:", "unknownPolicy: true\nspec:", 1)} {
				if ValidateTextAgent(path, []byte(invalid)) == nil {
					t.Fatal("accepted invalid YAML")
				}
			}
		})
	}
}

type receiptProvider struct{ calls int }

func (*receiptProvider) Kind() api.ProviderKind { return api.ProviderOpenAI }
func (p *receiptProvider) Send(context.Context, *api.Request) (<-chan *api.StreamEvent, <-chan error) {
	p.calls++
	events := make(chan *api.StreamEvent, 1)
	events <- &api.StreamEvent{Type: "usage", Usage: &api.Usage{InputTokens: 17, OutputTokens: 9}}
	close(events)
	errs := make(chan error)
	close(errs)
	return events, errs
}

func TestTextAgentBudgetReceiptAndHardCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meter.json")
	defer llmbudget.SetDefault(llmbudget.New(llmbudget.Config{StatePath: path, Models: map[string]llmbudget.Model{"fixture": {Name: "fixture", Kind: "api", Provider: "fixture", CostMicro: 1000}}}))()
	provider := &receiptProvider{}
	backend := agentFenceMeterProvider("fixture", provider)
	events, errs := backend.Send(context.Background(), &api.Request{Model: "fixture"})
	for range events {
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state llmbudget.State
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	if got := state.Models["fixture"]; got.DayTokens != 26 || got.DayCostUSD <= 0 {
		t.Fatalf("meter=%+v", got)
	}
	limit := int64(1)
	restore := llmbudget.SetDefault(llmbudget.New(llmbudget.Config{StatePath: filepath.Join(t.TempDir(), "meter.json"), Policy: &llmbudget.Policy{Version: 1,
		Bindings:    []llmbudget.Binding{{Model: "fixture", Provider: "fixture", Account: "fixture", Pool: "fixture", Lane: llmbudget.LaneAPIKey}},
		Constraints: []llmbudget.Constraint{{Provider: "fixture", DailySpendMicroUSD: &limit}},
	}}))
	defer restore()
	events, errs = backend.Send(context.Background(), &api.Request{Model: "fixture"})
	for range events {
	}
	var denied bool
	for err := range errs {
		denied = denied || err != nil
	}
	if !denied || provider.calls != 1 {
		t.Fatalf("denied=%v calls=%d", denied, provider.calls)
	}
}

type textAgentBlockingProvider struct {
	started chan struct{}
	stopped chan struct{}
}

func (*textAgentBlockingProvider) Kind() api.ProviderKind { return api.ProviderOpenAI }
func (p *textAgentBlockingProvider) Send(ctx context.Context, _ *api.Request) (<-chan *api.StreamEvent, <-chan error) {
	events := make(chan *api.StreamEvent)
	errs := make(chan error, 1)
	close(p.started)
	go func() { defer close(events); defer close(errs); <-ctx.Done(); errs <- ctx.Err(); close(p.stopped) }()
	return events, errs
}

func TestTextAgentCloseCancelsAndSettles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BASHY_HOME", t.TempDir())
	defer llmbudget.SetDefault(llmbudget.New(llmbudget.Config{StatePath: filepath.Join(t.TempDir(), "meter.json")}))()
	path, _ := filepath.Abs(filepath.Join("..", "..", "examples", "agent.yaml"))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	p := &textAgentBlockingProvider{started: make(chan struct{}), stopped: make(chan struct{})}
	a, err := OpenTextAgent(path, raw, public.WithHarnessProvider("openai", p))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	done := make(chan error, 1)
	go func() { _, err := a.Run(context.Background(), "wait"); done <- err }()
	select {
	case <-p.started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not start")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancellation lost")
		}
	case <-time.After(time.Second):
		t.Fatal("run leaked")
	}
	select {
	case <-p.stopped:
	case <-time.After(time.Second):
		t.Fatal("transport leaked")
	}
	if !a.app.harness.Settle(context.Background(), a.session) {
		t.Fatal("session not settled")
	}
}

type textAgentSourceProvider struct {
	applicationProvider
	system string
}

func (p *textAgentSourceProvider) Send(ctx context.Context, request *api.Request) (<-chan *api.StreamEvent, <-chan error) {
	p.system = request.System
	return p.applicationProvider.Send(ctx, request)
}

func TestTextAgentFreezesSourceAtPreparation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BASHY_HOME", t.TempDir())
	defer llmbudget.SetDefault(llmbudget.New(llmbudget.Config{StatePath: filepath.Join(t.TempDir(), "meter.json")}))()
	raw, err := os.ReadFile(filepath.Join("..", "..", "examples", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(path, []byte("FROZEN-POLICY-SOURCE"), 0600); err != nil {
		t.Fatal(err)
	}
	open, err := PrepareTextAgent(filepath.Join(dir, "agent.yaml"), raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("MUTATED-POLICY-SOURCE"), 0600); err != nil {
		t.Fatal(err)
	}
	p := &textAgentSourceProvider{}
	a, err := open(public.WithHarnessProvider("openai", p))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err := a.Run(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.system, "FROZEN-POLICY-SOURCE") || strings.Contains(p.system, "MUTATED-POLICY-SOURCE") {
		t.Fatalf("compiled source changed: %q", p.system)
	}
}
