//go:build e2e

package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// E2E tests for the `ycode weave` subverb tree. Each test drives the
// real ycode binary in a fresh temp git repo with HOME isolated to
// t.TempDir(), so the per-project queue at $HOME/.agents/ycode/weave/
// is sandboxed.
//
// Run: go test -tags e2e -count=1 ./cmd/ycode/ -run TestWeaveE2E
//
// Prerequisite: the binary at bin/ycode must already be built (`make
// compile`). The tests skip when it's missing rather than rebuild —
// keeps the unit-test feedback loop fast.

const weaveE2EBinary = "../../bin/ycode"

// weaveSetupRepo creates a fresh git repo with one seed commit and
// returns (repoDir, homeDir). HOME isolation is critical — weave
// stores its queue under $HOME/.agents/ycode/weave/<tag>/queue.json,
// and we MUST NOT touch the developer's real queue.
func weaveSetupRepo(t *testing.T) (string, string) {
	t.Helper()
	repo := t.TempDir()
	home := t.TempDir()
	// `git init` with explicit initial branch so the base-branch logic
	// in weaveBaseBranch finds "main" deterministically (older git
	// defaults to "master").
	cmds := [][]string{
		{"git", "init", "-q", "-b", "main"},
		{"git", "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-qm", "seed"},
	}
	for _, c := range cmds {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}
	return repo, home
}

// runWeave executes `bin/ycode weave <args...>` in the given repo
// with the given HOME, returns the combined output and an exit
// error (nil on exit 0). Stdin is /dev/null so subverbs that prompt
// for confirmation refuse rather than hanging.
func runWeave(t *testing.T, repo, home string, args ...string) (string, *exec.ExitError) {
	t.Helper()
	if _, err := os.Stat(weaveE2EBinary); os.IsNotExist(err) {
		t.Skipf("binary not found at %s; run 'make compile' first", weaveE2EBinary)
	}
	binAbs, err := filepath.Abs(weaveE2EBinary)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	full := append([]string{"weave"}, args...)
	cmd := exec.Command(binAbs, full...)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"TERM=dumb",
		"YCODE_NO_SERVER=1",
		"ANTHROPIC_API_KEY=",
		"OPENAI_API_KEY=",
		// Force non-interactive output mode — defaults that depend on
		// tty-detection would flap in CI.
		"YCODE_AGENT=1",
	)
	cmd.Stdin = nil // closed; reset --yes path checks this
	out, runErr := cmd.CombinedOutput()
	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		return string(out), ee
	}
	if runErr != nil {
		t.Fatalf("unexpected non-exit error running ycode weave %v: %v\n%s", args, runErr, out)
	}
	return string(out), nil
}

// parseEnvelope extracts the first JSON envelope (object containing
// "schema_version") from output, tolerating any leading non-JSON
// preamble. Git's worktree-add prints "Preparing worktree..." /
// "HEAD is now at ..." to stderr; weave's pull prints merge output;
// CombinedOutput interleaves all of it. The parser scans for each
// `{` and tries to decode a balanced object starting there,
// returning the first one that's a valid envelope.
func parseEnvelope(t *testing.T, output string) map[string]any {
	t.Helper()
	for i := 0; i < len(output); i++ {
		if output[i] != '{' {
			continue
		}
		dec := json.NewDecoder(strings.NewReader(output[i:]))
		var env map[string]any
		if err := dec.Decode(&env); err != nil {
			continue
		}
		if _, ok := env["schema_version"]; !ok {
			continue
		}
		return env
	}
	t.Fatalf("no JSON envelope in output: %s", output)
	return nil
}

// envExitCode pulls the exit code from an *exec.ExitError (or 0 if
// nil), normalizing across platforms.
func envExitCode(ee *exec.ExitError) int {
	if ee == nil {
		return 0
	}
	return ee.ExitCode()
}

// ─── Tests ────────────────────────────────────────────────────────

