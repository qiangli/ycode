package api

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// mockProvider implements Provider for testing.
type mockProvider struct {
	kind   ProviderKind
	sendFn func(ctx context.Context, req *Request) (<-chan *StreamEvent, <-chan error)
}

func (m *mockProvider) Send(ctx context.Context, req *Request) (<-chan *StreamEvent, <-chan error) {
	return m.sendFn(ctx, req)
}

func (m *mockProvider) Kind() ProviderKind {
	return m.kind
}

func newSuccessProvider(kind ProviderKind) *mockProvider {
	return &mockProvider{
		kind: kind,
		sendFn: func(ctx context.Context, req *Request) (<-chan *StreamEvent, <-chan error) {
			events := make(chan *StreamEvent, 1)
			events <- &StreamEvent{Type: "content_block_delta"}
			close(events)
			errCh := make(chan error)
			close(errCh)
			return events, errCh
		},
	}
}

func newFailingProvider(kind ProviderKind, errMsg string) *mockProvider {
	return &mockProvider{
		kind: kind,
		sendFn: func(ctx context.Context, req *Request) (<-chan *StreamEvent, <-chan error) {
			events := make(chan *StreamEvent)
			close(events)
			errCh := make(chan error, 1)
			errCh <- fmt.Errorf("provider error: %s", errMsg)
			close(errCh)
			return events, errCh
		},
	}
}

func TestFallbackProvider_PrimarySuccess(t *testing.T) {
	fp := &FallbackProvider{
		providers: []Provider{newSuccessProvider(ProviderAnthropic)},
		configs:   []ProviderConfig{{Kind: ProviderAnthropic}},
		cooldowns: make(map[int]time.Time),
		logger:    slog.Default(),
	}

	events, errCh := fp.Send(context.Background(), &Request{Model: "test"})
	if err := <-errCh; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var count int
	for range events {
		count++
	}
	if count != 1 {
		t.Errorf("expected 1 event, got %d", count)
	}
}

func TestFallbackProvider_StreamsMoreThanProviderBuffer(t *testing.T) {
	const want = 200
	provider := &mockProvider{
		kind: ProviderOpenAI,
		sendFn: func(context.Context, *Request) (<-chan *StreamEvent, <-chan error) {
			events := make(chan *StreamEvent, 64)
			errs := make(chan error, 1)
			go func() {
				defer close(events)
				defer close(errs)
				for i := 0; i < want; i++ {
					events <- &StreamEvent{Type: "content_block_delta"}
				}
			}()
			return events, errs
		},
	}
	fp := &FallbackProvider{
		providers: []Provider{provider},
		configs:   []ProviderConfig{{Kind: ProviderOpenAI}},
		cooldowns: make(map[int]time.Time),
		logger:    slog.Default(),
	}

	events, errCh := fp.Send(context.Background(), &Request{Model: "deepseek-v4-pro", Stream: true})
	var got int
	for range events {
		got++
	}
	if err := <-errCh; err != nil {
		t.Fatalf("unexpected stream error: %v", err)
	}
	if got != want {
		t.Fatalf("events = %d, want %d", got, want)
	}
}

func TestFallbackProvider_FallsBackOnTransient(t *testing.T) {
	fp := &FallbackProvider{
		providers: []Provider{
			newFailingProvider(ProviderAnthropic, "429 rate limit exceeded"),
			newSuccessProvider(ProviderOpenAI),
		},
		configs: []ProviderConfig{
			{Kind: ProviderAnthropic},
			{Kind: ProviderOpenAI},
		},
		cooldowns: make(map[int]time.Time),
		logger:    slog.Default(),
	}

	events, errCh := fp.Send(context.Background(), &Request{Model: "test"})
	if err := <-errCh; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var count int
	for range events {
		count++
	}
	if count != 1 {
		t.Errorf("expected 1 event from fallback, got %d", count)
	}
}

