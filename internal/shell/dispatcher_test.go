package shell

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/commands"
)

// fakeSkillResolver implements SkillResolver for tests.
type fakeSkillResolver struct {
	byName map[string]string
	byPath map[string]string
	err    error
}

func (f *fakeSkillResolver) Resolve(name string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	v, ok := f.byName[name]
	if !ok {
		return "", errors.New("not found: " + name)
	}
	return v, nil
}

func (f *fakeSkillResolver) ResolvePath(p string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	v, ok := f.byPath[p]
	if !ok {
		return "", errors.New("not found at path: " + p)
	}
	return v, nil
}

func (f *fakeSkillResolver) List() []string {
	out := make([]string, 0, len(f.byName))
	for k := range f.byName {
		out = append(out, k)
	}
	return out
}

func newTestRuntime(t *testing.T, opts Options) *ShellRuntime {
	t.Helper()
	if opts.WorkDir == "" {
		opts.WorkDir = t.TempDir()
	}
	rt, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return rt
}

func newSink() (*bytes.Buffer, *bytes.Buffer, WriterSink) {
	var out, errBuf bytes.Buffer
	return &out, &errBuf, WriterSink{StdoutW: &out, StderrW: &errBuf}
}

func TestDispatcher_Bash(t *testing.T) {
	rt := newTestRuntime(t, Options{})
	d := NewDispatcher(rt)
	out, _, sink := newSink()

	intent, err := Classify("echo hello")
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	res, err := d.Dispatch(context.Background(), intent, sink)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0", res.ExitCode)
	}
	if got := strings.TrimSpace(out.String()); got != "hello" {
		t.Fatalf("stdout = %q, want %q", got, "hello")
	}
}

func TestDispatcher_BashEnvPersistsAcrossCalls(t *testing.T) {
	rt := newTestRuntime(t, Options{})
	d := NewDispatcher(rt)

	// First call: set FOO.
	{
		_, _, sink := newSink()
		intent, _ := Classify("FOO=bar")
		if _, err := d.Dispatch(context.Background(), intent, sink); err != nil {
			t.Fatalf("set: %v", err)
		}
	}
	// Second call: read FOO.
	out, _, sink := newSink()
	intent, _ := Classify("echo $FOO")
	if _, err := d.Dispatch(context.Background(), intent, sink); err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "bar" {
		t.Fatalf("$FOO = %q, want %q (proves dispatcher uses persistent runner)", got, "bar")
	}
}

func TestDispatcher_SlashShellSafe(t *testing.T) {
	registry := commands.NewRegistry()
	registry.Register(&commands.Spec{
		Name:      "help",
		ShellSafe: true,
		Handler:   func(_ context.Context, args string) (string, error) { return "help body: " + args, nil },
	})
	rt := newTestRuntime(t, Options{Registry: registry})
	d := NewDispatcher(rt)

	out, _, sink := newSink()
	intent, _ := Classify("/help foo")
	res, err := d.Dispatch(context.Background(), intent, sink)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d", res.ExitCode)
	}
	if !strings.Contains(out.String(), "help body: foo") {
		t.Fatalf("stdout = %q, expected to contain 'help body: foo'", out.String())
	}
}

func TestDispatcher_SlashNotInShellSafeSet(t *testing.T) {
	registry := commands.NewRegistry()
	registry.Register(&commands.Spec{
		Name:      "deploy", // ShellSafe defaults to false
		ShellSafe: false,
		Handler:   func(_ context.Context, _ string) (string, error) { return "deployed", nil },
	})
	rt := newTestRuntime(t, Options{Registry: registry})
	d := NewDispatcher(rt)

	_, errBuf, sink := newSink()
	intent, _ := Classify("/deploy")
	res, err := d.Dispatch(context.Background(), intent, sink)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if res.ExitCode == 0 {
		t.Fatalf("expected non-zero exit, got 0")
	}
	if !strings.Contains(errBuf.String(), "not available in shell mode") {
		t.Fatalf("stderr = %q, expected 'not available in shell mode' message", errBuf.String())
	}
}

