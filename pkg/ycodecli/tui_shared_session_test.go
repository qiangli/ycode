package ycodecli

// Sprint: #387; Story: #27b3dfdf; Story-ID: 27b3dfdf7e76

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/api"
	harnesscli "github.com/qiangli/ycode/internal/harness/cli"
	"github.com/qiangli/ycode/internal/harness/frontend"
	"github.com/qiangli/ycode/internal/harness/frontend/tui"
	"github.com/qiangli/ycode/internal/harness/spec"
	public "github.com/qiangli/ycode/pkg/ycode"
)

// Same app: a turn submitted through the web (http) frontend is listed by the
// web selector and the terminal UI adapter maps the very same transcript.
func TestWebTurnVisibleToTUIAdapter(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-secret")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BASHY_HOME", "")
	workdir := t.TempDir()
	canonical, err := os.ReadFile(harnessFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(workdir, "agent.yaml")
	if err := os.WriteFile(fixture, canonical, 0o600); err != nil {
		t.Fatal(err)
	}
	app, err := openHarnessApplication(fixture, public.WithHarnessProvider("openai", &tuiProvider{}))
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const session = "shared-web-session"
	stream, err := app.Submit(ctx, frontend.Input{FrontendRef: "http", SessionID: session, Principal: "web-user", IdempotencyKey: "web-1", Body: []byte(`{"request":"hello from the web"}`)})
	if err != nil {
		t.Fatal(err)
	}
	for range stream {
	}

	var browser frontend.SessionBrowser = app
	sessions, err := browser.Sessions()
	if err != nil {
		t.Fatal(err)
	}
	listed := false
	for _, s := range sessions {
		listed = listed || s.ID == session
	}
	if !listed {
		t.Fatalf("web selector does not list %q: %+v", session, sessions)
	}
	web, err := browser.Transcript(session)
	if err != nil {
		t.Fatal(err)
	}
	terminal, err := (&tuiHost{app: app}).Transcript(session)
	if err != nil {
		t.Fatal(err)
	}
	if len(web) < 2 || len(web) != len(terminal) {
		t.Fatalf("web transcript %+v, terminal transcript %+v", web, terminal)
	}
	for i := range web {
		if web[i].Role != terminal[i].Role || web[i].Text != terminal[i].Text {
			t.Fatalf("entry %d differs: web %+v terminal %+v", i, web[i], terminal[i])
		}
	}
	if !strings.Contains(web[len(web)-2].Text, "hello from the web") || !strings.Contains(web[len(web)-1].Text, "stub-answer-") {
		t.Fatalf("transcript = %+v", web)
	}
}

