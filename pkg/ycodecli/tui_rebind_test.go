//go:build !windows

package ycodecli

// Sprint: #387; Story: #1625; Story-ID: 27b3dfdf7e76

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	harnesscli "github.com/qiangli/ycode/internal/harness/cli"
	"github.com/qiangli/ycode/internal/harness/frontend"
	"github.com/qiangli/ycode/internal/harness/frontend/tui"
	public "github.com/qiangli/ycode/pkg/ycode"
)

// rebindHost is a terminal host on fixture, as runTerminalTUI builds it.
func rebindHost(t *testing.T, fixture string, provider *tuiProvider) *tuiHost {
	t.Helper()
	app, err := openHarnessApplication(fixture, public.WithHarnessProvider("openai", provider))
	if err != nil {
		t.Fatal(err)
	}
	root := *app.doc.Spec.Interfaces.CLI.Root.Dispatch
	inv := harnesscli.Invocation{Dispatch: root, FrontendRef: root.Input.TerminalFrontendRef, ConfigFile: fixture}
	view, err := frontend.NewTUI(app.doc, inv.FrontendRef, cliController{app, root.AgentRef})
	if err != nil {
		t.Fatal(err)
	}
	host := &tuiHost{app: app, view: view, inv: inv, config: fixture, principal: "tui-user"}
	host.queueRef, _ = steeringQueue(app, root.AgentRef, inv.FrontendRef)
	t.Cleanup(func() {
		for _, b := range append(host.prior, host.bound()) {
			_ = b.app.Close()
		}
	})
	return host
}

// answer runs one turn to its end and returns the streamed answer text.
func answer(t *testing.T, host *tuiHost, session, text string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	stream, err := host.Turn(ctx, session, text)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	var failure string
	for ev := range stream {
		switch ev.Type {
		case "output.emitted":
			var body struct {
				Deliveries []struct {
					PayloadRef string `json:"payload_ref"`
				} `json:"deliveries"`
			}
			_ = json.Unmarshal(ev.Data, &body)
			for _, d := range body.Deliveries {
				raw, _ := host.Payload(d.PayloadRef)
				out.Write(raw)
			}
		case "turn.failed":
			failure = string(ev.Data)
		}
	}
	host.Settle(ctx, session)
	if failure != "" {
		return out.String(), errors.New(failure)
	}
	return out.String(), nil
}

// TestResumeAcrossConfigNeverMixesHistory is the review blocker: after
// /config B, /resume of a session recorded under A returns it to A's
// compiled configuration in this terminal (the provider sees its history
// on A's model), while a terminal that only ever served B refuses it with
// guidance and no provider request is made under B.
func TestResumeAcrossConfigNeverMixesHistory(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-secret")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BASHY_HOME", "")
	dir := t.TempDir()
	configA := clearFixture(t, dir, "a.yaml", "config-a")
	configB := clearFixture(t, dir, "b.yaml", "config-b")
	t.Chdir(dir)
	ctx := context.Background()

	provider := &tuiProvider{}
	host := rebindHost(t, configA, provider)
	digestA := host.bound().app.doc.ConfigDigest
	const old = "resume-old-a"
	if _, err := answer(t, host, old, "remember CLEAR-OLD-387"); err != nil {
		t.Fatal(err)
	}
	switched, err := host.Slash(ctx, old, tui.Slash{Name: "/config"}, []string{configB})
	if err != nil {
		t.Fatalf("/config B: %v\n%s", err, switched.Output)
	}
	if switched.Session == "" || switched.Session == old || !switched.Fresh {
		t.Fatalf("/config B kept the session bound to A: %+v", switched)
	}
	if strings.Contains(switched.Output, "keeps its configuration and stays resumable") {
		t.Fatalf("the old unconditional promise is back: %s", switched.Output)
	}
	if host.bound().app.doc.ConfigDigest == digestA {
		t.Fatal("/config B did not change the served configuration")
	}

	resumed, err := host.Slash(ctx, switched.Session, tui.Slash{Name: "/resume"}, []string{old})
	if err != nil {
		t.Fatalf("/resume old: %v", err)
	}
	if resumed.Session != old || !strings.Contains(resumed.Output, "restored for session "+old) {
		t.Fatalf("/resume old = %+v; want the session back on its configuration", resumed)
	}
	if got := host.bound().app.doc.ConfigDigest; got != digestA {
		t.Fatalf("served digest after /resume = %s, want A %s", got, digestA)
	}
	if got, err := answer(t, host, old, "what did I say?"); err != nil || !strings.Contains(got, "old-in-request=true") {
		t.Fatalf("resumed turn on A = %q, %v; want A's history in the provider request", got, err)
	}

	// A terminal that never served A cannot restore it: refused, with
	// guidance, and nothing reaches the provider under B.
	other := &tuiProvider{}
	onlyB := rebindHost(t, configB, other)
	_, err = onlyB.Slash(ctx, "fresh-b", tui.Slash{Name: "/resume"}, []string{old})
	if err == nil || !strings.Contains(err.Error(), "/config FILE") || !errors.Is(err, public.ErrSessionConfig) {
		t.Fatalf("/resume of an A session under B = %v; want a guided ErrSessionConfig refusal", err)
	}
	if _, err := answer(t, onlyB, old, "what did I say?"); !errors.Is(err, public.ErrSessionConfig) {
		t.Fatalf("a turn on an A session under B = %v; want ErrSessionConfig", err)
	}
	if n := other.calls.Load(); n != 0 {
		t.Fatalf("provider under B was called %d times with A's history", n)
	}
}

