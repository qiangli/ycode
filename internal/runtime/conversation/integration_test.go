package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/prompt"
	"github.com/qiangli/ycode/internal/runtime/routing"
	"github.com/qiangli/ycode/internal/tools"
)

// mockProvider implements api.Provider for testing conversation Turn().
type mockProvider struct {
	kind   api.ProviderKind
	sendFn func(ctx context.Context, req *api.Request) (<-chan *api.StreamEvent, <-chan error)
}

func (m *mockProvider) Send(ctx context.Context, req *api.Request) (<-chan *api.StreamEvent, <-chan error) {
	return m.sendFn(ctx, req)
}

func (m *mockProvider) Kind() api.ProviderKind { return m.kind }

// newTextProvider returns a mock provider that responds with a text message.
func newTextProvider(text string) *mockProvider {
	return &mockProvider{
		kind: api.ProviderAnthropic,
		sendFn: func(_ context.Context, _ *api.Request) (<-chan *api.StreamEvent, <-chan error) {
			events := make(chan *api.StreamEvent, 3)
			events <- &api.StreamEvent{Type: "message_start", Message: &api.Response{
				Content: []api.ContentBlock{{Type: api.ContentTypeText, Text: text}},
			}}
			events <- &api.StreamEvent{Type: "message_delta", Delta: json.RawMessage(`{"stop_reason":"end_turn"}`)}
			close(events)
			errCh := make(chan error)
			close(errCh)
			return events, errCh
		},
	}
}

// systemPromptCapture wraps a provider and captures the system prompt from requests.
type systemPromptCapture struct {
	inner         *mockProvider
	lastSystem    string
	lastToolNames []string
}

func newCaptureProvider(text string) *systemPromptCapture {
	inner := newTextProvider(text)
	cap := &systemPromptCapture{inner: inner}
	originalSend := inner.sendFn
	inner.sendFn = func(ctx context.Context, req *api.Request) (<-chan *api.StreamEvent, <-chan error) {
		cap.lastSystem = req.System
		cap.lastToolNames = nil
		for _, t := range req.Tools {
			cap.lastToolNames = append(cap.lastToolNames, t.Name)
		}
		return originalSend(ctx, req)
	}
	return cap
}

func newTestConversationRuntime(provider api.Provider, toolSpecs ...*tools.ToolSpec) *Runtime {
	reg := tools.NewRegistry()
	for _, spec := range toolSpecs {
		if spec.Handler == nil {
			s := *spec
			s.Handler = func(ctx context.Context, input json.RawMessage) (string, error) { return "ok", nil }
			spec = &s
		}
		if err := reg.Register(spec); err != nil {
			panic(fmt.Sprintf("register %s: %v", spec.Name, err))
		}
	}

	cfg := config.DefaultConfig()
	cfg.Model = "test-model"

	promptCtx := &prompt.ProjectContext{
		WorkDir:  "/tmp/test",
		Platform: "linux",
	}

	rt := NewRuntime(cfg, provider, nil, reg, promptCtx)
	return rt
}

// --- Integration tests ---