func TestDispatcher_SlashNoRegistry(t *testing.T) {
	rt := newTestRuntime(t, Options{}) // no Registry
	d := NewDispatcher(rt)

	_, errBuf, sink := newSink()
	intent, _ := Classify("/help")
	res, err := d.Dispatch(context.Background(), intent, sink)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if res.ExitCode == 0 {
		t.Fatalf("expected non-zero exit when no registry configured")
	}
	if !strings.Contains(errBuf.String(), "no slash registry configured") {
		t.Fatalf("stderr = %q, expected 'no slash registry configured'", errBuf.String())
	}
}

func TestDispatcher_SkillByName(t *testing.T) {
	skills := &fakeSkillResolver{byName: map[string]string{"review": "# review skill body"}}
	rt := newTestRuntime(t, Options{Skills: skills})
	d := NewDispatcher(rt)

	out, _, sink := newSink()
	intent, _ := Classify("@review")
	res, err := d.Dispatch(context.Background(), intent, sink)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d", res.ExitCode)
	}
	if !strings.Contains(out.String(), "review skill body") {
		t.Fatalf("stdout = %q, expected skill body", out.String())
	}
}

func TestDispatcher_SkillByPath(t *testing.T) {
	skills := &fakeSkillResolver{byPath: map[string]string{"./skills/foo": "# foo body"}}
	rt := newTestRuntime(t, Options{Skills: skills})
	d := NewDispatcher(rt)

	out, _, sink := newSink()
	intent, _ := Classify("@./skills/foo")
	res, err := d.Dispatch(context.Background(), intent, sink)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d", res.ExitCode)
	}
	if !strings.Contains(out.String(), "foo body") {
		t.Fatalf("stdout = %q, expected foo body", out.String())
	}
}

func TestDispatcher_PipeToSkill(t *testing.T) {
	skills := &fakeSkillResolver{byName: map[string]string{"summarize": "# summarize skill body"}}
	rt := newTestRuntime(t, Options{Skills: skills})
	d := NewDispatcher(rt)

	out, _, sink := newSink()
	intent, err := Classify("printf 'a\\nb\\nc' | @summarize")
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	res, err := d.Dispatch(context.Background(), intent, sink)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d", res.ExitCode)
	}
	body := out.String()
	if !strings.Contains(body, "summarize skill body") {
		t.Fatalf("missing skill body in output: %q", body)
	}
	if !strings.Contains(body, "input from upstream pipe") {
		t.Fatalf("missing upstream-pipe banner in output: %q", body)
	}
	if !strings.Contains(body, "a\nb\nc") {
		t.Fatalf("missing upstream stdout in output: %q", body)
	}
}

func TestDispatcher_SkillSourcePipe(t *testing.T) {
	skills := &fakeSkillResolver{byName: map[string]string{"summarize": "agent says: hello"}}
	rt := newTestRuntime(t, Options{Skills: skills})
	d := NewDispatcher(rt)

	out, _, sink := newSink()
	intent, err := Classify("@summarize | tr a-z A-Z")
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	res, err := d.Dispatch(context.Background(), intent, sink)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d", res.ExitCode)
	}
	body := out.String()
	if !strings.Contains(body, "AGENT SAYS: HELLO") {
		t.Fatalf("downstream uppercase did not run; out = %q", body)
	}
}

func TestDispatcher_PipeToSkillUpstreamFails(t *testing.T) {
	skills := &fakeSkillResolver{byName: map[string]string{"summarize": "ok"}}
	rt := newTestRuntime(t, Options{Skills: skills})
	d := NewDispatcher(rt)

	_, errBuf, sink := newSink()
	intent, err := Classify("false | @summarize")
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	res, err := d.Dispatch(context.Background(), intent, sink)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if res.ExitCode == 0 {
		t.Fatalf("expected non-zero exit on upstream failure")
	}
	if !strings.Contains(errBuf.String(), "upstream pipe exited") {
		t.Fatalf("expected upstream-failure message, got %q", errBuf.String())
	}
}