func TestWeaveE2E_Add_Then_List(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)

	out, ee := runWeave(t, repo, home, "add", "fix null deref", "--priority", "p0", "--body", "stack trace in log.txt")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("add exited %d; out=%s", got, out)
	}
	env := parseEnvelope(t, out)
	if env["status"] != "ok" {
		t.Fatalf("add status=%v env=%v", env["status"], env)
	}
	res, _ := env["result"].(map[string]any)
	if int(res["issue"].(float64)) != 1 {
		t.Fatalf("first add: expected issue=1, got %v", res["issue"])
	}
	if res["priority"] != "p0" {
		t.Fatalf("expected priority=p0, got %v", res["priority"])
	}

	// Second add gets ID=2 (NextID increments).
	out, ee = runWeave(t, repo, home, "add", "refactor users")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("add #2 exited %d; out=%s", got, out)
	}
	res2, _ := parseEnvelope(t, out)["result"].(map[string]any)
	if int(res2["issue"].(float64)) != 2 {
		t.Fatalf("second add: expected issue=2, got %v", res2["issue"])
	}

	// list should show both, with the p0 issue first via the priority sort.
	out, ee = runWeave(t, repo, home, "list")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("list exited %d; out=%s", got, out)
	}
	items := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("list: expected 2 items, got %d", len(items))
	}
}

func TestWeaveE2E_Add_RequiresTitle(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	out, ee := runWeave(t, repo, home, "add")
	if got := envExitCode(ee); got != 2 {
		t.Fatalf("expected exit=2 (invalid_arg), got %d; out=%s", got, out)
	}
	env := parseEnvelope(t, out)
	if env["status"] != "error" {
		t.Fatalf("expected status=error, got %v", env["status"])
	}
	errObj := env["error"].(map[string]any)
	if errObj["code"] != "invalid_arg" {
		t.Fatalf("expected error.code=invalid_arg, got %v", errObj["code"])
	}
}

func TestWeaveE2E_Next_OnEmptyQueue(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	out, ee := runWeave(t, repo, home, "next")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("next exited %d; out=%s", got, out)
	}
	res := parseEnvelope(t, out)["result"].(map[string]any)
	if res["empty"] != true {
		t.Fatalf("expected result.empty=true on fresh queue, got %v", res)
	}
}

func TestWeaveE2E_Prio_UpdatesIssue(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "feature work"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	out, ee := runWeave(t, repo, home, "prio", "1", "p0")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("prio exited %d; out=%s", got, out)
	}
	res := parseEnvelope(t, out)["result"].(map[string]any)
	if res["priority"] != "p0" {
		t.Fatalf("expected priority=p0 after change, got %v", res["priority"])
	}
	if res["previous"] != "p2" {
		t.Fatalf("expected previous=p2 (default), got %v", res["previous"])
	}
}

func TestWeaveE2E_Prio_AutoUnsupported(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "x"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	out, ee := runWeave(t, repo, home, "prio", "--auto")
	if got := envExitCode(ee); got != 5 {
		t.Fatalf("expected exit=5 (dependency_unhealthy), got %d; out=%s", got, out)
	}
	if parseEnvelope(t, out)["error"].(map[string]any)["code"] != "dependency_unhealthy" {
		t.Fatalf("expected error.code=dependency_unhealthy in %s", out)
	}
}

func TestWeaveE2E_Prio_InvalidTier(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "x"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	out, ee := runWeave(t, repo, home, "prio", "1", "p9")
	if got := envExitCode(ee); got != 2 {
		t.Fatalf("expected exit=2 (invalid_arg) for bad tier, got %d; out=%s", got, out)
	}
}