func TestFallbackProvider_ErrorAfterFirstEventDoesNotFallback(t *testing.T) {
	primary := &mockProvider{
		kind: ProviderOpenAI,
		sendFn: func(context.Context, *Request) (<-chan *StreamEvent, <-chan error) {
			events := make(chan *StreamEvent, 1)
			errs := make(chan error, 1)
			events <- &StreamEvent{Type: "content_block_delta"}
			close(events)
			errs <- fmt.Errorf("503 stream interrupted")
			close(errs)
			return events, errs
		},
	}
	var fallbackCalls atomic.Int32
	fallback := newSuccessProvider(ProviderOpenAI)
	originalFallbackSend := fallback.sendFn
	fallback.sendFn = func(ctx context.Context, req *Request) (<-chan *StreamEvent, <-chan error) {
		fallbackCalls.Add(1)
		return originalFallbackSend(ctx, req)
	}
	fp := &FallbackProvider{
		providers: []Provider{primary, fallback},
		configs: []ProviderConfig{
			{Kind: ProviderOpenAI},
			{Kind: ProviderOpenAI},
		},
		cooldowns: make(map[int]time.Time),
		logger:    slog.Default(),
	}

	events, errCh := fp.Send(context.Background(), &Request{Model: "deepseek-v4-pro", Stream: true})
	var count int
	for range events {
		count++
	}
	err := <-errCh
	if err == nil || !strings.Contains(err.Error(), "stream interrupted") {
		t.Fatalf("error = %v, want late stream error", err)
	}
	if count != 1 {
		t.Fatalf("events = %d, want the one primary event", count)
	}
	if got := fallbackCalls.Load(); got != 0 {
		t.Fatalf("fallback calls = %d, want 0 after partial response", got)
	}
}

