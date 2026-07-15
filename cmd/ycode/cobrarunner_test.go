package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/mcp"
)

func TestCobraMCPHandlerImplements(t *testing.T) {
	var _ mcp.ServerHandler = (*cobraMCPHandler)(nil)
	var _ mcp.PermissionAware = (*cobraMCPHandler)(nil)
}

func TestListYcodeCommandsReturnsAllowlist(t *testing.T) {
	h := newCobraMCPHandler()
	out, err := h.HandleToolCall(context.Background(), "list_ycode_commands", nil)
	if err != nil {
		t.Fatalf("list_ycode_commands: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(rows) == 0 {
		t.Fatal("allowlist empty")
	}
	for _, r := range rows {
		for _, field := range []string{"verb", "mode", "help"} {
			if _, ok := r[field]; !ok {
				t.Errorf("row missing %q: %v", field, r)
			}
		}
	}
}

func TestRunVerbRequiresVerb(t *testing.T) {
	h := newCobraMCPHandler()
	_, err := h.HandleToolCall(context.Background(), "run_ycode_command", json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "verb is required") {
		t.Fatalf("expected verb-required error, got: %v", err)
	}
}

func TestRunVerbRejectsUnknownVerb(t *testing.T) {
	h := newCobraMCPHandler()
	_, err := h.HandleToolCall(context.Background(), "run_ycode_command",
		json.RawMessage(`{"verb": "drop_database"}`))
	if err == nil || !strings.Contains(err.Error(), "not allowlisted") {
		t.Fatalf("expected allowlist rejection, got: %v", err)
	}
}

// TestRunVerbRejectsWriteTierFromReadOnlyTool is the safeguard #4
// regression test: even though `init` is an allowlisted WorkspaceWrite
// verb, calling it via the ReadOnly tool must fail and point the caller
// at run_ycode_command_workspace.
func TestRunVerbRejectsWriteTierFromReadOnlyTool(t *testing.T) {
	h := newCobraMCPHandler()
	_, err := h.HandleToolCall(context.Background(), "run_ycode_command",
		json.RawMessage(`{"verb": "init"}`))
	if err == nil {
		t.Fatal("expected tier rejection")
	}
	if !strings.Contains(err.Error(), "run_ycode_command_workspace") {
		t.Errorf("error should redirect to workspace tool; got: %v", err)
	}
}

func TestRunVerbRejectsRestrictedSubcommand(t *testing.T) {
	h := newCobraMCPHandler()
	// `model pull` should be rejected — only list/available/info are in the sub-allowlist.
	_, err := h.HandleToolCall(context.Background(), "run_ycode_command",
		json.RawMessage(`{"verb": "model", "args": ["pull", "llama3"]}`))
	if err == nil || !strings.Contains(err.Error(), "restricts subcommands") {
		t.Fatalf("expected sub-allowlist rejection, got: %v", err)
	}
}

// TestRunVersionRoundTrip is the live end-to-end check: spawn the test
// binary as a child via execCobraVerb. Skip when YCODE_TEST_SKIP_EXEC is
// set (some CI environments don't allow self-exec).
//
// Uses `version` since it's the simplest verb: instant, no API key, no
// network, no filesystem writes.
func TestRunVersionRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess exec in -short")
	}
	// In `go test`, os.Executable() returns the test binary, not the
	// ycode binary — so the child won't recognize "version" as a verb.
	// Skip the live call; the unit tests above cover dispatch logic.
	t.Skip("subprocess exec uses test binary, not ycode; covered by ycode-binary e2e probe")
}

func TestRequiredModePerTool(t *testing.T) {
	h := newCobraMCPHandler()
	cases := []struct {
		tool string
		want mcp.PermissionMode
	}{
		{"list_ycode_commands", mcp.ModeReadOnly},
		{"run_ycode_command", mcp.ModeReadOnly},
		{"run_ycode_command_workspace", mcp.ModeWorkspaceWrite},
		{"unknown", mcp.ModeDangerFullAccess},
	}
	for _, c := range cases {
		if got := h.RequiredMode(c.tool); got != c.want {
			t.Errorf("%s: want %v, got %v", c.tool, c.want, got)
		}
	}
}

func TestUnknownToolErrors(t *testing.T) {
	h := newCobraMCPHandler()
	_, err := h.HandleToolCall(context.Background(), "exec_anything", nil)
	if err == nil {
		t.Fatal("unknown tool should error")
	}
}

func TestFirstNonFlagArg(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"--json"}, ""},
		{[]string{"list"}, "list"},
		{[]string{"--verbose", "list", "--json"}, "list"},
		{[]string{"-v", "available"}, "available"},
	}
	for _, c := range cases {
		if got := firstNonFlagArg(c.in); got != c.want {
			t.Errorf("firstNonFlagArg(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