// TestWeaveE2E_Start_NoSpawn drives start with --no-spawn so the
// test doesn't need to execute a foreign tool. Verifies the
// worktree gets created, the queue moves to "working", and the
// envelope reports the sandbox path.
func TestWeaveE2E_Start_NoSpawn(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "test issue"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	out, ee := runWeave(t, repo, home, "start", "--no-spawn", "--issue", "1")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("start --no-spawn exited %d; out=%s", got, out)
	}
	res := parseEnvelope(t, out)["result"].(map[string]any)
	if res["state"] != "working" {
		t.Fatalf("expected state=working, got %v", res["state"])
	}
	sandbox, _ := res["sandbox"].(string)
	if sandbox == "" {
		t.Fatalf("expected sandbox path in result; got %v", res)
	}
	if _, err := os.Stat(sandbox); err != nil {
		t.Fatalf("sandbox dir missing on disk: %v", err)
	}
	// Branch should exist in the repo.
	branchOut, err := exec.Command("git", "-C", repo, "branch", "--list", "agent/weave-issue-1").CombinedOutput()
	if err != nil || !strings.Contains(string(branchOut), "agent/weave-issue-1") {
		t.Fatalf("expected branch agent/weave-issue-1 to exist; got %q (err=%v)", branchOut, err)
	}

	// list should now show state=working.
	out, _ = runWeave(t, repo, home, "list")
	items := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)
	if items[0].(map[string]any)["state"] != "working" {
		t.Fatalf("expected list to show state=working, got %v", items[0])
	}
}

func TestWeaveE2E_Start_WithTool_SubmittedOnSuccess(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "with tool"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	// `bash -c "true"` exits 0 → state should advance to "submitted"
	// after Wait, with exit_code=0 and a log path populated (since
	// the test runs in non-TTY mode and the PTY output goes to a
	// per-issue log file).
	out, ee := runWeave(t, repo, home, "start", "--issue", "1", "--", "bash", "-c", "true")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("start exited %d; out=%s", got, out)
	}
	res := parseEnvelope(t, out)["result"].(map[string]any)
	if res["state"] != "submitted" {
		t.Fatalf("expected state=submitted after success, got %v", res["state"])
	}
	if int(res["exit_code"].(float64)) != 0 {
		t.Fatalf("expected exit_code=0, got %v", res["exit_code"])
	}
	logPath, _ := res["log_path"].(string)
	if logPath == "" {
		t.Fatalf("expected log_path to be populated in non-TTY mode; res=%v", res)
	}
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("log file missing: %v", err)
	}
	// And the queue confirms the state via `list`.
	out, _ = runWeave(t, repo, home, "list", "--history")
	items := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)
	if items[0].(map[string]any)["state"] != "submitted" {
		t.Fatalf("list does not reflect submitted: %v", items[0])
	}
}

func TestWeaveE2E_Start_FailedOnNonZero(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "failing tool"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	// `bash -c "exit 7"` exits 7 → start should return exit 1 (its
	// own envelope reports generic_failure) but the queue item
	// must record state=failed with exit_code=7 so the orchestrator
	// can decide whether to retry, surface the log, or abandon.
	out, ee := runWeave(t, repo, home, "start", "--issue", "1", "--", "bash", "-c", "exit 7")
	if got := envExitCode(ee); got == 0 {
		t.Fatalf("expected non-zero exit when tool fails; got=%d; out=%s", got, out)
	}
	// State must be reflected on disk even though the envelope was
	// an error. Read via `list --history` to confirm.
	out2, _ := runWeave(t, repo, home, "list", "--history")
	items := parseEnvelope(t, out2)["result"].(map[string]any)["items"].([]any)
	item := items[0].(map[string]any)
	if item["state"] != "failed" {
		t.Fatalf("expected state=failed after non-zero exit, got %v", item["state"])
	}
	if int(item["exit_code"].(float64)) != 7 {
		t.Fatalf("expected exit_code=7, got %v", item["exit_code"])
	}
}