// The required direction across a process-like boundary: a TUI turn, /save
// with a title, the app CLOSED; then a fresh app over the same durable config
// serves the web frontend, whose authenticated GET /sessions lists the titled
// session, GET /sessions?id= returns its transcript, and the next web turn
// resumes the same session ID.
func TestWebAndTUIShareSessions(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-secret")
	t.Setenv("YCODE_HTTP_TOKEN", "web-bearer")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BASHY_HOME", "")
	workdir := t.TempDir()
	canonical, err := os.ReadFile(harnessFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	// The web chat app is the http frontend with its built-in chat page; both
	// apps below open this one durable config.
	webConfig := strings.Replace(string(canonical), "      kind: http\n", "      kind: http\n      ui: chat\n", 1)
	if webConfig == string(canonical) {
		t.Fatal("fixture has no http frontend to serve the chat page")
	}
	fixture := filepath.Join(workdir, "agent.yaml")
	if err := os.WriteFile(fixture, []byte(webConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const session, title = "tui-then-web", "durable-title"

	// 1. TUI: one turn through the terminal frontend, then /save TITLE.
	app, err := openHarnessApplication(fixture, public.WithHarnessProvider("openai", &tuiProvider{}))
	if err != nil {
		t.Fatal(err)
	}
	view, err := frontend.NewTUI(app.doc, "tui", cliController{app, "coder"})
	if err != nil {
		app.Close()
		t.Fatal(err)
	}
	host := &tuiHost{app: app, view: view, config: fixture, principal: "tui-user",
		inv: harnesscli.Invocation{FrontendRef: "tui", Dispatch: spec.CLIDispatch{AgentRef: "coder", Input: &spec.CLIInput{PayloadKey: "request"}}}}
	stream, err := host.Turn(ctx, session, "hello from the terminal")
	if err != nil {
		app.Close()
		t.Fatal(err)
	}
	deltas := 0
	for item := range stream {
		if item.Type != "llm.delta" {
			continue
		}
		// Payload convention: the journaled delta names a payload_ref and
		// carries no raw provider text inline.
		var data map[string]any
		if err := json.Unmarshal(item.Data, &data); err != nil {
			t.Fatal(err)
		}
		ref, _ := data["payload_ref"].(string)
		if _, inline := data["text"]; inline || ref == "" {
			t.Fatalf("llm.delta data = %v, want payload_ref and no inline text", data)
		}
		if _, err := host.Payload(ref); err != nil {
			t.Fatalf("llm.delta payload %s: %v", ref, err)
		}
		deltas++
	}
	if deltas == 0 {
		t.Fatal("TUI turn journaled no llm.delta")
	}
	saved, err := host.Slash(ctx, session, tui.Slash{Name: "/save"}, []string{title})
	if err != nil {
		app.Close()
		t.Fatalf("/save %s: %v (%s)", title, err, saved.Output)
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}

	// 2. Fresh app, same durable config: the web frontend over HTTP.
	fresh, err := openHarnessApplication(fixture, public.WithHarnessProvider("openai", &tuiProvider{}))
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	network, err := frontend.NewNetwork(fresh.doc, "http", fresh, envAuthenticator{})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(network)
	defer server.Close()
	get := func(path, bearer string) *http.Response {
		t.Helper()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if bearer != "" {
			request.Header.Set("Authorization", "Bearer "+bearer)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { response.Body.Close() })
		return response
	}
	if response := get("/sessions", ""); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /sessions without bearer: %d", response.StatusCode)
	}
	var sessions []frontend.SessionInfo
	if response := get("/sessions", "web-bearer"); response.StatusCode != http.StatusOK {
		t.Fatalf("GET /sessions: %d", response.StatusCode)
	} else if err := json.NewDecoder(response.Body).Decode(&sessions); err != nil {
		t.Fatal(err)
	}
	listed := false
	for _, s := range sessions {
		listed = listed || (s.ID == session && s.Title == title)
	}
	if !listed {
		t.Fatalf("fresh web app does not list %q titled %q: %+v", session, title, sessions)
	}
	transcript := func() []frontend.TranscriptEntry {
		t.Helper()
		var entries []frontend.TranscriptEntry
		response := get("/sessions?id="+session, "web-bearer")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("GET /sessions?id=: %d", response.StatusCode)
		}
		if err := json.NewDecoder(response.Body).Decode(&entries); err != nil {
			t.Fatal(err)
		}
		return entries
	}
	before := transcript()
	n := len(before)
	if n < 2 || !strings.Contains(before[n-2].Text, "hello from the terminal") || !strings.Contains(before[n-1].Text, "stub-answer-") {
		t.Fatalf("web transcript of the TUI session = %+v", before)
	}

	// 3. The next web turn resumes the same session ID.
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/", strings.NewReader(`{"operation":"submit","session_id":"`+session+`","body":{"request":"hello again from the web"}}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer web-bearer")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var lines []map[string]any
	decoder := json.NewDecoder(response.Body)
	for decoder.More() {
		var line map[string]any
		if err := decoder.Decode(&line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || len(lines) == 0 {
		t.Fatalf("web turn: %d %v", response.StatusCode, lines)
	}
	for _, line := range lines {
		if id, ok := line["session_id"].(string); ok && id != session {
			t.Fatalf("web turn ran on session %q, want %q: %v", id, session, line)
		}
	}
	after := transcript()
	if len(after) != n+2 || !strings.Contains(after[n].Text, "hello again from the web") || !strings.Contains(after[n+1].Text, "stub-answer-") {
		t.Fatalf("resumed transcript = %+v", after)
	}
	for i := range before {
		if after[i] != before[i] {
			t.Fatalf("resumed transcript rewrote entry %d: %+v -> %+v", i, before[i], after[i])
		}
	}
}

// pausingProvider holds its first answer until released.
type pausingProvider struct {
	started, release chan struct{}
}

func (*pausingProvider) Kind() api.ProviderKind { return api.ProviderOpenAI }
func (p *pausingProvider) Send(ctx context.Context, request *api.Request) (<-chan *api.StreamEvent, <-chan error) {
	events, errs := make(chan *api.StreamEvent, 3), make(chan error, 1)
	go func() {
		defer close(events)
		defer close(errs)
		select {
		case p.started <- struct{}{}:
		default:
		}
		<-p.release
		text, _ := json.Marshal(map[string]string{"type": "text_delta", "text": "after-pause"})
		stop, _ := json.Marshal(map[string]string{"stop_reason": api.StopReasonEndTurn})
		events <- &api.StreamEvent{Type: "content_block_delta", Delta: text}
		events <- &api.StreamEvent{Type: "message_delta", Delta: stop}
	}()
	return events, errs
}

// /resume on a session whose live turn is cooperatively paused is Continue:
// the turn completes, and with nothing paused /resume is the session switch.
func TestTUIResumeContinuesLivePause(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-secret")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BASHY_HOME", "")
	workdir := t.TempDir()
	canonical, err := os.ReadFile(harnessFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(workdir, "agent.yaml")
	if err := os.WriteFile(fixture, canonical, 0o600); err != nil {
		t.Fatal(err)
	}
	backend := &pausingProvider{started: make(chan struct{}, 1), release: make(chan struct{})}
	app, err := openHarnessApplication(fixture, public.WithHarnessProvider("openai", backend))
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	view, err := frontend.NewTUI(app.doc, "tui", cliController{app, "coder"})
	if err != nil {
		t.Fatal(err)
	}
	host := &tuiHost{app: app, view: view, config: fixture, principal: "tui-user",
		inv: harnesscli.Invocation{FrontendRef: "tui", Dispatch: spec.CLIDispatch{AgentRef: "coder", Input: &spec.CLIInput{PayloadKey: "request"}}}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const session = "paused-session"
	resume := tui.Slash{Name: "/resume"}
	if result, _ := host.Slash(ctx, session, resume, nil); strings.Contains(result.Output, "continued") {
		t.Fatalf("/resume continued with no live turn: %+v", result)
	}
	stream, err := host.Turn(ctx, session, "pause me")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-backend.started:
	case <-ctx.Done():
		t.Fatal("provider never started")
	}
	paused := make(chan error, 1)
	go func() { paused <- app.harness.Pause(ctx, session) }()
	time.Sleep(200 * time.Millisecond) // the pause request precedes the stage end
	close(backend.release)
	if err := <-paused; err != nil {
		t.Fatal(err)
	}
	result, err := host.Slash(ctx, session, resume, nil)
	if err != nil || !strings.Contains(result.Output, "continued") || result.Session != "" {
		t.Fatalf("/resume on a paused turn = %+v, %v", result, err)
	}
	var types []string
	for item := range stream {
		types = append(types, item.Type)
	}
	joined := strings.Join(types, " ")
	if !strings.Contains(joined, "session.paused session.continued session.turn-committed") || !strings.Contains(joined, "output.emitted") {
		t.Fatalf("the continued turn did not complete: %v", types)
	}
	if result, _ := host.Slash(ctx, session, resume, nil); strings.Contains(result.Output, "continued") {
		t.Fatalf("/resume continued a finished turn: %+v", result)
	}
}

// The authored dispatch governs a slash, not its name: a document whose
// `plan` command declares the btw action makes /plan TEXT a transcript-
// isolated side query admitted under that command's route, never a mode
// switch through Harness.Plan.
func TestTUISlashFollowsAuthoredDispatch(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-secret")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BASHY_HOME", "")
	workdir := t.TempDir()
	canonical, err := os.ReadFile(harnessFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	const planned = `            short: Toggle planning mode or enter it with a request
            args: {min: 0, max: -1}`
	const authored = `            short: Ask aside (authored as btw)
            args: {min: 1, max: -1}`
	custom := strings.Replace(string(canonical), planned, authored, 1)
	custom = strings.Replace(custom, "dispatch: {operation: session, action: plan,", "dispatch: {operation: session, action: btw,", 1)
	if custom == string(canonical) || strings.Count(custom, "action: btw,") != 2 {
		t.Fatal("fixture no longer carries the canonical plan command")
	}
	fixture := filepath.Join(workdir, "agent.yaml")
	if err := os.WriteFile(fixture, []byte(custom), 0o600); err != nil {
		t.Fatal(err)
	}
	app, err := openHarnessApplication(fixture, public.WithHarnessProvider("openai", &tuiProvider{}))
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	view, err := frontend.NewTUI(app.doc, "tui", cliController{app, "coder"})
	if err != nil {
		t.Fatal(err)
	}
	host := &tuiHost{app: app, view: view, config: fixture, principal: "tui-user",
		inv: harnesscli.Invocation{FrontendRef: "tui", Dispatch: spec.CLIDispatch{AgentRef: "coder", Input: &spec.CLIInput{PayloadKey: "request"}}}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const session = "authored-session"
	plan := tui.Slash{Name: "/plan"}
	if _, err := host.Slash(ctx, session, plan, nil); err == nil || !strings.Contains(err.Error(), "arguments as declared") {
		t.Fatalf("/plan with no text bypassed the authored args: %v", err)
	}
	result, err := host.Slash(ctx, session, plan, []string{"what", "is", "this"})
	if err != nil || result.Start == nil {
		t.Fatalf("/plan TEXT = %+v, %v", result, err)
	}
	stream, err := result.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for item := range stream {
		types = append(types, item.Type)
		if item.Type == "session.mode-selected" {
			t.Fatalf("the authored btw command switched modes: %v", types)
		}
	}
	if mode, err := app.harness.SessionMode(session); err != nil || mode != "act" {
		t.Fatalf("mode after authored btw = %q, %v", mode, err)
	}
	if !strings.Contains(strings.Join(types, " "), "output.emitted") {
		t.Fatalf("the side query did not answer: %v", types)
	}
	if messages, _ := app.harness.Transcript(session); len(messages) != 0 {
		t.Fatalf("btw wrote the transcript: %d messages", len(messages))
	}
}