func TestIntegration_PreactivationActivatesTools(t *testing.T) {
	// Setup: register deferred git tools.
	cap := newCaptureProvider("I'll help you commit.")

	rt := newTestConversationRuntime(cap.inner,
		&tools.ToolSpec{Name: "bash", Description: "Execute bash", AlwaysAvailable: true},
		&tools.ToolSpec{Name: "git_status", Description: "Show working tree status"},
		&tools.ToolSpec{Name: "git_log", Description: "Show commit history"},
		&tools.ToolSpec{Name: "git_commit", Description: "Stage files and create a git commit"},
	)

	// Send a message mentioning "commit" — should pre-activate git tools.
	messages := []api.Message{
		{
			Role:    api.RoleUser,
			Content: []api.ContentBlock{{Type: api.ContentTypeText, Text: "please commit my changes"}},
		},
	}

	result, err := rt.Turn(context.Background(), messages)
	if err != nil {
		t.Fatalf("Turn failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Verify pre-activated tools are in the API request's tool list.
	hasGitCommit := false
	hasGitStatus := false
	for _, name := range cap.lastToolNames {
		if name == "git_commit" {
			hasGitCommit = true
		}
		if name == "git_status" {
			hasGitStatus = true
		}
	}
	if !hasGitCommit {
		t.Error("git_commit should be pre-activated and included in tool definitions")
	}
	if !hasGitStatus {
		t.Error("git_status should be pre-activated and included in tool definitions")
	}
}

func TestIntegration_DiagnosticsSectionInjectedWhenDegraded(t *testing.T) {
	// Setup: QualityMonitor with a degraded tool.
	qm := tools.NewQualityMonitor(0.7)
	qm.RecordCall("bash", true, 100)
	qm.RecordCall("bash", false, 200)
	qm.RecordCall("bash", false, 300)
	qm.RecordCall("bash", false, 400)
	// Success rate: 1/4 = 25% → below 70% threshold.

	cap := newCaptureProvider("Here's the result.")

	reg := tools.NewRegistry()
	reg.SetQualityMonitor(qm)
	reg.Register(&tools.ToolSpec{
		Name: "bash", Description: "Execute bash", AlwaysAvailable: true,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) { return "ok", nil },
	})

	cfg := config.DefaultConfig()
	cfg.Model = "test-model"
	promptCtx := &prompt.ProjectContext{WorkDir: "/tmp/test", Platform: "linux"}

	rt := NewRuntime(cfg, cap.inner, nil, reg, promptCtx)

	messages := []api.Message{
		{Role: api.RoleUser, Content: []api.ContentBlock{{Type: api.ContentTypeText, Text: "run a test"}}},
	}

	_, err := rt.Turn(context.Background(), messages)
	if err != nil {
		t.Fatalf("Turn failed: %v", err)
	}

	// Verify the system prompt contains diagnostics about the degraded tool.
	if !strings.Contains(cap.lastSystem, "Runtime diagnostics") {
		t.Error("system prompt should contain Runtime diagnostics section")
	}
	if !strings.Contains(cap.lastSystem, "bash") {
		t.Error("system prompt should mention degraded tool 'bash'")
	}
}

func TestIntegration_NoDiagnosticsWhenHealthy(t *testing.T) {
	// Setup: QualityMonitor with healthy tools only.
	qm := tools.NewQualityMonitor(0.7)
	for range 10 {
		qm.RecordCall("bash", true, 50)
	}

	cap := newCaptureProvider("Done.")

	reg := tools.NewRegistry()
	reg.SetQualityMonitor(qm)
	reg.Register(&tools.ToolSpec{
		Name: "bash", Description: "Execute bash", AlwaysAvailable: true,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) { return "ok", nil },
	})

	cfg := config.DefaultConfig()
	cfg.Model = "test-model"
	promptCtx := &prompt.ProjectContext{WorkDir: "/tmp/test", Platform: "linux"}

	rt := NewRuntime(cfg, cap.inner, nil, reg, promptCtx)

	messages := []api.Message{
		{Role: api.RoleUser, Content: []api.ContentBlock{{Type: api.ContentTypeText, Text: "hello"}}},
	}

	_, err := rt.Turn(context.Background(), messages)
	if err != nil {
		t.Fatalf("Turn failed: %v", err)
	}

	// Diagnostics should NOT appear when everything is healthy.
	if strings.Contains(cap.lastSystem, "Runtime diagnostics") {
		t.Error("system prompt should NOT contain diagnostics when all tools are healthy")
	}
}