// TestWeaveE2E_PTY_SubagentSeesTTY confirms the headline MVP fix:
// even when ycode itself is launched with no TTY (orchestrator pipe
// / backgrounded by shell &), the subagent inside the worktree
// receives a real PTY. Verified by having bash check `[[ -t 0 ]]`
// and writing the answer to a file inside the worktree.
func TestWeaveE2E_PTY_SubagentSeesTTY(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "tty check"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	// bash writes "TTY" or "NOTTY" inside the sandbox depending on
	// whether stdin is a terminal. With weave's default PTY auto
	// mode, the file must contain "TTY".
	script := `if [ -t 0 ]; then echo TTY > pty-check; else echo NOTTY > pty-check; fi`
	if _, ee := runWeave(t, repo, home, "start", "--issue", "1", "--", "bash", "-c", script); envExitCode(ee) != 0 {
		t.Fatalf("start with tty-check failed")
	}
	out, _ := runWeave(t, repo, home, "list", "--history")
	item := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)[0].(map[string]any)
	sandbox := item["sandbox"].(string)
	if sandbox == "" {
		t.Fatalf("expected sandbox path on item; got %v", item)
	}
	got, err := os.ReadFile(filepath.Join(sandbox, "pty-check"))
	if err != nil {
		t.Fatalf("read pty-check: %v", err)
	}
	if strings.TrimSpace(string(got)) != "TTY" {
		t.Fatalf("subagent did NOT see a TTY: pty-check=%q (PTY allocation broken?)", got)
	}
}

func TestWeaveE2E_PTY_Never_FallsBackToInheritedFDs(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "no-pty"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	// With --pty=never, the inherited (non-TTY) stdin reaches bash —
	// bash should report NOTTY in the sandbox file.
	script := `if [ -t 0 ]; then echo TTY > pty-check; else echo NOTTY > pty-check; fi`
	if _, ee := runWeave(t, repo, home, "start", "--issue", "1", "--pty", "never", "--", "bash", "-c", script); envExitCode(ee) != 0 {
		t.Fatalf("start --pty=never failed")
	}
	out, _ := runWeave(t, repo, home, "list", "--history")
	item := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)[0].(map[string]any)
	got, err := os.ReadFile(filepath.Join(item["sandbox"].(string), "pty-check"))
	if err != nil {
		t.Fatalf("read pty-check: %v", err)
	}
	if strings.TrimSpace(string(got)) != "NOTTY" {
		t.Fatalf("expected NOTTY with --pty=never, got %q", got)
	}
}

func TestWeaveE2E_Resume_RequiresWorkingSandbox(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "x"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	// No prior start → resume must refuse with state_conflict.
	out, ee := runWeave(t, repo, home, "start", "--issue", "1", "--resume", "--", "bash", "-c", "true")
	if got := envExitCode(ee); got != 4 {
		t.Fatalf("expected exit=4 (state_conflict), got %d; out=%s", got, out)
	}
}

func TestWeaveE2E_Abandon(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "to abandon"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	if _, ee := runWeave(t, repo, home, "start", "--no-spawn", "--issue", "1"); envExitCode(ee) != 0 {
		t.Fatalf("start failed")
	}
	out, ee := runWeave(t, repo, home, "abandon", "1", "--reason", "test")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("abandon exited %d; out=%s", got, out)
	}
	res := parseEnvelope(t, out)["result"].(map[string]any)
	if res["state"] != "abandoned" {
		t.Fatalf("expected state=abandoned, got %v", res["state"])
	}
	if res["reason"] != "test" {
		t.Fatalf("expected reason=test, got %v", res["reason"])
	}
}

func TestWeaveE2E_Shell_AgentModeReturnsEnvelope(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "x"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	if _, ee := runWeave(t, repo, home, "start", "--no-spawn", "--issue", "1"); envExitCode(ee) != 0 {
		t.Fatalf("start failed")
	}
	// In agent mode (YCODE_AGENT=1 is set by runWeave) shell returns
	// the sandbox info instead of exec'ing — agents can't drive an
	// interactive shell anyway.
	out, ee := runWeave(t, repo, home, "shell", "1")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("shell exited %d; out=%s", got, out)
	}
	res := parseEnvelope(t, out)["result"].(map[string]any)
	if res["sandbox"] == nil || res["shell"] == nil {
		t.Fatalf("expected sandbox+shell in result, got %v", res)
	}
}