// TestRebindSameFileRecomputesRoute: an edited configuration reloaded from
// the same path brings its own terminal route, not the one compiled before.
func TestRebindSameFileRecomputesRoute(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-secret")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BASHY_HOME", "")
	dir := t.TempDir()
	config := clearFixture(t, dir, "agent.yaml", "route-fixture")
	t.Chdir(dir)
	host := rebindHost(t, config, &tuiProvider{})
	if got := host.bound().inv.FrontendRef; got != "tui" {
		t.Fatalf("initial terminal frontend = %q", got)
	}
	raw, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, edit := range [][3]string{
		{"terminalFrontendRef: tui,", "terminalFrontendRef: tui-alt,", "1"},
		{"tui, http,", "tui, tui-alt, http,", "2"},
		{"    tui:\n      kind: tui\n", "    tui-alt:\n      kind: tui\n      trust: local-user\n      hitl: true\n      limits: {maxInputBytes: 1048576}\n    tui:\n      kind: tui\n", "1"},
	} {
		if n := strings.Count(text, edit[0]); n == 0 || (edit[2] == "1" && n != 1) {
			t.Fatalf("fixture anchor %q appears %d times", edit[0], n)
		}
		text = strings.ReplaceAll(text, edit[0], edit[1])
	}
	if err := os.WriteFile(config, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := host.Slash(context.Background(), "route-session", tui.Slash{Name: "/config"}, []string{config})
	if err != nil {
		t.Fatalf("/config same file: %v\n%s", err, out.Output)
	}
	if got := host.bound().inv.FrontendRef; got != "tui-alt" {
		t.Fatalf("terminal frontend after reloading the edited file = %q, want tui-alt", got)
	}
	if !out.Rebound {
		t.Fatalf("same-session reload not reported as a rebind: %+v", out)
	}
}

// TestClearHonorsAuthoredRequiredFlag: /clear goes through the same
// required-flag guard as every other declared command shortcut.
func TestClearHonorsAuthoredRequiredFlag(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-secret")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BASHY_HOME", "")
	dir := t.TempDir()
	config := clearFixture(t, dir, "agent.yaml", "flag-fixture")
	t.Chdir(dir)
	raw, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "scope: local}], dispatch: {operation: session, action: clear}"
	text := string(raw)
	if strings.Count(text, anchor) != 1 {
		t.Fatalf("fixture has %d clear commands", strings.Count(text, anchor))
	}
	text = strings.Replace(text, anchor, "scope: local}, {name: reason, type: string, default: \"\", required: true, usage: Why, scope: local}], dispatch: {operation: session, action: clear}", 1)
	if err := os.WriteFile(config, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	host := rebindHost(t, config, &tuiProvider{})
	result, err := host.Slash(context.Background(), "flag-session", tui.Slash{Name: "/clear"}, nil)
	if err == nil || !strings.Contains(err.Error(), "requires --reason") {
		t.Fatalf("/clear with an authored required flag = %+v, %v; want a refusal", result, err)
	}
	if result.Session != "" {
		t.Fatalf("refused /clear still moved to session %s", result.Session)
	}
}

// TestResumeBindsSelectionOnlySessions: a session whose only events are a
// /model selection or a /plan toggle is bound to the configuration that
// recorded them. After /config B, /resume returns it to A (not accepted
// under B, where its next turn would fail the model/mode digest check), and
// the next turn runs.
func TestResumeBindsSelectionOnlySessions(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-secret")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BASHY_HOME", "")
	for _, c := range []struct {
		name  string
		slash tui.Slash
		args  []string
	}{
		{"model", tui.Slash{Name: "/model"}, []string{"secondary"}},
		{"plan", tui.Slash{Name: "/plan"}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			configA := clearFixture(t, dir, "a.yaml", "config-a")
			configB := clearFixture(t, dir, "b.yaml", "config-b")
			t.Chdir(dir)
			ctx := context.Background()
			host := rebindHost(t, configA, &tuiProvider{})
			digestA := host.bound().app.doc.ConfigDigest
			old := "selection-only-" + c.name
			if out, err := host.Slash(ctx, old, c.slash, c.args); err != nil {
				t.Fatalf("%s: %v\n%s", c.slash.Name, err, out.Output)
			}
			switched, err := host.Slash(ctx, old, tui.Slash{Name: "/config"}, []string{configB})
			if err != nil {
				t.Fatalf("/config B: %v", err)
			}
			if err := host.bound().app.harness.CheckSessionConfig(old); !errors.Is(err, public.ErrSessionConfig) {
				t.Fatalf("B accepts a session whose %s selection was made under A: %v", c.name, err)
			}
			resumed, err := host.Slash(ctx, switched.Session, tui.Slash{Name: "/resume"}, []string{old})
			if err != nil {
				t.Fatalf("/resume: %v", err)
			}
			if resumed.Session != old {
				t.Fatalf("/resume = %+v", resumed)
			}
			if got := host.bound().app.doc.ConfigDigest; got != digestA {
				t.Fatalf("served digest after /resume = %s, want A %s", got, digestA)
			}
			if _, err := answer(t, host, old, "next turn"); err != nil {
				t.Fatalf("next turn after /resume: %v", err)
			}
		})
	}
}

