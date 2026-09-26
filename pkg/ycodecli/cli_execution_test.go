package ycodecli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	harnesscli "github.com/qiangli/ycode/internal/harness/cli"
	"github.com/qiangli/ycode/internal/harness/spec"
	public "github.com/qiangli/ycode/pkg/ycode"
)

func TestYAMLCLIProjectsInputsThroughDurableHarness(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	provider := &applicationProvider{}
	app, err := openHarnessApplication(harnessFixture(t), public.WithHarnessProvider("openai", provider))
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	cases := []struct {
		args                  []string
		stdin, mode, frontend string
		terminal              bool
	}{
		{args: []string{"prompt", "one"}, mode: "args", frontend: "one-shot"},
		{stdin: "two\n", mode: "stdin", frontend: "one-shot"},
		{args: []string{"repl"}, stdin: "three\n\n", mode: "repl", frontend: "repl"},
		{stdin: "four\n", mode: "repl", frontend: "tui", terminal: true},
	}
	for _, item := range cases {
		root, err := harnesscli.New(app.doc, func(ctx context.Context, inv harnesscli.Invocation, streams harnesscli.IO) error {
			if inv.Mode != item.mode || inv.FrontendRef != item.frontend || inv.Dispatch.AgentRef != "coder" || inv.Dispatch.TriggerRef != "interactive-input" {
				t.Fatalf("unexpected invocation: %#v", inv)
			}
			return runCLIInput(ctx, app, inv, streams)
		}, harnesscli.Options{IsTerminal: item.terminal})
		if err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		root.SetIn(bytes.NewBufferString(item.stdin))
		root.SetOut(&output)
		root.SetArgs(append([]string{"--session", "yaml-cli-session"}, item.args...))
		if err := root.ExecuteContext(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(output.Bytes(), []byte("configured-output")) {
			t.Fatalf("output = %q", output.String())
		}
	}
	if provider.calls.Load() != int32(len(cases)) {
		t.Fatalf("provider calls = %d", provider.calls.Load())
	}
	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(filepath.Join(base, app.doc.Spec.Runtime.ControlRoot.PlatformDataDir, "sessions", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if got := bytes.Count(log, []byte(`"type":"session.turn-committed"`)); got != len(cases) || !bytes.Contains(log, []byte("yaml-cli-session")) {
		t.Fatalf("CLI persisted %d session commits, want %d", got, len(cases))
	}
}

func TestYAMLCLIRejectsEmptyStdinAndMismatchedRoute(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	app, err := openHarnessApplication(harnessFixture(t), public.WithHarnessProvider("openai", &applicationProvider{}))
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	inv := harnesscli.Invocation{Dispatch: *app.doc.Spec.Interfaces.CLI.Root.Dispatch, Mode: "stdin", FrontendRef: "one-shot"}
	streams := harnesscli.IO{In: bytes.NewBufferString(" \n"), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	if err := runCLIInput(context.Background(), app, inv, streams); harnesscli.ExitCode(err) != 2 {
		t.Fatalf("empty stdin: %v", err)
	}
	inv.Dispatch.TriggerRef = "unconfigured"
	if err := runCLIInput(context.Background(), app, inv, streams); err == nil {
		t.Fatal("mismatched route was accepted")
	}
}

func TestShellWorkspaceCannotEscapeCompiledRoot(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	if cwd, err := shellWorkspace(root, ".", "child"); err != nil || cwd == "" {
		t.Fatalf("child workspace: %q, %v", cwd, err)
	}
	outside := t.TempDir()
	if _, err := shellWorkspace(root, ".", outside); err == nil {
		t.Fatal("outside workdir accepted")
	}
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := shellWorkspace(root, ".", link); err == nil {
		t.Fatal("symlink escape accepted")
	}
}

func TestInspectionHonorsDeclaredBashyColumns(t *testing.T) {
	doc, err := spec.Load(harnessFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	inv := harnesscli.Invocation{Dispatch: spec.CLIDispatch{Operation: "inspect", Resource: "bashy", Action: "list", Columns: []string{"contract", "execution.preflight"}}}
	if err := inspectCLI(doc, inv, &output); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "bashy\tbashy-run-v1\trequired\n" {
		t.Fatalf("columns = %q", got)
	}
}