func TestWeaveE2E_Shell_NoSandbox(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "x"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	out, ee := runWeave(t, repo, home, "shell", "1")
	if got := envExitCode(ee); got != 4 {
		t.Fatalf("expected exit=4 (state_conflict), got %d; out=%s", got, out)
	}
}

func TestWeaveE2E_Reset(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "x"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	if _, ee := runWeave(t, repo, home, "start", "--no-spawn", "--issue", "1"); envExitCode(ee) != 0 {
		t.Fatalf("start failed")
	}
	// Without --yes, reset refuses in agent/non-TTY mode.
	out, ee := runWeave(t, repo, home, "reset")
	if got := envExitCode(ee); got != 2 {
		t.Fatalf("expected exit=2 without --yes, got %d; out=%s", got, out)
	}
	// With --yes, it tears everything down.
	out, ee = runWeave(t, repo, home, "reset", "--yes")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("reset --yes exited %d; out=%s", got, out)
	}
	res := parseEnvelope(t, out)["result"].(map[string]any)
	if int(res["count"].(float64)) != 1 {
		t.Fatalf("expected count=1 teardown, got %v", res["count"])
	}
	// list after reset is empty.
	out, _ = runWeave(t, repo, home, "list")
	res2 := parseEnvelope(t, out)["result"].(map[string]any)
	if items, _ := res2["items"].([]any); len(items) != 0 {
		t.Fatalf("expected empty list after reset, got %v", items)
	}
}

func TestWeaveE2E_Open_LocalBackendDeferred(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	// Without --issue: hard refuse (no local fallback).
	out, ee := runWeave(t, repo, home, "open")
	if got := envExitCode(ee); got != 5 {
		t.Fatalf("expected exit=5 for open without local fallback, got %d; out=%s", got, out)
	}
	if parseEnvelope(t, out)["error"].(map[string]any)["code"] != "dependency_unhealthy" {
		t.Fatalf("expected dependency_unhealthy in %s", out)
	}
}

func TestWeaveE2E_Open_IssueSurfacesSandboxURL(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "x"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	if _, ee := runWeave(t, repo, home, "start", "--no-spawn", "--issue", "1"); envExitCode(ee) != 0 {
		t.Fatalf("start failed")
	}
	out, ee := runWeave(t, repo, home, "open", "--issue", "1")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("open --issue 1 with live sandbox exited %d; out=%s", got, out)
	}
	res := parseEnvelope(t, out)["result"].(map[string]any)
	if sb, _ := res["sandbox_url"].(string); !strings.HasPrefix(sb, "file://") {
		t.Fatalf("expected sandbox_url=file://...; got %v", res)
	}
}

func TestWeaveE2E_InitBoard_LocalBackendDeferred(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	out, ee := runWeave(t, repo, home, "init-board")
	if got := envExitCode(ee); got != 5 {
		t.Fatalf("expected exit=5 for init-board on local backend, got %d; out=%s", got, out)
	}
	if parseEnvelope(t, out)["error"].(map[string]any)["code"] != "dependency_unhealthy" {
		t.Fatalf("expected dependency_unhealthy in %s", out)
	}
}

func TestWeaveE2E_Start_OutsideGitRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	// Use a tmp dir that's NOT a git repo.
	non := t.TempDir()
	home := t.TempDir()
	out, ee := runWeave(t, non, home, "add", "x")
	if got := envExitCode(ee); got != 3 {
		t.Fatalf("expected exit=3 (precondition_failed) outside git repo, got %d; out=%s", got, out)
	}
	if parseEnvelope(t, out)["error"].(map[string]any)["code"] != "precondition_failed" {
		t.Fatalf("expected precondition_failed in %s", out)
	}
}

