package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/conversation"
	"github.com/qiangli/ycode/internal/runtime/session"
	"github.com/qiangli/ycode/internal/runtime/taskqueue"
	"github.com/qiangli/ycode/internal/runtime/usage"
	"github.com/qiangli/ycode/internal/service"
)

// stubExtractProvider is a no-network api.Provider that emits a single
// content_block_delta carrying the canned JSON. Used to verify wire
// routing for /api/extract without an actual LLM.
type stubExtractProvider struct {
	kind    api.ProviderKind
	emitted string
	lastReq *api.Request
}

func (p *stubExtractProvider) Kind() api.ProviderKind { return p.kind }

func (p *stubExtractProvider) Send(_ context.Context, req *api.Request) (<-chan *api.StreamEvent, <-chan error) {
	p.lastReq = req
	events := make(chan *api.StreamEvent, 4)
	errc := make(chan error, 1)
	go func() {
		defer close(events)
		defer close(errc)
		delta, _ := json.Marshal(map[string]string{"type": "text_delta", "text": p.emitted})
		events <- &api.StreamEvent{Type: "content_block_delta", Delta: delta}
		events <- &api.StreamEvent{Type: "message_stop"}
	}()
	return events, errc
}

// fakeAppForExtract is a minimal AppBackend that exposes a stub provider
// and a fixed config — enough for the /api/extract and /api/embed paths.
type fakeAppForExtract struct {
	provider api.Provider
	cfg      *config.Config
}

func (a *fakeAppForExtract) Session() *session.Session                      { return &session.Session{ID: "fx"} }
func (a *fakeAppForExtract) SessionID() string                              { return "fx" }
func (a *fakeAppForExtract) SessionMessages() []api.Message                 { return nil }
func (a *fakeAppForExtract) MessageCount() int                              { return 0 }
func (a *fakeAppForExtract) Config() *config.Config                         { return a.cfg }
func (a *fakeAppForExtract) Model() string                                  { return a.cfg.Model }
func (a *fakeAppForExtract) Provider() api.Provider                         { return a.provider }
func (a *fakeAppForExtract) ProviderKind() string                           { return "stub" }
func (a *fakeAppForExtract) Version() string                                { return "test" }
func (a *fakeAppForExtract) WorkDir() string                                { return "/tmp/x" }
func (a *fakeAppForExtract) InPlanMode() bool                               { return false }
func (a *fakeAppForExtract) SwitchModel(string) (string, error)             { return "", nil }
func (a *fakeAppForExtract) UsageTracker() *usage.Tracker                   { return usage.NewTracker() }
func (a *fakeAppForExtract) NextTurnIndex() int                             { return 0 }
func (a *fakeAppForExtract) HasCommand(string) bool                         { return false }
func (a *fakeAppForExtract) SetProgressFunc(func(string))                   {}
func (a *fakeAppForExtract) SetDeltaFunc(func(string))                      {}
func (a *fakeAppForExtract) SetUsageFunc(func(int, int, int, int))          {}
func (a *fakeAppForExtract) SetAgentEventFunc(func(string, map[string]any)) {}
func (a *fakeAppForExtract) InstallRemotePermissionPrompter(service.PermissionRequester) {
}
func (a *fakeAppForExtract) Close() error { return nil }
func (a *fakeAppForExtract) ExecuteCommand(_ context.Context, _, _ string) (string, error) {
	return "", nil
}
func (a *fakeAppForExtract) ConversationRuntime() *conversation.Runtime { return nil }
func (a *fakeAppForExtract) RunTurnWithRecovery(_ context.Context, _ []api.Message) (*conversation.TurnResult, *conversation.RecoveryResult, error) {
	return nil, nil, nil
}
func (a *fakeAppForExtract) ExecuteTools(_ context.Context, _ []conversation.ToolCall, _ chan<- taskqueue.TaskEvent) []api.ContentBlock {
	return nil
}

// mockServiceWithApp extends mockService with a configurable AppBackend
// so /api/extract and /api/embed can resolve a per-workDir App.
type mockServiceWithApp struct {
	*mockService
	app service.AppBackend
}

func (m *mockServiceWithApp) LookupApp(_ context.Context, _ string) (service.AppBackend, error) {
	return m.app, nil
}

func newExtractTestServer(t *testing.T, provider api.Provider) *httptest.Server {
	t.Helper()
	memBus := bus.NewMemoryBus()
	t.Cleanup(func() { memBus.Close() })
	app := &fakeAppForExtract{provider: provider, cfg: &config.Config{Model: "test-model", MaxTokens: 1024}}
	svc := &mockServiceWithApp{
		mockService: &mockService{b: memBus},
		app:         app,
	}
	srv := New(Config{}, svc)
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)
	return ts
}

func TestHandleExtract_OK(t *testing.T) {
	stub := &stubExtractProvider{kind: api.ProviderOpenAI, emitted: `{"email":"a@b.c"}`}
	ts := newExtractTestServer(t, stub)

	body := extractRequest{
		Prompt: "find email",
		Schema: json.RawMessage(`{"type":"object","properties":{"email":{"type":"string"}}}`),
	}
	buf, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", ts.URL+"/api/extract", bytes.NewReader(buf))
	req.Header.Set("X-Work-Dir", "/tmp/x")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var got map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["email"] != "a@b.c" {
		t.Errorf("email = %q, want a@b.c", got["email"])
	}
	// Verify the request reached the provider with the schema and the default model.
	if stub.lastReq == nil {
		t.Fatal("provider never received a request")
	}
	if stub.lastReq.Model != "test-model" {
		t.Errorf("model = %q, want test-model", stub.lastReq.Model)
	}
	if stub.lastReq.ResponseFormat == nil || stub.lastReq.ResponseFormat.Type != "json_schema" {
		t.Errorf("expected json_schema response format, got %+v", stub.lastReq.ResponseFormat)
	}
}

func TestHandleExtract_RequiresWorkDir(t *testing.T) {
	ts := newExtractTestServer(t, &stubExtractProvider{kind: api.ProviderOpenAI, emitted: "{}"})

	body := extractRequest{Prompt: "x"}
	buf, _ := json.Marshal(body)
	resp, err := http.Post(ts.URL+"/api/extract", "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("missing work_dir: got %d, want 400", resp.StatusCode)
	}
}

func TestHandleExtract_RequiresPrompt(t *testing.T) {
	ts := newExtractTestServer(t, &stubExtractProvider{kind: api.ProviderOpenAI, emitted: "{}"})

	req, _ := http.NewRequest("POST", ts.URL+"/api/extract", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("X-Work-Dir", "/tmp/x")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("missing prompt: got %d, want 400", resp.StatusCode)
	}
}

func TestHandleExtract_OverrideModel(t *testing.T) {
	stub := &stubExtractProvider{kind: api.ProviderOpenAI, emitted: `{"ok":true}`}
	ts := newExtractTestServer(t, stub)

	body := extractRequest{Prompt: "x", Model: "claude-haiku-4-5-20251001"}
	buf, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", ts.URL+"/api/extract", bytes.NewReader(buf))
	req.Header.Set("X-Work-Dir", "/tmp/x")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d, want 200", resp.StatusCode)
	}
	if stub.lastReq.Model != "claude-haiku-4-5-20251001" {
		t.Errorf("model override ignored: got %q", stub.lastReq.Model)
	}
}