func TestIntegration_ScoringPreactivatesToolsByDescription(t *testing.T) {
	cap := newCaptureProvider("Checking metrics.")

	rt := newTestConversationRuntime(cap.inner,
		&tools.ToolSpec{Name: "bash", Description: "Execute bash", AlwaysAvailable: true},
		&tools.ToolSpec{Name: "query_metrics", Description: "Query tool execution metrics for debugging performance issues and slow tool detection"},
	)

	// "query_metrics" should score high when message contains the tool name.
	messages := []api.Message{
		{Role: api.RoleUser, Content: []api.ContentBlock{{Type: api.ContentTypeText, Text: "check query_metrics for recent failures"}}},
	}

	_, err := rt.Turn(context.Background(), messages)
	if err != nil {
		t.Fatalf("Turn failed: %v", err)
	}

	hasQueryMetrics := false
	for _, name := range cap.lastToolNames {
		if name == "query_metrics" {
			hasQueryMetrics = true
		}
	}
	if !hasQueryMetrics {
		t.Error("query_metrics should be pre-activated by SearchTools scoring")
	}
}

func TestIntegration_RouterSetAndAccessible(t *testing.T) {
	provider := newTextProvider("ok")
	rt := newTestConversationRuntime(provider,
		&tools.ToolSpec{Name: "bash", Description: "Execute bash", AlwaysAvailable: true},
	)

	// Verify router can be set.
	router := routing.NewRouter(
		routing.WithLoadProvider(routing.StaticLoadProvider{Load: 1.0}),
	)
	rt.SetInferenceRouter(router)

	if rt.inferenceRouter == nil {
		t.Error("inference router should be set")
	}
}

func TestIntegration_TTLExpiresActivatedTools(t *testing.T) {
	cap := newCaptureProvider("done")

	rt := newTestConversationRuntime(cap.inner,
		&tools.ToolSpec{Name: "bash", Description: "Execute bash", AlwaysAvailable: true},
		&tools.ToolSpec{Name: "git_commit", Description: "Stage and commit"},
	)

	// Turn 1: pre-activate git_commit via keyword "commit".
	messages := []api.Message{
		{Role: api.RoleUser, Content: []api.ContentBlock{{Type: api.ContentTypeText, Text: "commit changes"}}},
	}
	rt.Turn(context.Background(), messages)

	if _, ok := rt.activatedTools["git_commit"]; !ok {
		t.Fatal("git_commit should be activated after turn 1")
	}

	// Simulate turns passing beyond TTL (8 turns).
	for i := 0; i < activatedToolTTL+1; i++ {
		genericMsg := []api.Message{
			{Role: api.RoleUser, Content: []api.ContentBlock{{Type: api.ContentTypeText, Text: "hello world foo bar"}}},
		}
		rt.Turn(context.Background(), genericMsg)
	}

	// git_commit should have expired.
	if _, ok := rt.activatedTools["git_commit"]; ok {
		t.Errorf("git_commit should have expired after %d turns without use", activatedToolTTL)
	}
}

func TestIntegration_PriorSessionSummaryClearedAfterFirstTurn(t *testing.T) {
	cap := newCaptureProvider("ok")

	rt := newTestConversationRuntime(cap.inner,
		&tools.ToolSpec{Name: "bash", Description: "Execute bash", AlwaysAvailable: true},
	)

	// Inject prior session summary.
	rt.promptCtx.Diagnostics = &prompt.DiagnosticsInfo{
		PriorSessionSummary: "Fixed auth middleware; 3 tests remaining.",
	}

	// Turn 1: should include prior session summary.
	messages := []api.Message{
		{Role: api.RoleUser, Content: []api.ContentBlock{{Type: api.ContentTypeText, Text: "continue"}}},
	}
	rt.Turn(context.Background(), messages)

	if !strings.Contains(cap.lastSystem, "Prior session context") {
		t.Error("turn 1 should include prior session summary in system prompt")
	}

	// Turn 2: should NOT include prior session summary.
	rt.Turn(context.Background(), messages)

	if strings.Contains(cap.lastSystem, "Prior session context") {
		t.Error("turn 2 should NOT include prior session summary")
	}
}

// Note: No init() to suppress logging — it would affect other tests in this package
// (e.g., TestRecordError_NilSession depends on the default logger).
// Integration tests use the default slog logger.