func TestWeaveE2E_AddFromFile_Markdown(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	mdPath := filepath.Join(repo, "issues.md")
	md := strings.Join([]string{
		"# my backlog",
		"",
		"- [ ] first thing",
		"- [ ] second thing",
		"- [x] already done (still gets added; state is checkbox-agnostic)",
		"random line",
		"- not-a-checklist line",
	}, "\n")
	if err := os.WriteFile(mdPath, []byte(md), 0o644); err != nil {
		t.Fatalf("write md: %v", err)
	}
	out, ee := runWeave(t, repo, home, "add", "--from-file", mdPath, "--priority", "p1")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("add --from-file exited %d; out=%s", got, out)
	}
	res := parseEnvelope(t, out)["result"].(map[string]any)
	if int(res["count"].(float64)) != 3 {
		t.Fatalf("expected 3 entries parsed, got %v", res["count"])
	}
	added := res["added"].([]any)
	if added[0].(map[string]any)["priority"] != "p1" {
		t.Fatalf("expected priority=p1 (from --priority), got %v", added[0])
	}
}

func TestWeaveE2E_AddFromFile_JSON(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	jsonPath := filepath.Join(repo, "issues.json")
	jsonData := `[
	  {"title": "fix null deref", "priority": "p0"},
	  {"title": "refactor users", "body": "as discussed"}
	]`
	if err := os.WriteFile(jsonPath, []byte(jsonData), 0o644); err != nil {
		t.Fatalf("write json: %v", err)
	}
	out, ee := runWeave(t, repo, home, "add", "--from-file", jsonPath)
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("add --from-file json exited %d; out=%s", got, out)
	}
	res := parseEnvelope(t, out)["result"].(map[string]any)
	if int(res["count"].(float64)) != 2 {
		t.Fatalf("expected 2 entries parsed, got %v", res["count"])
	}
	added := res["added"].([]any)
	if added[0].(map[string]any)["priority"] != "p0" {
		t.Fatalf("expected first entry priority=p0 (from JSON), got %v", added[0])
	}
}

// TestWeaveE2E_Pull_NothingToMerge exercises the pull happy-path
// envelope when no working branches have commits ahead of main.
// The richer "ahead → merge → done" path needs a tool that actually
// commits; covered separately when we wire a deterministic committer.
func TestWeaveE2E_Pull_NothingToMerge(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "x"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	if _, ee := runWeave(t, repo, home, "start", "--no-spawn", "--issue", "1"); envExitCode(ee) != 0 {
		t.Fatalf("start failed")
	}
	out, ee := runWeave(t, repo, home, "pull")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("pull exited %d; out=%s", got, out)
	}
	res := parseEnvelope(t, out)["result"].(map[string]any)
	results, _ := res["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("expected 1 result row, got %d (%v)", len(results), res)
	}
	if results[0].(map[string]any)["status"] != "empty" {
		t.Fatalf("expected status=empty when no commits ahead, got %v", results[0])
	}
}

// TestWeaveE2E_Pull_MergesCommittedWork exercises the full happy
// path: add → start (with a tool that makes a commit) → pull → the
// merge lands on main, the item flips to "done", the worktree is
// torn down.
func TestWeaveE2E_Pull_MergesCommittedWork(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "add a file"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	// Tool that touches a file and commits inside the sandbox cwd.
	// `runWeaveStart` cd's into the sandbox before exec'ing, so the
	// commit lands on the agent/weave-issue-1 branch.
	script := `set -e; echo hi > new.txt; git add new.txt; git commit -qm "feat: add new.txt"`
	out, ee := runWeave(t, repo, home, "start", "--issue", "1", "--", "bash", "-c", script)
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("start with committer exited %d; out=%s", got, out)
	}
	out, ee = runWeave(t, repo, home, "pull")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("pull exited %d; out=%s", got, out)
	}
	res := parseEnvelope(t, out)["result"].(map[string]any)["results"].([]any)
	if len(res) != 1 || res[0].(map[string]any)["status"] != "merged" {
		t.Fatalf("expected one row with status=merged, got %v", res)
	}
	// Verify on disk: new.txt should exist on the repo's main HEAD.
	if _, err := os.Stat(filepath.Join(repo, "new.txt")); err != nil {
		t.Fatalf("expected new.txt on main after pull: %v", err)
	}
	// list with --history should show state=done.
	out, _ = runWeave(t, repo, home, "list", "--history")
	items := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)
	if items[0].(map[string]any)["state"] != "done" {
		t.Fatalf("expected state=done after pull, got %v", items[0])
	}
}

