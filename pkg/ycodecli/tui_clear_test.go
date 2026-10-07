//go:build !windows

package ycodecli

// Sprint: #387; Story: #1625; Story-ID: 27b3dfdf7e76

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	harnesscli "github.com/qiangli/ycode/internal/harness/cli"
	"github.com/qiangli/ycode/internal/harness/frontend"
	"github.com/qiangli/ycode/internal/harness/frontend/tui"
	"github.com/qiangli/ycode/internal/harness/spec"
	public "github.com/qiangli/ycode/pkg/ycode"
)

// clearFixture is instructionFixture with a second declared model on the
// route, so a session model selection is observable in provider requests.
func clearFixture(t *testing.T, dir, file, name string) string {
	t.Helper()
	path := instructionFixture(t, dir, file, name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, edit := range [][2]string{
		{"\n  models:\n", "\n  models:\n    secondary:\n      providerRef: openai\n      id: gpt-alt-387\n      limits: {contextTokens: 128000, maxOutputTokens: 16384}\n      capabilities: {streaming: true, toolCalls: true, parallelToolCalls: true, structuredOutput: true}\n"},
		{"      fallbackOn: [transport-exhausted,", "        - modelRef: secondary\n          transportRetry:\n            maxAttempts: 1\n            backoff: {initialMs: 250, multiplier: 2, maxMs: 2000, jitter: full}\n            retryOn: [transport]\n          timeoutMs: 120000\n      fallbackOn: [transport-exhausted,"},
	} {
		if strings.Count(text, edit[0]) != 1 {
			t.Fatalf("fixture edit anchor %q is not unique", edit[0])
		}
		text = strings.Replace(text, edit[0], edit[1], 1)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestTUIClearOverPTY is the A7 /clear acceptance: after a /config switch
// and a /model selection, /clear is refused during a turn and during a
// pending approval, then starts a distinct session whose next provider
// request carries none of the old conversation but keeps the configuration
// and model; the old session stays resumable in this and a fresh terminal.
func TestTUIClearOverPTY(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("BASHY_HOME", "")
	t.Setenv("OPENAI_API_KEY", "test-secret")
	workdir := t.TempDir()
	fixture := clearFixture(t, workdir, "agent.yaml", "genie-fixture")
	custom := clearFixture(t, workdir, "custom.yaml", "custom-fixture")

	run := startTUIOverPTY(t, workdir, home, fixture, "")
	run.expect("greeting", "Ctrl-D leaves.")
	run.line("/help")
	run.expect("/help lists /clear", "Start a clean conversation")

	run.line("/config " + custom)
	run.expect("/config switches", "configuration: "+custom)
	run.expect("the status line names the served configuration", "custom-fixture · gpt-5.6")
	match := greetingSession.FindStringSubmatch(run.screen.text())
	if match == nil {
		t.Fatalf("no session in the greeting:\n%s", run.screen.text())
	}
	old := match[1]
	run.line("/model secondary")
	run.expect("/model selects", "now uses secondary")
	run.line("remember CLEAR-OLD-387")
	run.expectCount("old turn end", "turn ended in", 1)

	// Refused, never cancelling, while a turn runs.
	run.line("please be slow")
	run.expect("slow turn running", "esc stops")
	run.line("/clear")
	run.expect("/clear refused mid-turn", "/clear refused during a turn")
	if _, err := io.WriteString(run.term, "\x1b"); err != nil {
		t.Fatal(err)
	}
	run.expect("ESC", "interrupted")

	// Refused while an approval waits; the approval is still answerable.
	run.line("approve-me please")
	run.expect("hitl prompt", "approve? y/n")
	run.line("/clear")
	run.expectCount("/clear refused during approval", "/clear refused during a turn", 2)
	if _, err := io.WriteString(run.term, "n"); err != nil {
		t.Fatal(err)
	}
	run.expectCount("rejected turn end", "turn ended in", 2)
	if _, err := os.Stat(filepath.Join(workdir, "hitl.txt")); err == nil {
		t.Fatal("rejected write ran")
	}

	run.line("/clear")
	run.expect("/clear starts a new session", "cleared: new session ")
	run.expect("/clear keeps the old session", "/resume "+old+" returns to it")
	run.expect("/clear keeps the model", "model secondary (gpt-alt-387)")
	ended := strings.Count(run.screen.text(), "turn ended in")
	run.line("what did I say?")
	run.expect("no old conversation and the kept model", "old-in-request=false model=gpt-alt-387")
	run.expectCount("cleared turn end", "turn ended in", ended+1)
	run.line("/config")
	run.expect("/clear keeps the configuration", "origin: /config in this terminal")
	run.expect("/clear keeps the configuration name", "name: custom-fixture")
	run.line("/model")
	run.expect("/model on the cleared session", "current: secondary (")
	run.line("/resume " + old)
	run.expect("old session resumes in this terminal", "› remember CLEAR-OLD-387")
	run.quit()

	app, err := openHarnessApplication(custom, public.WithHarnessProvider("openai", &tuiProvider{}))
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	sessions, err := app.harness.Sessions()
	if err != nil {
		t.Fatal(err)
	}
	var fresh string
	for _, s := range sessions {
		if s.ID != old {
			fresh = s.ID
		}
	}
	if fresh == "" || len(sessions) != 2 {
		t.Fatalf("sessions = %+v, want the old and one cleared session", sessions)
	}
	users := func(id string) string {
		entries, err := app.Transcript(id)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, e := range entries {
			if e.Role == "user" {
				out = append(out, e.Text)
			}
		}
		return strings.Join(out, " | ")
	}
	if got := users(old); !strings.Contains(got, "CLEAR-OLD-387") {
		t.Fatalf("old session lost its history: %q", got)
	}
	if got := users(fresh); strings.Contains(got, "CLEAR-OLD-387") || !strings.Contains(got, "what did I say?") {
		t.Fatalf("cleared session transcript = %q", got)
	}

	// A fresh terminal still resumes the cleared-from session on its
	// configuration: its history reaches the provider again.
	second := startTUIOverPTY(t, workdir, home, custom, "resume "+old)
	second.expect("fresh terminal resumes the old session", "session "+old)
	second.line("what did I say?")
	second.expect("the old conversation continues", "old-in-request=true model=gpt-alt-387")
	second.expectCount("resumed turn end", "turn ended in", 1)
	second.quit()
}

// TestClearRefusesLiveTurnAndCarriesModel exercises the declared `clear`
// below the terminal: the harness refuses it while a turn is live on the
// session (and the turn is not cancelled), and the CLI command moves the
// terminal pointer to a new session that keeps the model selection.
func TestClearRefusesLiveTurnAndCarriesModel(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-secret")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BASHY_HOME", "")
	workdir := t.TempDir()
	fixture := clearFixture(t, workdir, "agent.yaml", "clear-fixture")
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
	const session = "clear-live"
	if err := app.harness.SetSessionModel(session, "secondary"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	turnCtx, stop := context.WithCancel(ctx)
	stream, err := host.Turn(turnCtx, session, "please be slow")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for !app.harness.Busy() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := host.Slash(ctx, session, tui.Slash{Name: "/clear"}, nil); err == nil || !strings.Contains(err.Error(), "live on this session") {
		t.Fatalf("/clear during a live turn = %v, want a refusal", err)
	}
	if !app.harness.Busy() {
		t.Fatal("the refused /clear cancelled the live turn")
	}
	stop()
	for range stream {
	}
	app.harness.Settle(ctx, session)

	pointer := filepath.Join(t.TempDir(), "session")
	if err := os.WriteFile(pointer, []byte(session+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(SessionFileEnv, pointer)
	var out bytes.Buffer
	root, err := harnesscli.New(app.doc, dispatchCLI, harnesscli.Options{LookupEnv: os.LookupEnv})
	if err != nil {
		t.Fatal(err)
	}
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--file", fixture, "clear"})
	if err := root.ExecuteContext(ctx); err != nil {
		t.Fatalf("clear: %v\n%s", err, out.String())
	}
	next := strings.TrimSpace(out.String())
	if next == "" || next == session || pointedSession() != next {
		t.Fatalf("clear printed %q, pointer %q; want a new session", next, pointedSession())
	}
	reopened, err := openHarnessApplication(fixture, public.WithHarnessProvider("openai", &tuiProvider{}))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if ref, err := reopened.harness.SessionModelOverride(next); err != nil || ref != "secondary" {
		t.Fatalf("cleared session model = %q, %v; want secondary", ref, err)
	}
	if ref, err := reopened.harness.SessionModelOverride(session); err != nil || ref != "secondary" {
		t.Fatalf("old session model = %q, %v; want it untouched", ref, err)
	}
}
