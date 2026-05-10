//go:build e2e

package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// E2E tests for `ycode shell` and `yc <verb>` built-ins. Drives the
// real binary; covers the surfaces verified manually:
//   - yc symbols / search-symbols / repomap / git / manifest /
//     browser fetch / remember+recall
//   - ycode shell --manifest, --suggest, -c "/help"
//
// Skipped here:
//   - yc refs   (known-hung; tracked separately)
//   - yc graph  (needs ycode serve; bonsai DB-state-dependent)
//   - !/?       (LLM-dependent)

// runYC runs `ycode yc <args...>` in repo with HOME isolated.
func runYC(t *testing.T, repo, home string, args ...string) (string, error) {
	t.Helper()
	if _, err := os.Stat(e2eBinaryPath); os.IsNotExist(err) {
		t.Skipf("binary not found at %s; run 'make compile' first", e2eBinaryPath)
	}
	binAbs, _ := filepath.Abs(e2eBinaryPath)
	cmd := exec.Command(binAbs, append([]string{"yc"}, args...)...)
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

func TestE2E_YC_Symbols_TreesitterParse(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test skipped in -short")
	}
	repo := t.TempDir()
	src := filepath.Join(repo, "main.go")
	if err := os.WriteFile(src, []byte("package main\n\nfunc Hello() string { return \"hi\" }\n\ntype Greeter struct{}\n"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	out, err := runYC(t, repo, t.TempDir(), "symbols", src)
	if err != nil {
		t.Fatalf("yc symbols: %v\n%s", err, out)
	}
	if !strings.Contains(out, "func Hello()") || !strings.Contains(out, "type Greeter") {
		t.Errorf("expected Hello + Greeter in output; got:\n%s", out)
	}
}

func TestE2E_YC_SearchSymbols_FindByName(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test skipped in -short")
	}
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package x\nfunc DoTheThing() {}\nfunc otherThing() {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := runYC(t, repo, t.TempDir(), "search-symbols", "DoTheThing", repo)
	if err != nil {
		t.Fatalf("yc search-symbols: %v\n%s", err, out)
	}
	if !strings.Contains(out, "DoTheThing") {
		t.Errorf("expected DoTheThing in output; got:\n%s", out)
	}
	if strings.Contains(out, "otherThing") {
		t.Errorf("non-matching symbol leaked into output:\n%s", out)
	}
}

func TestE2E_YC_Repomap_TokenBudgeted(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test skipped in -short")
	}
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package x\nfunc One() {}\n"), 0o644); err != nil {
		t.Fatalf("write a.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "b.go"), []byte("package x\nfunc Two() {}\n"), 0o644); err != nil {
		t.Fatalf("write b.go: %v", err)
	}
	out, err := runYC(t, repo, t.TempDir(), "repomap", "--budget=300", repo)
	if err != nil {
		t.Fatalf("yc repomap: %v\n%s", err, out)
	}
	if !strings.Contains(out, "func One()") || !strings.Contains(out, "func Two()") {
		t.Errorf("expected both funcs in repomap; got:\n%s", out)
	}
}

func TestE2E_YC_GitStatus_NativeGoGit(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test skipped in -short")
	}
	repo := initRepo(t)
	// Add an untracked file to verify status detection.
	if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write untracked: %v", err)
	}
	out, err := runYC(t, repo, t.TempDir(), "git", "status")
	if err != nil {
		t.Fatalf("yc git status: %v\n%s", err, out)
	}
	if !strings.Contains(out, "untracked.txt") {
		t.Errorf("expected untracked.txt in git status; got:\n%s", out)
	}
}

func TestE2E_YC_Manifest_ValidJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test skipped in -short")
	}
	out, err := runYC(t, t.TempDir(), t.TempDir(), "manifest")
	if err != nil {
		t.Fatalf("yc manifest: %v\n%s", err, out)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	bs, ok := m["builtins"].([]any)
	if !ok || len(bs) < 10 {
		t.Errorf("expected >=10 builtins in manifest; got: %v", m["builtins"])
	}
}

func TestE2E_YC_BrowserFetch_HTTPGet(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test skipped in -short")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":"yes"}`))
	}))
	defer srv.Close()

	out, err := runYC(t, t.TempDir(), t.TempDir(), "browser", "fetch", srv.URL)
	if err != nil {
		t.Fatalf("yc browser fetch: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"ok":"yes"`) {
		t.Errorf("expected JSON body in output; got:\n%s", out)
	}
	if !strings.Contains(out, "200") {
		t.Errorf("expected 200 status in headers; got:\n%s", out)
	}
}

func TestE2E_YC_RememberRecall_RoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test skipped in -short")
	}
	repo := initRepo(t)
	home := t.TempDir()

	if out, err := runYC(t, repo, home, "remember", "the answer is 42", "--type=user"); err != nil {
		t.Fatalf("yc remember: %v\n%s", err, out)
	}
	out, err := runYC(t, repo, home, "recall", "answer", "--limit=2")
	if err != nil {
		t.Fatalf("yc recall: %v\n%s", err, out)
	}
	if !strings.Contains(out, "42") {
		t.Errorf("recall did not surface saved memory; got:\n%s", out)
	}
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
