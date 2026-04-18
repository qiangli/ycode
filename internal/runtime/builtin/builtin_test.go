package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/session"
)

// mockProvider implements api.Provider for testing.
type mockProvider struct {
	response string
	err      error
}

func (m *mockProvider) Kind() api.ProviderKind { return api.ProviderAnthropic }

func (m *mockProvider) Send(_ context.Context, _ *api.Request) (<-chan *api.StreamEvent, <-chan error) {
	events := make(chan *api.StreamEvent, 4)
	errc := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errc)

		if m.err != nil {
			errc <- m.err
			return
		}

		delta, _ := json.Marshal(struct {
			Text string `json:"text"`
		}{Text: m.response})
		events <- &api.StreamEvent{
			Type:  "content_block_delta",
			Delta: delta,
		}
	}()

	return events, errc
}

func TestModelChain_SingleShot_Success(t *testing.T) {
	provider := &mockProvider{response: "feat: add login endpoint"}
	chain := &ModelChain{
		Models: []session.ModelSpec{
			{Provider: provider, Model: "test-model"},
		},
	}

	got, err := chain.SingleShot(context.Background(), "system", "user content", 256)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "feat: add login endpoint" {
		t.Errorf("got %q, want %q", got, "feat: add login endpoint")
	}
}

func TestModelChain_SingleShot_Fallback(t *testing.T) {
	failing := &mockProvider{err: fmt.Errorf("rate limited")}
	working := &mockProvider{response: "fix: resolve null pointer"}
	chain := &ModelChain{
		Models: []session.ModelSpec{
			{Provider: failing, Model: "weak-model"},
			{Provider: working, Model: "main-model"},
		},
	}

	got, err := chain.SingleShot(context.Background(), "system", "user", 256)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "fix: resolve null pointer" {
		t.Errorf("got %q, want fallback response", got)
	}
}

func TestModelChain_SingleShot_AllFail(t *testing.T) {
	p1 := &mockProvider{err: fmt.Errorf("error 1")}
	p2 := &mockProvider{err: fmt.Errorf("error 2")}
	chain := &ModelChain{
		Models: []session.ModelSpec{
			{Provider: p1, Model: "model-1"},
			{Provider: p2, Model: "model-2"},
		},
	}

	_, err := chain.SingleShot(context.Background(), "system", "user", 256)
	if err == nil {
		t.Fatal("expected error when all models fail")
	}
}

func TestModelChain_SingleShot_EmptyResponse(t *testing.T) {
	provider := &mockProvider{response: ""}
	chain := &ModelChain{
		Models: []session.ModelSpec{
			{Provider: provider, Model: "test-model"},
		},
	}

	_, err := chain.SingleShot(context.Background(), "system", "user", 256)
	if err == nil {
		t.Fatal("expected error for empty response")
	}
}

func TestResolveModelChain_WithWeakModel(t *testing.T) {
	provider := &mockProvider{}
	cfg := &config.Config{
		Model:     "claude-sonnet-4-6-20250514",
		WeakModel: "claude-haiku-4-5-20251001",
	}

	chain := ResolveModelChain(cfg, provider)
	if len(chain.Models) != 2 {
		t.Fatalf("expected 2 models in chain, got %d", len(chain.Models))
	}
	if chain.Models[0].Model != "claude-haiku-4-5-20251001" {
		t.Errorf("first model should be weak model, got %q", chain.Models[0].Model)
	}
	if chain.Models[1].Model != "claude-sonnet-4-6-20250514" {
		t.Errorf("second model should be main model, got %q", chain.Models[1].Model)
	}
}

func TestResolveModelChain_WithoutWeakModel(t *testing.T) {
	provider := &mockProvider{}
	cfg := &config.Config{
		Model: "claude-sonnet-4-6-20250514",
	}

	chain := ResolveModelChain(cfg, provider)
	if len(chain.Models) != 1 {
		t.Fatalf("expected 1 model in chain, got %d", len(chain.Models))
	}
	if chain.Models[0].Model != "claude-sonnet-4-6-20250514" {
		t.Errorf("model should be main model, got %q", chain.Models[0].Model)
	}
}

func TestResolveModelChain_WithAlias(t *testing.T) {
	provider := &mockProvider{}
	cfg := &config.Config{
		Model:     "claude-sonnet-4-6-20250514",
		WeakModel: "haiku",
	}

	chain := ResolveModelChain(cfg, provider)
	if len(chain.Models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(chain.Models))
	}
	// "haiku" should resolve to full model ID.
	if chain.Models[0].Model != "claude-haiku-4-5-20251001" {
		t.Errorf("alias should resolve, got %q", chain.Models[0].Model)
	}
}