// TestRestoredBindingDispatchesDeclaredActionsOnIt: after the file on disk
// changes (here: rewritten in place) and /resume restores the binding
// compiled from the original, declared actions answer from that bound
// document — /config reports its digest — and an action that would reopen
// the file fails closed instead of silently running the new policy.
func TestRestoredBindingDispatchesDeclaredActionsOnIt(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-secret")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BASHY_HOME", "")
	dir := t.TempDir()
	config := clearFixture(t, dir, "agent.yaml", "config-a")
	t.Chdir(dir)
	ctx := context.Background()
	host := rebindHost(t, config, &tuiProvider{})
	digestA := host.bound().app.doc.ConfigDigest
	const old = "bound-old"
	if _, err := answer(t, host, old, "remember CLEAR-OLD-387"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(config)
	if err != nil || !strings.Contains(string(raw), "config-a") {
		t.Fatalf("fixture name anchor missing: %v", err)
	}
	if err := os.WriteFile(config, []byte(strings.Replace(string(raw), "config-a", "config-b-edited", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	switched, err := host.Slash(ctx, old, tui.Slash{Name: "/config"}, []string{config})
	if err != nil {
		t.Fatalf("/config same file: %v", err)
	}
	digestB := host.bound().app.doc.ConfigDigest
	if digestB == digestA {
		t.Fatal("the edit did not change the configuration digest")
	}
	if _, err := host.Slash(ctx, switched.Session, tui.Slash{Name: "/resume"}, []string{old}); err != nil {
		t.Fatalf("/resume old: %v", err)
	}
	b := host.bound()
	if b.app.doc.ConfigDigest != digestA {
		t.Fatalf("restored digest = %s, want A %s", b.app.doc.ConfigDigest, digestA)
	}
	source, err := host.Slash(ctx, old, tui.Slash{Name: "/config"}, nil)
	if err != nil {
		t.Fatalf("/config: %v", err)
	}
	if !strings.Contains(source.Output, "digest: "+digestA) || strings.Contains(source.Output, digestB) {
		t.Fatalf("/config after restoring A reports:\n%s\nwant digest %s, never %s", source.Output, digestA, digestB)
	}
	if got, err := answer(t, host, old, "what did I say?"); err != nil || !strings.Contains(got, "old-in-request=true") {
		t.Fatalf("turn on restored A = %q, %v", got, err)
	}
	for _, op := range []string{"acp", "shell", "serve"} {
		inv := b.inv
		inv.Dispatch.Operation = op
		inv.Command = []string{"ycode", op}
		err := b.dispatch(ctx, inv, harnesscli.IO{Out: &strings.Builder{}, Err: &strings.Builder{}})
		if err == nil || !strings.Contains(err.Error(), "not the bound "+digestA) {
			t.Fatalf("%s under restored A with B on disk = %v; want a fail-closed refusal", op, err)
		}
	}
}

// TestConfigSlashLiteralPaths: /config FILE selects a path with spaces
// (what the TUI parses from a quoted FILE) as one literal file, with
// $(...) and backticks never evaluated, and an ordinary path as before.
func TestConfigSlashLiteralPaths(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-secret")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BASHY_HOME", "")
	dir := t.TempDir()
	configA := clearFixture(t, dir, "a.yaml", "config-a")
	spaced := filepath.Join(dir, "my configs")
	if err := os.Mkdir(spaced, 0o700); err != nil {
		t.Fatal(err)
	}
	configB := clearFixture(t, spaced, "b $(touch pwned) `touch pwned`.yaml", "config-b")
	t.Chdir(dir)
	ctx := context.Background()
	host := rebindHost(t, configA, &tuiProvider{})

	for _, step := range []struct{ arg, want string }{
		{"my configs/b $(touch pwned) `touch pwned`.yaml", configB},
		{"a.yaml", configA},
	} {
		out, err := host.Slash(ctx, "path-session", tui.Slash{Name: "/config"}, []string{step.arg})
		if err != nil {
			t.Fatalf("/config %q: %v\n%s", step.arg, err, out.Output)
		}
		if got := host.bound().config; got != step.want {
			t.Fatalf("/config %q serves %q, want %q", step.arg, got, step.want)
		}
	}
	for _, d := range []string{dir, spaced} {
		if _, err := os.Stat(filepath.Join(d, "pwned")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("a /config path was evaluated: %s/pwned exists (%v)", d, err)
		}
	}
}
