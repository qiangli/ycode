package ycodecli

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	harnesscli "github.com/qiangli/ycode/internal/harness/cli"
	public "github.com/qiangli/ycode/pkg/ycode"
)

func TestYAMLCLISessionGroupAndResume(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("BASHY_HOME", "")
	provider := &applicationProvider{}
	app, err := openHarnessApplication(harnessFixture(t), public.WithHarnessProvider("openai", provider))
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	run := func(stdin string, args ...string) (string, error) {
		t.Helper()
		root, err := harnesscli.New(app.doc, func(ctx context.Context, inv harnesscli.Invocation, streams harnesscli.IO) error {
			if inv.Dispatch.Operation == "session" {
				return sessionCLI(ctx, app, inv, streams.Out)
			}
			return runCLIInput(ctx, app, inv, streams)
		}, harnesscli.Options{})
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		root.SetIn(strings.NewReader(stdin))
		root.SetOut(&out)
		root.SetArgs(args)
		err = root.ExecuteContext(context.Background())
		return out.String(), err
	}

	if _, err := run("", "resume"); err == nil || !strings.Contains(err.Error(), "no session to resume") {
		t.Fatalf("resume on an empty log: err=%v", err)
	}
	if _, err := run("", "--session", "sess-one", "prompt", "first request"); err != nil {
		t.Fatal(err)
	}
	if _, err := run("", "--session", "sess-two", "prompt", "second request"); err != nil {
		t.Fatal(err)
	}
	// resume continues the latest session (sess-two), a prefix picks another.
	if _, err := run("follow up\n", "resume"); err != nil {
		t.Fatal(err)
	}
	if _, err := run("", "--session", "sess-o", "prompt", "third request"); err != nil {
		t.Fatal(err)
	}

	list, err := run("", "session", "list")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(list), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[1], "sess-one") || !strings.HasPrefix(lines[2], "sess-two") || !strings.Contains(lines[1], "first request") {
		t.Fatalf("session list:\n%s", list)
	}
	show, err := run("", "session", "show", "sess-two")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(show, "turns    2 committed") || !strings.Contains(show, "[user] follow up") {
		t.Fatalf("session show:\n%s", show)
	}
	if out, err := run("", "session", "rename", "sess-two", "second", "work"); err != nil || !strings.Contains(out, "second work") {
		t.Fatalf("rename: %q %v", out, err)
	}
	export, err := run("", "session", "export", "sess-two")
	if err != nil || !strings.HasPrefix(export, "# second work\n") || !strings.Contains(export, "## user\n\nsecond request") {
		t.Fatalf("export: %v\n%s", err, export)
	}
	found, err := run("", "session", "search", "THIRD")
	if err != nil || !strings.HasPrefix(found, "sess-one") {
		t.Fatalf("search: %v\n%s", err, found)
	}
	child, err := run("", "session", "fork", "sess-one")
	if err != nil {
		t.Fatal(err)
	}
	childID := strings.TrimSpace(child)
	if show, err := run("", "session", "show", childID); err != nil || !strings.Contains(show, "parent   sess-one") || !strings.Contains(show, "[user] third request") {
		t.Fatalf("fork child show: %v\n%s", err, show)
	}
	if js, err := run("", "session", "list", "--json"); err != nil || !strings.Contains(js, `"schema_version": "ycode-sessions-v1"`) {
		t.Fatalf("list --json: %v\n%s", err, js)
	}
	if _, err := run("", "session", "rename", "sess-two"); err == nil {
		t.Fatal("rename without a title must be a usage error")
	}
}

// A terminal front end exports a session pointer: a turn typed there
// continues the pointed session, `new` and `resume` move the pointer (resume
// never nests a second session loop), and `status` reports it.
func TestYAMLCLITerminalSessionPointer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("BASHY_HOME", "")
	pointer := home + "/terminal.session"
	if err := os.WriteFile(pointer, []byte("sess-a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(SessionFileEnv, pointer)
	provider := &applicationProvider{}
	app, err := openHarnessApplication(harnessFixture(t), public.WithHarnessProvider("openai", provider))
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	run := func(args ...string) string {
		t.Helper()
		root, err := harnesscli.New(app.doc, func(ctx context.Context, inv harnesscli.Invocation, streams harnesscli.IO) error {
			if inv.Dispatch.Operation == "session" {
				return sessionCLI(ctx, app, inv, streams.Out)
			}
			return runCLIInput(ctx, app, inv, streams)
		}, harnesscli.Options{})
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		root.SetIn(strings.NewReader(""))
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(args)
		if err := root.ExecuteContext(context.Background()); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out.String())
		}
		return out.String()
	}
	pointed := func() string {
		data, err := os.ReadFile(pointer)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(data))
	}

	run("prompt", "first request")
	if show := run("session", "show", "sess-a"); !strings.Contains(show, "[user] first request") {
		t.Fatalf("the turn did not land in the pointed session:\n%s", show)
	}
	if status := run("status"); !strings.Contains(status, "sess-a") || !strings.Contains(status, "turns    1 committed") {
		t.Fatalf("status:\n%s", status)
	}
	fresh := strings.TrimSpace(run("new"))
	if fresh == "" || fresh == "sess-a" || pointed() != fresh {
		t.Fatalf("new printed %q, pointer %q", fresh, pointed())
	}
	if status := run("status", "--json"); !strings.Contains(status, `"session": "`+fresh+`"`) || !strings.Contains(status, `"terminal": true`) {
		t.Fatalf("status --json:\n%s", status)
	}
	if out := run("resume", "sess-a"); !strings.Contains(out, "session sess-a") || pointed() != "sess-a" {
		t.Fatalf("resume inside a terminal: %q, pointer %q", out, pointed())
	}
	run("resume", fresh)
	if out := run("resume"); pointed() != "sess-a" || !strings.Contains(out, "session sess-a") {
		t.Fatalf("resume without a session picks the latest: %q, pointer %q", out, pointed())
	}
}
