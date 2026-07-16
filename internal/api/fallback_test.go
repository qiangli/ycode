package api

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
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