func TestWeaveE2E_NoSpawn_RequiresGitRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	non := t.TempDir()
	home := t.TempDir()
	out, ee := runWeave(t, non, home, "start", "--no-spawn", "--issue", "1")
	if got := envExitCode(ee); got != 3 {
		t.Fatalf("expected exit=3 (precondition_failed) outside git, got %d; out=%s", got, out)
	}
}

// startWeaveAsync launches `ycode weave <args...>` in the background
// without waiting; returns the *exec.Cmd so the caller can later
// Wait or Kill. Used by the wait-subverb tests that need a real
// long-running `weave start` to wait on.
func startWeaveAsync(t *testing.T, repo, home string, args ...string) *exec.Cmd {
	t.Helper()
	binAbs, err := filepath.Abs(weaveE2EBinary)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	full := append([]string{"weave"}, args...)
	cmd := exec.Command(binAbs, full...)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"TERM=dumb",
		"YCODE_NO_SERVER=1",
		"ANTHROPIC_API_KEY=",
		"OPENAI_API_KEY=",
		"YCODE_AGENT=1",
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start async weave: %v", err)
	}
	return cmd
}

func TestWeaveE2E_Wait_Issue_BlocksUntilSubmitted(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "sleeping tool"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	// Subagent takes ~2s to complete, in the background. wait
	// --issue 1 must return ok once state flips to "submitted".
	bg := startWeaveAsync(t, repo, home, "start", "--issue", "1", "--", "bash", "-c", "sleep 2; exit 0")
	defer bg.Wait()

	start := time.Now()
	out, ee := runWeave(t, repo, home, "wait", "--issue", "1", "--timeout", "10s")
	elapsed := time.Since(start)
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("wait --issue 1 exited %d after %s; out=%s", got, elapsed, out)
	}
	if elapsed < 1500*time.Millisecond {
		t.Fatalf("wait returned too early (%s); should have blocked for ~2s", elapsed)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("wait took too long (%s); subagent should have finished in ~2s", elapsed)
	}
	ready := parseEnvelope(t, out)["result"].(map[string]any)["ready"].([]any)
	if len(ready) != 1 {
		t.Fatalf("expected 1 ready item, got %v", ready)
	}
	if ready[0].(map[string]any)["state"] != "submitted" {
		t.Fatalf("expected state=submitted, got %v", ready[0])
	}
}

func TestWeaveE2E_Wait_All_BlocksUntilQueueDrains(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "first"); envExitCode(ee) != 0 {
		t.Fatalf("add 1 failed")
	}
	if _, ee := runWeave(t, repo, home, "add", "second"); envExitCode(ee) != 0 {
		t.Fatalf("add 2 failed")
	}
	bg1 := startWeaveAsync(t, repo, home, "start", "--issue", "1", "--", "bash", "-c", "sleep 1; exit 0")
	defer bg1.Wait()
	bg2 := startWeaveAsync(t, repo, home, "start", "--issue", "2", "--", "bash", "-c", "sleep 2; exit 0")
	defer bg2.Wait()

	out, ee := runWeave(t, repo, home, "wait", "--all", "--timeout", "10s")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("wait --all exited %d; out=%s", got, out)
	}
	ready := parseEnvelope(t, out)["result"].(map[string]any)["ready"].([]any)
	if len(ready) != 2 {
		t.Fatalf("expected 2 ready items, got %d (%v)", len(ready), ready)
	}
	for _, r := range ready {
		if r.(map[string]any)["state"] != "submitted" {
			t.Fatalf("expected all submitted, got %v", r)
		}
	}
}