func TestDispatcher_AgentNoProvider(t *testing.T) {
	rt := newTestRuntime(t, Options{}) // Provider == nil
	d := NewDispatcher(rt)

	t.Run("agent shot reports missing provider", func(t *testing.T) {
		_, errBuf, sink := newSink()
		intent, _ := Classify("!ping host")
		res, err := d.Dispatch(context.Background(), intent, sink)
		if err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		if res.ExitCode == 0 {
			t.Fatalf("expected non-zero exit when no provider configured")
		}
		if !strings.Contains(errBuf.String(), "no LLM provider configured") {
			t.Fatalf("stderr = %q, expected provider-missing message", errBuf.String())
		}
	})
	t.Run("agent qa reports missing provider", func(t *testing.T) {
		_, errBuf, sink := newSink()
		intent, _ := Classify("?why is the sky blue")
		res, err := d.Dispatch(context.Background(), intent, sink)
		if err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		if res.ExitCode == 0 {
			t.Fatalf("expected non-zero exit when no provider configured")
		}
		if !strings.Contains(errBuf.String(), "no LLM provider configured") {
			t.Fatalf("stderr = %q", errBuf.String())
		}
	})
}

func TestDispatcher_AgentWithMockProvider(t *testing.T) {
	provider := newMockProvider("hello world")
	rt := newTestRuntime(t, Options{Provider: provider, Model: "test-model"})
	d := NewDispatcher(rt)

	t.Run("agent shot streams response", func(t *testing.T) {
		out, _, sink := newSink()
		intent, _ := Classify("!summarize my day")
		res, err := d.Dispatch(context.Background(), intent, sink)
		if err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("exit = %d", res.ExitCode)
		}
		if !strings.Contains(out.String(), "hello world") {
			t.Fatalf("stdout = %q, expected 'hello world'", out.String())
		}
	})
	t.Run("agent qa streams response", func(t *testing.T) {
		out, _, sink := newSink()
		intent, _ := Classify("?what time is it")
		res, err := d.Dispatch(context.Background(), intent, sink)
		if err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("exit = %d", res.ExitCode)
		}
		if !strings.Contains(out.String(), "hello world") {
			t.Fatalf("stdout = %q", out.String())
		}
	})
}

func TestDispatcher_AgentShotSeesShellContext(t *testing.T) {
	provider := newMockProvider("noted")
	rt := newTestRuntime(t, Options{Provider: provider, Model: "test-model"})
	d := NewDispatcher(rt)

	// First, run a bash command so its output ends up in history.
	_, _, sink := newSink()
	bashIntent, _ := Classify("echo abc-marker")
	if _, err := d.Dispatch(context.Background(), bashIntent, sink); err != nil {
		t.Fatalf("bash: %v", err)
	}

	// Inspect the history that the next `!` would attach.
	cmd, output := rt.History()
	if cmd != "echo abc-marker" {
		t.Errorf("lastCmd = %q, want echo abc-marker", cmd)
	}
	if !strings.Contains(output, "abc-marker") {
		t.Errorf("lastOutput = %q, want it to contain abc-marker", output)
	}
}

func TestDispatcher_Empty(t *testing.T) {
	rt := newTestRuntime(t, Options{})
	d := NewDispatcher(rt)
	_, _, sink := newSink()
	intent, _ := Classify("   ")
	res, err := d.Dispatch(context.Background(), intent, sink)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0", res.ExitCode)
	}
}

func TestIsShellSafeSlash(t *testing.T) {
	registry := commands.NewRegistry()
	registry.Register(&commands.Spec{Name: "help", ShellSafe: true,
		Handler: func(_ context.Context, _ string) (string, error) { return "ok", nil }})
	registry.Register(&commands.Spec{Name: "deploy", ShellSafe: false,
		Handler: func(_ context.Context, _ string) (string, error) { return "no", nil }})
	rt := newTestRuntime(t, Options{Registry: registry})

	if !IsShellSafeSlash(rt, "help") {
		t.Errorf("help should be shell-safe")
	}
	if IsShellSafeSlash(rt, "deploy") {
		t.Errorf("deploy should NOT be shell-safe")
	}
	if IsShellSafeSlash(rt, "missing") {
		t.Errorf("unregistered name should NOT be shell-safe")
	}
}