func TestFallbackProvider_CancellationClosesPromptly(t *testing.T) {
	provider := &mockProvider{
		kind: ProviderOpenAI,
		sendFn: func(context.Context, *Request) (<-chan *StreamEvent, <-chan error) {
			return make(chan *StreamEvent), make(chan error)
		},
	}
	fp := &FallbackProvider{
		providers: []Provider{provider},
		configs:   []ProviderConfig{{Kind: ProviderOpenAI}},
		cooldowns: make(map[int]time.Time),
		logger:    slog.Default(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	events, errCh := fp.Send(ctx, &Request{Model: "deepseek-v4-pro", Stream: true})
	cancel()

	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("unexpected event after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("event channel did not close after cancellation")
	}
	select {
	case err, ok := <-errCh:
		if !ok || err != context.Canceled {
			t.Fatalf("error = %v, open = %v; want context.Canceled", err, ok)
		}
	case <-time.After(time.Second):
		t.Fatal("error channel did not close after cancellation")
	}
}

func TestFallbackProvider_NoFallbackOnNonTransient(t *testing.T) {
	fp := &FallbackProvider{
		providers: []Provider{
			newFailingProvider(ProviderAnthropic, "invalid api key"),
			newSuccessProvider(ProviderOpenAI),
		},
		configs: []ProviderConfig{
			{Kind: ProviderAnthropic},
			{Kind: ProviderOpenAI},
		},
		cooldowns: make(map[int]time.Time),
		logger:    slog.Default(),
	}

	_, errCh := fp.Send(context.Background(), &Request{Model: "test"})
	err := <-errCh
	if err == nil {
		t.Fatal("expected error for non-transient failure")
	}
}

// TestFallbackProvider_PermanentAuthFailsFastWithClearError is the regression
// test for the stale-DEEPSEEK_API_KEY incident: a 401 (invalid key — PERMANENT)
// was collapsed into "all providers in fallback chain failed or on cooldown",
// hiding which provider and which key failed, and burning the fallback chain
// on a key that could never work. A permanent auth failure must instead
// return IMMEDIATELY with an error naming the provider, the status, and a
// last-4 key fingerprint — and must NOT put the provider on cooldown.
func TestFallbackProvider_PermanentAuthFailsFastWithClearError(t *testing.T) {
	var fallbackTried bool
	fallback := &mockProvider{
		kind: ProviderOpenAI,
		sendFn: func(ctx context.Context, req *Request) (<-chan *StreamEvent, <-chan error) {
			fallbackTried = true
			return newSuccessProvider(ProviderOpenAI).sendFn(ctx, req)
		},
	}
	deepseek := &mockProvider{
		kind: ProviderOpenAI,
		sendFn: func(ctx context.Context, req *Request) (<-chan *StreamEvent, <-chan error) {
			events := make(chan *StreamEvent)
			close(events)
			errs := make(chan error, 1)
			errs <- &ClassifiedError{Reason: ReasonAuthPermanent, Action: ActionAbort, StatusCode: 401, Body: `{"error":{"message":"Invalid API key"}}`}
			close(errs)
			return events, errs
		},
	}
	fp := &FallbackProvider{
		providers: []Provider{deepseek, fallback},
		configs: []ProviderConfig{
			{Kind: ProviderOpenAI, DisplayName: "deepseek", APIKey: "sk-deepseek-stale-key-1c75"},
			{Kind: ProviderOpenAI, DisplayName: "openai", APIKey: "sk-openai-good-key-9f8e"},
		},
		cooldowns: make(map[int]time.Time),
		logger:    slog.Default(),
	}

	_, errCh := fp.Send(context.Background(), &Request{Model: "deepseek-v4-pro"})
	err := <-errCh
	if err == nil {
		t.Fatal("expected error for 401")
	}

	// The error must name the provider, the status, and the key fingerprint.
	if !strings.Contains(err.Error(), `provider "deepseek"`) {
		t.Errorf("error should name the provider, got: %v", err)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should include the 401 status, got: %v", err)
	}
	if !strings.Contains(err.Error(), "…1c75") {
		t.Errorf("error should include the last-4 key fingerprint, got: %v", err)
	}
	// And it must NEVER collapse into the opaque cooldown message.
	if strings.Contains(err.Error(), "all providers in fallback chain failed or on cooldown") {
		t.Errorf("401 must not collapse into the cooldown message, got: %v", err)
	}
	// The full key must never leak.
	if strings.Contains(err.Error(), "sk-deepseek-stale-key") {
		t.Errorf("error leaks key material: %v", err)
	}
	// Fail fast: the fallback provider must NOT be tried on a permanent error.
	if fallbackTried {
		t.Error("permanent auth failure must fail fast, not burn the fallback chain")
	}
	// No cooldown: a permanent failure is not cooldown-eligible.
	if fp.isOnCooldown(0) {
		t.Error("permanent auth failure must NOT put the provider on cooldown")
	}
	// The surfaced error is recognizably a permanent auth failure.
	if !IsPermanentAuthError(err) {
		t.Errorf("surfaced error should classify as permanent auth, got: %v", err)
	}
}

// TestFallbackProvider_ClassifiedRateLimitCoolsDownAndFallsBack guards the
// TRANSIENT path: a 429 still cools the provider down and rotates to the
// fallback, exactly as before.
func TestFallbackProvider_ClassifiedRateLimitCoolsDownAndFallsBack(t *testing.T) {
	fp := &FallbackProvider{
		providers: []Provider{
			&mockProvider{
				kind: ProviderAnthropic,
				sendFn: func(ctx context.Context, req *Request) (<-chan *StreamEvent, <-chan error) {
					events := make(chan *StreamEvent)
					close(events)
					errs := make(chan error, 1)
					errs <- &ClassifiedError{Reason: ReasonRateLimit, Action: ActionRetry, StatusCode: 429, Body: "rate limit exceeded"}
					close(errs)
					return events, errs
				},
			},
			newSuccessProvider(ProviderOpenAI),
		},
		configs: []ProviderConfig{
			{Kind: ProviderAnthropic},
			{Kind: ProviderOpenAI},
		},
		cooldowns: make(map[int]time.Time),
		logger:    slog.Default(),
	}

	events, errCh := fp.Send(context.Background(), &Request{Model: "test"})
	if err := <-errCh; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var count int
	for range events {
		count++
	}
	if count != 1 {
		t.Errorf("expected 1 event from fallback, got %d", count)
	}
	// Transient failure: the rate-limited provider goes on cooldown.
	if !fp.isOnCooldown(0) {
		t.Error("transient 429 should put the provider on cooldown")
	}
}

// TestFallbackProvider_ExhaustedChainReportsLastError: when every provider
// fails transiently, the collapse message must carry the underlying cause
// instead of a bare "failed or on cooldown".
func TestFallbackProvider_ExhaustedChainReportsLastError(t *testing.T) {
	fp := &FallbackProvider{
		providers: []Provider{
			newFailingProvider(ProviderAnthropic, "429 rate limit exceeded"),
		},
		configs: []ProviderConfig{
			{Kind: ProviderAnthropic, DisplayName: "anthropic"},
		},
		cooldowns: make(map[int]time.Time),
		logger:    slog.Default(),
	}

	_, errCh := fp.Send(context.Background(), &Request{Model: "test"})
	err := <-errCh
	if err == nil {
		t.Fatal("expected error when the chain is exhausted")
	}
	if !strings.Contains(err.Error(), "all providers in fallback chain failed or on cooldown") {
		t.Errorf("expected the collapse message prefix, got: %v", err)
	}
	if !strings.Contains(err.Error(), "429 rate limit exceeded") {
		t.Errorf("collapse message should carry the last provider error, got: %v", err)
	}
}

func TestFallbackProvider_ModelNotFoundRetriesWithFallbackModel(t *testing.T) {
	var models []string
	provider := &mockProvider{
		kind: ProviderOpenAI,
		sendFn: func(ctx context.Context, req *Request) (<-chan *StreamEvent, <-chan error) {
			models = append(models, req.Model)
			events := make(chan *StreamEvent, 1)
			errs := make(chan error, 1)
			// The untagged name and its :latest form both 404, so the chain walks
			// all the way to the configured known-good default.
			if req.Model == "missing-model" || req.Model == "missing-model:latest" {
				close(events)
				errs <- &ClassifiedError{Reason: ReasonModelNotFound, Action: ActionFallbackModel, StatusCode: 404, Body: "model not found"}
				close(errs)
				return events, errs
			}
			events <- &StreamEvent{Type: "content_block_delta"}
			close(events)
			close(errs)
			return events, errs
		},
	}
	fp := &FallbackProvider{
		providers:         []Provider{provider},
		configs:           []ProviderConfig{{Kind: ProviderOpenAI}},
		fallbackModelName: "gpt-4.1",
		cooldowns:         make(map[int]time.Time),
		logger:            slog.Default(),
	}

	events, errCh := fp.Send(context.Background(), &Request{Model: "missing-model"})
	if err := <-errCh; err != nil {
		t.Fatalf("expected fallback success, got %v", err)
	}
	for range events {
	}
	if got, want := fmt.Sprint(models), "[missing-model missing-model:latest gpt-4.1]"; got != want {
		t.Errorf("models tried = %s, want %s", got, want)
	}
}

func TestFallbackProvider_ModelNotFoundWithoutFallbackIsActionable(t *testing.T) {
	fp := &FallbackProvider{
		providers: []Provider{&mockProvider{
			kind: ProviderOpenAI,
			sendFn: func(context.Context, *Request) (<-chan *StreamEvent, <-chan error) {
				events := make(chan *StreamEvent)
				close(events)
				errs := make(chan error, 1)
				errs <- &ClassifiedError{Reason: ReasonModelNotFound, Action: ActionFallbackModel, StatusCode: 404, Body: "model not found"}
				close(errs)
				return events, errs
			},
		}},
		configs:   []ProviderConfig{{Kind: ProviderOpenAI}},
		cooldowns: make(map[int]time.Time),
		logger:    slog.Default(),
	}

	_, errCh := fp.Send(context.Background(), &Request{Model: "missing-model"})
	if err := <-errCh; err == nil || !strings.Contains(err.Error(), "no alternate model is configured") {
		t.Fatalf("expected actionable fallback error, got %v", err)
	}
}

func TestIsTransientError(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"429 rate limit exceeded", true},
		{"Rate limit reached", true},
		{"503 Service Unavailable", true},
		{"connection refused", true},
		{"request timeout", true},
		{"invalid api key", false},
		{"model not found", false},
	}
	for _, tt := range tests {
		got := isTransientError(fmt.Errorf("error: %s", tt.msg))
		if got != tt.want {
			t.Errorf("isTransientError(%q) = %v, want %v", tt.msg, got, tt.want)
		}
	}
}

