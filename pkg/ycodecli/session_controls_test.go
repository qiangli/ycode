package ycodecli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	harnesscli "github.com/qiangli/ycode/internal/harness/cli"
	public "github.com/qiangli/ycode/pkg/ycode"
)

func TestDeclaredSessionControlsExecuteAndDryRun(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BASHY_HOME", "")
	provider := &applicationProvider{}
	app, err := openHarnessApplication(harnessFixture(t), public.WithHarnessProvider("openai", provider))
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	run := func(args ...string) (string, error) {
		root, err := harnesscli.New(app.doc, func(ctx context.Context, inv harnesscli.Invocation, streams harnesscli.IO) error {
			return sessionCLI(ctx, app, inv, streams.Out)
		}, harnesscli.Options{})
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetArgs(args)
		err = root.ExecuteContext(context.Background())
		return out.String(), err
	}
	for _, args := range [][]string{{"pause"}, {"btw", "aside"}, {"retry"}, {"revert"}, {"compact"}, {"plan", "change nothing"}, {"model", "use", "primary"}} {
		args = append(args, "--session", "fresh", "--dry-run")
		out, err := run(args...)
		if err != nil || !strings.Contains(out, `"dry_run": true`) {
			t.Fatalf("dry run %v: %s %v", args, out, err)
		}
	}
	if sessions, err := app.harness.Sessions(); err != nil || len(sessions) != 0 || provider.calls.Load() != 0 {
		t.Fatalf("dry run mutated: %v %v calls=%d", sessions, err, provider.calls.Load())
	}
	if out, err := run("model", "use", "primary", "--session", "cli"); err != nil || !strings.Contains(out, `"model_ref": "primary"`) {
		t.Fatalf("model use: %s %v", out, err)
	}
	if out, err := run("model", "current", "--session", "cli"); err != nil || out != "gpt-5.6\n" {
		t.Fatalf("model current: %s %v", out, err)
	}
	if out, err := run("plan", "explain the task", "--session", "cli"); err != nil || !strings.Contains(out, "configured-output") {
		t.Fatalf("plan: %s %v", out, err)
	}
	if out, err := run("retry", "--session", "cli", "--json"); err != nil || !strings.Contains(out, "output.emitted") {
		t.Fatalf("retry: %s %v", out, err)
	}
	if out, err := run("revert", "--session", "cli"); err != nil || !strings.Contains(out, `"files_restored": false`) {
		t.Fatalf("revert: %s %v", out, err)
	}
	if out, err := run("compact", "--session", "cli"); err != nil || !strings.Contains(out, "nothing-to-compact") {
		t.Fatalf("compact: %s %v", out, err)
	}
	if out, err := run("btw", "remember", "--session", "cli"); err != nil || !strings.Contains(out, "configured-output") {
		t.Fatalf("btw: %s %v", out, err)
	}
	if _, err := run("pause", "--session", "cli"); err == nil {
		t.Fatal("pause faked success without live owner")
	}
}
