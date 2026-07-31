//go:build e2e

package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// E2E tests for `ycode shell`. Drives the real binary.
//   - ycode shell --manifest, --suggest, -c "/help"
//
// The `yc <verb>` tests that used to live here were REMOVED: `yc` is not
// a top-level ycode subcommand any more (the binary answers `unknown
// command "yc"`), so they asserted against a surface that no longer
// exists. The yc verbs live on as in-process built-ins reached through
// the shell — see internal/shell/. Skipped historically and also gone:
// `yc refs` (hung), `yc graph` (needs a running server).

// runYC runs `ycode yc <args...>` in repo with HOME isolated.
// runShellArgs runs `ycode shell <args...>`.
func runShellArgs(t *testing.T, repo, home string, args ...string) (string, error) {
	t.Helper()
	if _, err := os.Stat(e2eBinaryPath); os.IsNotExist(err) {
		t.Skipf("binary not found at %s; run 'make compile' first", e2eBinaryPath)
	}
	binAbs, _ := filepath.Abs(e2eBinaryPath)
	cmd := exec.Command(binAbs, append([]string{"shell"}, args...)...)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"TERM=dumb",
		"YCODE_NO_SERVER=1",
		"ANTHROPIC_API_KEY=", "OPENAI_API_KEY=",
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestE2E_Shell_Manifest_HasFourSentinels(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test skipped in -short")
	}
	out, err := runShellArgs(t, t.TempDir(), t.TempDir(), "--manifest")
	if err != nil {
		t.Fatalf("shell --manifest: %v\n%s", err, out)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	sentinels, _ := m["sentinels"].([]any)
	if len(sentinels) != 4 {
		t.Errorf("expected 4 sentinels (/, @, !, ?); got: %v", sentinels)
	}
	if _, ok := m["builtins"]; !ok {
		t.Error("manifest missing builtins")
	}
	if _, ok := m["modes"]; !ok {
		t.Error("manifest missing modes")
	}
}

func TestE2E_Shell_Suggest_EmitsHint(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test skipped in -short")
	}
	out, err := runShellArgs(t, t.TempDir(), t.TempDir(), "--suggest", "grep -r foo .")
	if err != nil {
		t.Fatalf("shell --suggest: %v\n%s", err, out)
	}
	// Hint engine should suggest `yc search-symbols` for grep -r.
	if !strings.Contains(out, "yc search-symbols") {
		t.Errorf("expected `yc search-symbols` hint; got:\n%s", out)
	}
}

func TestE2E_Shell_SlashSentinel_HelpDispatch(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test skipped in -short")
	}
	out, err := runShellArgs(t, t.TempDir(), t.TempDir(), "-c", "/help")
	if err != nil {
		t.Fatalf("shell -c /help: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Sentinels") {
		t.Errorf("/help did not render sentinel docs; got:\n%s", out)
	}
}
