package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

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

// mockThinkingProvider simulates a reasoning model that only returns thinking content.
type mockThinkingProvider struct {
	thinking string
}

func (m *mockThinkingProvider) Kind() api.ProviderKind { return api.ProviderOpenAI }

func (m *mockThinkingProvider) Send(_ context.Context, _ *api.Request) (<-chan *api.StreamEvent, <-chan error) {
	events := make(chan *api.StreamEvent, 4)
	errc := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errc)

		delta, _ := json.Marshal(struct {
			Type     string `json:"type"`
			Thinking string `json:"thinking"`
		}{Type: "thinking_delta", Thinking: m.thinking})
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

// TestSingleShot_ThinkingFallback verifies that reasoning model output
// (thinking deltas only, no text) is handled by extracting the commit message.
func TestSingleShot_ThinkingFallback(t *testing.T) {
	provider := &mockThinkingProvider{
		thinking: "The user wants a commit message.\n\nfeat(builtin): add thinking fallback for reasoning models",
	}
	chain := &ModelChain{
		Models: []session.ModelSpec{
			{Provider: provider, Model: "reasoning-model"},
		},
	}

	got, err := chain.SingleShot(context.Background(), "system", "user content", 256)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "feat(builtin): add thinking fallback for reasoning models" {
		t.Errorf("got %q, want extracted commit message", got)
	}
}

func TestExtractFromThinking(t *testing.T) {
	tests := []struct {
		name     string
		thinking string
		want     string
	}{
		{
			"message at end",
			"Analysis of changes...\n\nfeat: add login endpoint",
			"feat: add login endpoint",
		},
		{
			"message with backticks",
			"The commit message should be:\n`fix(api): handle nil response`",
			"fix(api): handle nil response",
		},
		{
			"message with quotes",
			"I'll generate:\n\"refactor: extract commit logic\"",
			"refactor: extract commit logic",
		},
		{
			"message in middle",
			"Looking at diffs...\nfeat: add endpoint\nThis covers the changes.",
			"feat: add endpoint",
		},
		{
			"no conventional commit",
			"This is just thinking with no commit message.",
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFromThinking(tt.thinking)
			if got != tt.want {
				t.Errorf("extractFromThinking() = %q, want %q", got, tt.want)
			}
		})
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

// slowProvider simulates an LLM that takes a long time to respond.
// It respects context cancellation so tests remain fast.
type slowProvider struct {
	delay    time.Duration
	response string
}

func (s *slowProvider) Kind() api.ProviderKind { return api.ProviderAnthropic }

func (s *slowProvider) Send(ctx context.Context, _ *api.Request) (<-chan *api.StreamEvent, <-chan error) {
	events := make(chan *api.StreamEvent, 4)
	errc := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errc)

		select {
		case <-time.After(s.delay):
			// Delay elapsed — send response.
		case <-ctx.Done():
			errc <- ctx.Err()
			return
		}

		delta, _ := json.Marshal(struct {
			Text string `json:"text"`
		}{Text: s.response})
		events <- &api.StreamEvent{
			Type:  "content_block_delta",
			Delta: delta,
		}
	}()

	return events, errc
}

// TestModelChain_TimeoutIsChainWide verifies that the timeout is shared across
// all models in the chain, not applied per-model. This is a contract test for
// a recurring bug where per-model timeouts caused total stall time to multiply
// by the number of models in the chain.
func TestModelChain_TimeoutIsChainWide(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout contract test in short mode")
	}

	// Two slow providers: each takes 3s to respond.
	// With a 2s chain-wide timeout, the entire call should fail in ~2s.
	// If timeouts were per-model, it would take ~4s (2s × 2 models).
	p1 := &slowProvider{delay: 3 * time.Second, response: "slow1"}
	p2 := &slowProvider{delay: 3 * time.Second, response: "slow2"}

	chain := &ModelChain{
		Models: []session.ModelSpec{
			{Provider: p1, Model: "slow-model-1"},
			{Provider: p2, Model: "slow-model-2"},
		},
	}

	timeout := 2 * time.Second

	start := time.Now()
	_, err := chain.SingleShotWithUsageAndTimeout(
		context.Background(), "system", "user", 256, timeout,
	)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	// The total time must be bounded by the timeout (with generous margin
	// for scheduling jitter), NOT timeout × number of models.
	maxAllowed := timeout + 500*time.Millisecond
	if elapsed > maxAllowed {
		t.Errorf("chain took %v, want ≤ %v (timeout %v × 2 models = %v would indicate per-model timeout bug)",
			elapsed.Round(time.Millisecond), maxAllowed, timeout, 2*timeout)
	}
}

// TestModelChain_StreamingTimeoutIsChainWide is the streaming variant of the
// chain-wide timeout contract test.
func TestModelChain_StreamingTimeoutIsChainWide(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout contract test in short mode")
	}

	p1 := &slowProvider{delay: 3 * time.Second, response: "slow1"}
	p2 := &slowProvider{delay: 3 * time.Second, response: "slow2"}

	chain := &ModelChain{
		Models: []session.ModelSpec{
			{Provider: p1, Model: "slow-model-1"},
			{Provider: p2, Model: "slow-model-2"},
		},
	}

	timeout := 2 * time.Second

	start := time.Now()
	_, err := chain.SingleShotStreamingWithTimeout(
		context.Background(), "system", "user", 256, timeout, nil, nil,
	)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	maxAllowed := timeout + 500*time.Millisecond
	if elapsed > maxAllowed {
		t.Errorf("streaming chain took %v, want ≤ %v", elapsed.Round(time.Millisecond), maxAllowed)
	}
}

// TestModelChain_FastFallbackWithinTimeout verifies that if the first model
// fails fast, the second model still gets the remaining timeout budget.
func TestModelChain_FastFallbackWithinTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout contract test in short mode")
	}

	// First model fails immediately, second responds after 500ms.
	failing := &mockProvider{err: fmt.Errorf("rate limited")}
	slow := &slowProvider{delay: 500 * time.Millisecond, response: "fallback success"}

	chain := &ModelChain{
		Models: []session.ModelSpec{
			{Provider: failing, Model: "broken-model"},
			{Provider: slow, Model: "fallback-model"},
		},
	}

	result, err := chain.SingleShotWithUsageAndTimeout(
		context.Background(), "system", "user", 256, 5*time.Second,
	)
	if err != nil {
		t.Fatalf("expected fallback to succeed, got: %v", err)
	}
	if result.Text != "fallback success" {
		t.Errorf("got %q, want %q", result.Text, "fallback success")
	}
}