func TestFallbackProvider_Kind(t *testing.T) {
	fp := &FallbackProvider{
		providers: []Provider{
			newSuccessProvider(ProviderAnthropic),
			newSuccessProvider(ProviderOpenAI),
		},
	}
	if fp.Kind() != ProviderAnthropic {
		t.Errorf("expected Anthropic, got %s", fp.Kind())
	}
}

// TestFallbackModelUntaggedTriesLatest: a ModelNotFound on an UNTAGGED model
// retries "<model>:latest" first (Ollama/cloudbox-pool tag convention), then the
// configured default — and the chain always terminates without mangling the
// default or looping.
func TestFallbackModelUntaggedTriesLatest(t *testing.T) {
	fp := &FallbackProvider{fallbackModelName: "gpt-4.1"}
	if got := fp.fallbackModel("gpt-oss-20b"); got != "gpt-oss-20b:latest" {
		t.Errorf("untagged request: got %q, want gpt-oss-20b:latest", got)
	}
	if got := fp.fallbackModel("gpt-oss-20b:latest"); got != "gpt-4.1" {
		t.Errorf("after :latest retry: got %q, want gpt-4.1 (configured default)", got)
	}
	// The default itself must return itself (no ":latest" mangling) so the caller's
	// fallback==requested check errors out instead of trying "gpt-4.1:latest".
	if got := fp.fallbackModel("gpt-4.1"); got != "gpt-4.1" {
		t.Errorf("default model: got %q, want gpt-4.1 unchanged", got)
	}
	// Empty configured default: untagged still tries :latest, tagged terminates.
	fp2 := &FallbackProvider{fallbackModelName: ""}
	if got := fp2.fallbackModel("gpt-oss-20b"); got != "gpt-oss-20b:latest" {
		t.Errorf("empty default, untagged: got %q, want gpt-oss-20b:latest", got)
	}
	if got := fp2.fallbackModel("qwen2.5-coder:32b"); got != "" {
		t.Errorf("empty default, tagged: got %q, want empty (terminates)", got)
	}
	if got := fp2.fallbackModel(""); got != "" {
		t.Errorf("empty request: got %q, want empty", got)
	}
}