func TestWeaveE2E_Wait_TimesOutWhenNeverCompletes(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "never finishes"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	// Long sleep we'll kill below; --no-spawn keeps state=working
	// without blocking the test so wait has something to wait on.
	if _, ee := runWeave(t, repo, home, "start", "--no-spawn", "--issue", "1"); envExitCode(ee) != 0 {
		t.Fatalf("start --no-spawn failed")
	}
	out, ee := runWeave(t, repo, home, "wait", "--issue", "1", "--timeout", "1s")
	if got := envExitCode(ee); got != 3 {
		t.Fatalf("expected exit=3 (precondition_failed) on timeout, got %d; out=%s", got, out)
	}
	errObj := parseEnvelope(t, out)["error"].(map[string]any)
	if errObj["code"] != "precondition_failed" {
		t.Fatalf("expected precondition_failed, got %v", errObj)
	}
	if !strings.Contains(errObj["message"].(string), "timeout") {
		t.Fatalf("expected 'timeout' in message, got %v", errObj["message"])
	}
}

func TestWeaveE2E_Wait_RequiresIssueOrAll(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	out, ee := runWeave(t, repo, home, "wait")
	if got := envExitCode(ee); got != 2 {
		t.Fatalf("expected exit=2 (invalid_arg) without --issue/--all, got %d; out=%s", got, out)
	}
}

// TestWeaveE2E_OrchestratorFlow exercises the full MVP scenario:
// orchestrator backgrounds two subagents, waits for both, pulls
// their work into main, and verifies both commits are merged.
// This is the headline test — if this passes, the documented
// orchestrator pattern actually works.
func TestWeaveE2E_OrchestratorFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "add alpha"); envExitCode(ee) != 0 {
		t.Fatalf("add 1 failed")
	}
	if _, ee := runWeave(t, repo, home, "add", "add beta"); envExitCode(ee) != 0 {
		t.Fatalf("add 2 failed")
	}
	// Two subagents in parallel: each touches a distinct file and
	// commits inside its own worktree.
	script1 := `set -e; echo a > alpha.txt; git add alpha.txt; git commit -qm "feat: alpha"; sleep 1`
	script2 := `set -e; echo b > beta.txt; git add beta.txt; git commit -qm "feat: beta"; sleep 1`
	bg1 := startWeaveAsync(t, repo, home, "start", "--issue", "1", "--", "bash", "-c", script1)
	bg2 := startWeaveAsync(t, repo, home, "start", "--issue", "2", "--", "bash", "-c", script2)
	defer bg1.Wait()
	defer bg2.Wait()

	out, ee := runWeave(t, repo, home, "wait", "--all", "--timeout", "15s")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("wait --all exited %d; out=%s", got, out)
	}
	out, ee = runWeave(t, repo, home, "pull")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("pull exited %d; out=%s", got, out)
	}
	results := parseEnvelope(t, out)["result"].(map[string]any)["results"].([]any)
	mergedCount := 0
	for _, r := range results {
		if r.(map[string]any)["status"] == "merged" {
			mergedCount++
		}
	}
	if mergedCount != 2 {
		t.Fatalf("expected 2 merged branches, got %d (%v)", mergedCount, results)
	}
	// Both commits should be on main.
	for _, f := range []string{"alpha.txt", "beta.txt"} {
		if _, err := os.Stat(filepath.Join(repo, f)); err != nil {
			t.Fatalf("expected %s on main: %v", f, err)
		}
	}
}
