//go:build e2e

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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
	return runWeaveEnv(t, repo, home, nil, args...)
}

func runWeaveEnv(t *testing.T, repo, home string, extraEnv []string, args ...string) (string, *exec.ExitError) {
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
	cmd.Env = append(cmd.Env, extraEnv...)
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

func runWeaveAsyncEnv(t *testing.T, repo, home string, extraEnv []string, args ...string) <-chan struct {
	out string
	ee  *exec.ExitError
} {
	t.Helper()
	ch := make(chan struct {
		out string
		ee  *exec.ExitError
	}, 1)
	go func() {
		out, ee := runWeaveEnv(t, repo, home, extraEnv, args...)
		ch <- struct {
			out string
			ee  *exec.ExitError
		}{out: out, ee: ee}
	}()
	return ch
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
	if res["state"] != "allocated" {
		t.Fatalf("expected state=allocated for --no-spawn, got %v", res["state"])
	}
	sandbox, _ := res["sandbox"].(string)
	if sandbox == "" {
		t.Fatalf("expected sandbox path in result; got %v", res)
	}
	if _, err := os.Stat(sandbox); err != nil {
		t.Fatalf("sandbox dir missing on disk: %v", err)
	}
	// The agent's branch lives in the sandbox clone, NOT in the
	// user's repo. This is the new isolation guarantee: the user's
	// repo refs are untouched until `weave pull` fetches.
	userBranchOut, _ := exec.Command("git", "-C", repo, "branch", "--list", "agent/weave-issue-1").CombinedOutput()
	if strings.Contains(string(userBranchOut), "agent/weave-issue-1") {
		t.Fatalf("isolation violation: agent branch leaked into user repo; got %q", userBranchOut)
	}
	sandboxBranchOut, err := exec.Command("git", "-C", sandbox, "branch", "--show-current").CombinedOutput()
	if err != nil || strings.TrimSpace(string(sandboxBranchOut)) != "agent/weave-issue-1" {
		t.Fatalf("expected sandbox checked out on agent/weave-issue-1; got %q (err=%v)", sandboxBranchOut, err)
	}

	// list shows allocated, with no stale flag (nothing is running).
	out, _ = runWeave(t, repo, home, "list")
	item := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if item["state"] != "allocated" {
		t.Fatalf("expected list to show state=allocated, got %v", item)
	}
	if stale, _ := item["stale"].(bool); stale {
		t.Fatalf("allocated item must not read as stale: %v", item)
	}
	// An allocated sandbox is assignable: resume launches a tool in it
	// and the normal lifecycle proceeds.
	out, ee = runWeave(t, repo, home, "start", "--issue", "1", "--resume", "--", "bash", "-c", "echo hi > a.txt; git add a.txt; git -c user.name=t -c user.email=t@t commit -qm a; exit 0")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("resume from allocated exited %d; out=%s", got, out)
	}
	out, _ = runWeave(t, repo, home, "list")
	item = parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if item["state"] != "submitted" {
		t.Fatalf("expected submitted after assigned run, got %v", item["state"])
	}
}

// TestWeaveE2E_SandboxIsolation_CannotMutateUserRepoRefs is the
// load-bearing test for the worktree → clone refactor. An agent
// inside the sandbox runs `git update-ref refs/heads/main HEAD`
// inside its own clone — that must NOT touch the user's repo
// ref. With the old `git worktree add` model, that command would
// mutate the SHARED ref and silently move main.
func TestWeaveE2E_SandboxIsolation_CannotMutateUserRepoRefs(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "isolation check"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	// Get the user repo's main commit before the agent runs.
	userMainBefore, err := exec.Command("git", "-C", repo, "rev-parse", "main").CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse main: %v", err)
	}
	// Subagent makes a commit then attempts to force-overwrite the
	// "main" ref to its own HEAD. In the new model that update-ref
	// happens in the sandbox's .git, NOT the user's.
	script := `set -e
echo poison > poison.txt
git add poison.txt
git commit -qm "agent: trying to overwrite main"
git update-ref refs/heads/main HEAD
echo done`
	if _, ee := runWeave(t, repo, home, "start", "--issue", "1", "--", "bash", "-c", script); envExitCode(ee) != 0 {
		t.Fatalf("start with poison script failed")
	}
	userMainAfter, err := exec.Command("git", "-C", repo, "rev-parse", "main").CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse main after: %v", err)
	}
	if strings.TrimSpace(string(userMainBefore)) != strings.TrimSpace(string(userMainAfter)) {
		t.Fatalf("isolation violation: user repo main moved from %s to %s",
			strings.TrimSpace(string(userMainBefore)), strings.TrimSpace(string(userMainAfter)))
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

func TestWeaveE2E_Start_MaxRuntimeTrapTermExitZeroRecordsKilledBy(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "watchdog provenance"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	script := `set -e
trap 'exit 0' TERM
echo done > watchdog.txt
git add watchdog.txt
git -c user.email=t@t -c user.name=t commit -qm "agent: commit before watchdog"
sleep 600 &
wait $!`
	out, ee := runWeave(t, repo, home, "start", "--issue", "1", "--max-runtime", "5s", "--", "bash", "-c", script)
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("start exited %d; out=%s", got, out)
	}
	out, _ = runWeave(t, repo, home, "list", "--history")
	item := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if item["state"] != "submitted" {
		t.Fatalf("expected trapped SIGTERM exit 0 to submit, got %v", item["state"])
	}
	killedBy, _ := item["killed_by"].(string)
	if !strings.Contains(killedBy, "runtime exceeds --max-runtime") {
		t.Fatalf("expected killed_by to record max-runtime watchdog, got %q; item=%v", killedBy, item)
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
	if _, ee := runWeave(t, repo, home, "start", "--issue", "1", "--", "bash", "-c", "exit 0"); envExitCode(ee) != 0 {
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

// TestWeaveE2E_Pull_IssueArgSkipsLiveWorkingBystander reproduces a
// dogfood incident where `weave pull <other-issue>` still swept a
// live working item, fetched its stale sandbox branch, and removed
// the sandbox under the running wrapper.
func TestWeaveE2E_Pull_IssueArgSkipsLiveWorkingBystander(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "submitted green"); envExitCode(ee) != 0 {
		t.Fatalf("add issue 1 failed")
	}
	if _, ee := runWeave(t, repo, home, "add", "live retry with stale commit"); envExitCode(ee) != 0 {
		t.Fatalf("add issue 2 failed")
	}

	liveScript := `set -e; echo stale > stale.txt; git add stale.txt; git commit -qm "feat: stale retry commit"; touch committed.ready; sleep 600`
	bg := startWeaveAsync(t, repo, home, "start", "--issue", "2", "--", "bash", "-c", liveScript)
	defer func() { _ = bg.Process.Kill(); _ = bg.Wait() }()

	var liveSandbox string
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		item := weaveIssueItem(t, repo, home, 2)
		liveSandbox, _ = item["sandbox"].(string)
		if pidF, ok := item["wrapper_pid"].(float64); ok && pidF > 0 && liveSandbox != "" {
			if _, err := os.Stat(filepath.Join(liveSandbox, "committed.ready")); err == nil {
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if liveSandbox == "" {
		t.Fatalf("issue 2 never recorded a sandbox")
	}
	if _, err := os.Stat(filepath.Join(liveSandbox, "committed.ready")); err != nil {
		t.Fatalf("issue 2 stale commit marker never appeared: %v", err)
	}

	greenScript := `set -e; echo green > green.txt; git add green.txt; git commit -qm "feat: green"`
	out, ee := runWeave(t, repo, home, "start", "--issue", "1", "--", "bash", "-c", greenScript)
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("start issue 1 exited %d; out=%s", got, out)
	}

	out, ee = runWeave(t, repo, home, "pull", "1")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("pull issue 1 exited %d; out=%s", got, out)
	}
	results := parseEnvelope(t, out)["result"].(map[string]any)["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("targeted pull should only return issue 1, got %v", results)
	}
	row := results[0].(map[string]any)
	if int(row["issue"].(float64)) != 1 || row["status"] != "merged" {
		t.Fatalf("expected issue 1 merged, got %v", row)
	}
	if _, err := os.Stat(filepath.Join(repo, "green.txt")); err != nil {
		t.Fatalf("expected green.txt on main after targeted pull: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "stale.txt")); !os.IsNotExist(err) {
		t.Fatalf("targeted pull should not merge stale.txt; stat err=%v", err)
	}
	if _, err := os.Stat(liveSandbox); err != nil {
		t.Fatalf("live issue 2 sandbox should remain after targeted pull: %v", err)
	}

	out, ee = runWeave(t, repo, home, "pull")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("blanket pull exited %d; out=%s", got, out)
	}
	results = parseEnvelope(t, out)["result"].(map[string]any)["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("blanket pull should only report live issue 2, got %v", results)
	}
	row = results[0].(map[string]any)
	if int(row["issue"].(float64)) != 2 || row["status"] != "running" {
		t.Fatalf("expected issue 2 running, got %v", row)
	}
	if detail, _ := row["detail"].(string); !strings.Contains(detail, "wrapper pid") || !strings.Contains(detail, "alive; wait or kill before pull") {
		t.Fatalf("expected live-wrapper detail, got %v", row)
	}
	if _, err := os.Stat(liveSandbox); err != nil {
		t.Fatalf("live issue 2 sandbox should remain after blanket pull: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "stale.txt")); !os.IsNotExist(err) {
		t.Fatalf("blanket pull should not merge stale.txt; stat err=%v", err)
	}

	if _, ee := runWeave(t, repo, home, "kill", "2", "--reason", "cleanup"); envExitCode(ee) != 0 {
		t.Fatalf("cleanup kill failed")
	}
	_ = bg.Wait()
}

// TestWeaveE2E_Kill stops a running subagent precisely without
// removing its sandbox or branch, then verifies the partial work
// is preserved for later inspection / resume.
func TestWeaveE2E_Kill(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "long-running thing"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	// Subagent commits a partial file then sleeps a long time —
	// simulates the "stuck mid-edit" case the dogfood found.
	script := `set -e
echo partial > partial.txt
git add partial.txt
git commit -qm "partial work"
sleep 600`
	bg := startWeaveAsync(t, repo, home, "start", "--issue", "1", "--", "bash", "-c", script)

	// Wait for the wrapper PID to land in the queue.
	deadline := time.Now().Add(15 * time.Second)
	var wrapperPid int
	for time.Now().Before(deadline) {
		out, _ := runWeave(t, repo, home, "list", "--json")
		items := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)
		if len(items) > 0 {
			if pidF, ok := items[0].(map[string]any)["wrapper_pid"].(float64); ok && pidF > 0 {
				wrapperPid = int(pidF)
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if wrapperPid == 0 {
		_ = bg.Process.Kill()
		bg.Wait()
		t.Fatalf("wrapper_pid never landed in queue")
	}

	out, ee := runWeave(t, repo, home, "kill", "1", "--reason", "test-kill")
	if got := envExitCode(ee); got != 0 {
		_ = bg.Process.Kill()
		bg.Wait()
		t.Fatalf("kill exited %d; out=%s", got, out)
	}
	res := parseEnvelope(t, out)["result"].(map[string]any)
	if res["state"] != "killed" {
		t.Fatalf("expected state=killed after forced kill, got %v", res["state"])
	}
	if int(res["wrapper_pid"].(float64)) != wrapperPid {
		t.Fatalf("kill envelope reports wrong wrapper_pid: %v vs %d", res["wrapper_pid"], wrapperPid)
	}

	// Reap the bg process so the test doesn't dangle.
	_ = bg.Wait()

	// Sandbox + partial commit must still be there.
	out, _ = runWeave(t, repo, home, "list", "--history")
	item := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)[0].(map[string]any)
	sandbox := item["sandbox"].(string)
	if _, err := os.Stat(filepath.Join(sandbox, "partial.txt")); err != nil {
		t.Fatalf("partial work lost after kill: %v", err)
	}
}

func TestWeaveE2E_Kill_VerifiesCommittedWork(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	verify := `test -f partial.txt && echo killed-verify-ok`
	if _, ee := runWeave(t, repo, home, "add", "verified killed work", "--verify", verify); envExitCode(ee) != 0 {
		t.Fatalf("add --verify failed")
	}
	script := `set -e
echo partial > partial.txt
git add partial.txt
git commit -qm "partial work"
sleep 600`
	bg := startWeaveAsync(t, repo, home, "start", "--issue", "1", "--", "bash", "-c", script)

	if pid := waitIssueWrapperPid(t, repo, home); pid == 0 {
		_ = bg.Process.Kill()
		bg.Wait()
		t.Fatalf("wrapper_pid never landed in queue")
	}
	out, ee := runWeave(t, repo, home, "kill", "1", "--reason", "test-kill")
	if got := envExitCode(ee); got != 0 {
		_ = bg.Process.Kill()
		bg.Wait()
		t.Fatalf("kill exited %d; out=%s", got, out)
	}
	res := parseEnvelope(t, out)["result"].(map[string]any)
	if res["state"] != "killed" {
		t.Fatalf("expected state=killed after forced kill, got %v", res["state"])
	}
	ve, ok := res["verify_exit"].(float64)
	if !ok || int(ve) != 0 {
		t.Fatalf("kill envelope should report verify_exit=0, got %v (res=%v)", res["verify_exit"], res)
	}
	if !strings.Contains(fmt.Sprint(res["verify_output"]), "killed-verify-ok") {
		t.Fatalf("kill envelope should include verify output, got %q", res["verify_output"])
	}
	_ = bg.Wait()

	item := weaveIssueItem(t, repo, home, 1)
	if item["state"] != "killed" {
		t.Fatalf("expected persisted state=killed, got %v", item["state"])
	}
	ve, ok = item["verify_exit"].(float64)
	if !ok || int(ve) != 0 {
		t.Fatalf("expected persisted verify_exit=0, got %v (item=%v)", item["verify_exit"], item)
	}
	if item["verify_tree"] != "head" {
		t.Fatalf("expected verify_tree=head, got %v", item["verify_tree"])
	}
	if item["dirty"] != false {
		t.Fatalf("expected clean tree for committed killed work, item=%v", item)
	}
	if !strings.Contains(fmt.Sprint(item["verify_output"]), "killed-verify-ok") {
		t.Fatalf("expected persisted verify output, got %q", item["verify_output"])
	}
}

func TestWeaveE2E_Kill_SkipsVerifyWithoutEvidence(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "clean killed work", "--verify", "echo should-not-run"); envExitCode(ee) != 0 {
		t.Fatalf("add --verify failed")
	}
	bg := startWeaveAsync(t, repo, home, "start", "--issue", "1", "--", "bash", "-c", "sleep 600")

	if pid := waitIssueWrapperPid(t, repo, home); pid == 0 {
		_ = bg.Process.Kill()
		bg.Wait()
		t.Fatalf("wrapper_pid never landed in queue")
	}
	out, ee := runWeave(t, repo, home, "kill", "1", "--reason", "test-clean-kill")
	if got := envExitCode(ee); got != 0 {
		_ = bg.Process.Kill()
		bg.Wait()
		t.Fatalf("kill exited %d; out=%s", got, out)
	}
	res := parseEnvelope(t, out)["result"].(map[string]any)
	if _, ok := res["verify_exit"]; ok {
		t.Fatalf("kill envelope should omit verify_exit for clean zero-commit kill, got %v", res)
	}
	_ = bg.Wait()

	item := weaveIssueItem(t, repo, home, 1)
	if item["state"] != "killed" {
		t.Fatalf("expected state=killed, got %v", item["state"])
	}
	if _, ok := item["verify_exit"]; ok {
		t.Fatalf("verify should be skipped for clean zero-commit kill, item=%v", item)
	}
	if got, _ := item["commits_ahead"].(float64); int(got) != 0 {
		t.Fatalf("expected commits_ahead=0, got %v", item["commits_ahead"])
	}
	if item["dirty"] != false {
		t.Fatalf("expected clean tree, item=%v", item)
	}
}

// TestWeaveE2E_Kill_RequiresWorking refuses if the item isn't
// currently working.
func TestWeaveE2E_Kill_RequiresWorking(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "x"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	out, ee := runWeave(t, repo, home, "kill", "1")
	if got := envExitCode(ee); got != 4 {
		t.Fatalf("expected exit=4 (state_conflict) for kill on non-working, got %d; out=%s", got, out)
	}
}

func TestWeaveE2E_Kill_NotFoundHintsOtherActiveQueue(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "active elsewhere"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	if out, ee := runWeave(t, repo, home, "start", "--no-spawn", "--issue", "1"); envExitCode(ee) != 0 {
		t.Fatalf("start --no-spawn failed: out=%s", out)
	}

	otherRepo := t.TempDir()
	cmds := [][]string{
		{"git", "init", "-q", "-b", "main"},
		{"git", "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-qm", "seed"},
	}
	for _, c := range cmds {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = otherRepo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}

	out, ee := runWeave(t, otherRepo, home, "kill", "1")
	if got := envExitCode(ee); got != 2 {
		t.Fatalf("expected exit=2 (invalid_arg) for wrong-repo kill, got %d; out=%s", got, out)
	}
	env := parseEnvelope(t, out)
	if env["status"] != "error" {
		t.Fatalf("expected status=error, got %v", env["status"])
	}
	errObj := env["error"].(map[string]any)
	if errObj["code"] != "invalid_arg" {
		t.Fatalf("expected error.code=invalid_arg, got %v", errObj["code"])
	}
	msg, _ := errObj["message"].(string)
	if !strings.Contains(msg, "queues are per-repo") || !strings.Contains(msg, repo+" (1)") {
		t.Fatalf("expected wrong-repo hint to mention %s active queue; msg=%q out=%s", repo, msg, out)
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

// ─── Watchdog / guard tests (post-OOM hardening) ──────────────────
//
// Background: the 2026-06-10 dogfood OOM. A weave-launched claude
// TUI survived `weave abandon` because the wrapper died on SIGTERM
// without forwarding it to the subagent (its own session, courtesy
// of pty.Start), and a `--resume` retry recorded no fresh
// wrapper_pid, so `weave kill` aimed at a dead PID. These tests pin
// the four fixes: signal forwarding kills the whole subagent tree,
// duplicate wrappers are refused, stale "working" items are flagged,
// and the wall-clock + memory watchdogs fire.

// waitIssueWrapperPid polls `weave list --json` until issue 1 has a
// non-zero wrapper_pid, returning it (0 on timeout).
func waitIssueWrapperPid(t *testing.T, repo, home string) int {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		out, _ := runWeave(t, repo, home, "list", "--json")
		items := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)
		if len(items) > 0 {
			if pidF, ok := items[0].(map[string]any)["wrapper_pid"].(float64); ok && pidF > 0 {
				return int(pidF)
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return 0
}

// pidGone reports whether the pid no longer exists (signal-0 probe
// via the kill binary, keeping this file syscall-free).
func pidGone(pid int) bool {
	return exec.Command("kill", "-0", strconv.Itoa(pid)).Run() != nil
}

// issueSandbox returns issue 1's sandbox path from the queue.
func issueSandbox(t *testing.T, repo, home string) string {
	t.Helper()
	item := weaveIssueItem(t, repo, home, 1)
	sb, _ := item["sandbox"].(string)
	return sb
}

func weaveIssueItem(t *testing.T, repo, home string, id int) map[string]any {
	t.Helper()
	out, _ := runWeave(t, repo, home, "list", "--json")
	items := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)
	if len(items) == 0 {
		t.Fatalf("no items in queue")
	}
	for _, raw := range items {
		item := raw.(map[string]any)
		if int(item["id"].(float64)) == id {
			return item
		}
	}
	t.Fatalf("issue %d not found in queue: %v", id, items)
	return nil
}

// TestWeaveE2E_Kill_TerminatesSubagentTree asserts `weave kill`
// reaches the subagent AND its children — not just the wrapper. The
// subagent records its own PID and a background child's PID into the
// sandbox; after kill, both must be gone.
func TestWeaveE2E_Kill_TerminatesSubagentTree(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "tree-kill target"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	script := `echo $$ > pid.txt
sleep 600 &
echo $! > childpid.txt
wait`
	bg := startWeaveAsync(t, repo, home, "start", "--issue", "1", "--", "bash", "-c", script)
	defer func() { _ = bg.Process.Kill(); _ = bg.Wait() }()

	if pid := waitIssueWrapperPid(t, repo, home); pid == 0 {
		t.Fatalf("wrapper_pid never landed in queue")
	}
	sandbox := issueSandbox(t, repo, home)

	// Wait for the subagent to write both PID files.
	var toolPid, childPid int
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		b1, err1 := os.ReadFile(filepath.Join(sandbox, "pid.txt"))
		b2, err2 := os.ReadFile(filepath.Join(sandbox, "childpid.txt"))
		if err1 == nil && err2 == nil {
			toolPid, _ = strconv.Atoi(strings.TrimSpace(string(b1)))
			childPid, _ = strconv.Atoi(strings.TrimSpace(string(b2)))
			if toolPid > 0 && childPid > 0 {
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if toolPid == 0 || childPid == 0 {
		t.Fatalf("subagent never wrote pid files")
	}

	if out, ee := runWeave(t, repo, home, "kill", "1", "--reason", "tree-kill test"); envExitCode(ee) != 0 {
		t.Fatalf("kill failed: %s", out)
	}

	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if pidGone(toolPid) && pidGone(childPid) {
			_ = bg.Wait()
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("subagent tree survived kill: tool(%d) gone=%v child(%d) gone=%v",
		toolPid, pidGone(toolPid), childPid, pidGone(childPid))
}

// TestWeaveE2E_Start_RefusesLiveDuplicateWrapper pins the resume
// guard: while a wrapper is alive on an issue, a second
// `weave start --resume` for the same issue must refuse with a
// state-conflict instead of racing two agents in one sandbox.
func TestWeaveE2E_Start_RefusesLiveDuplicateWrapper(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "dup guard"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	bg := startWeaveAsync(t, repo, home, "start", "--issue", "1", "--", "bash", "-c", "sleep 600")
	defer func() { _ = bg.Process.Kill(); _ = bg.Wait() }()

	if pid := waitIssueWrapperPid(t, repo, home); pid == 0 {
		t.Fatalf("wrapper_pid never landed in queue")
	}
	out, ee := runWeave(t, repo, home, "start", "--resume", "--issue", "1", "--", "bash", "-c", "true")
	if got := envExitCode(ee); got != 4 {
		t.Fatalf("expected exit=4 (state_conflict) for duplicate start, got %d; out=%s", got, out)
	}
	if !strings.Contains(out, "live wrapper") {
		t.Fatalf("expected 'live wrapper' in error, got: %s", out)
	}
	if _, ee := runWeave(t, repo, home, "kill", "1", "--reason", "cleanup"); envExitCode(ee) != 0 {
		t.Fatalf("cleanup kill failed")
	}
	_ = bg.Wait()
}

// TestWeaveE2E_List_MarksStaleWorking: a "working" item whose
// wrapper is dead (here: --no-spawn, whose wrapper exits
// immediately) must carry stale=true so orchestrators can tell a
// crashed agent from a live one.
func TestWeaveE2E_List_MarksStaleWorking(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "stale detect"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	bg := startWeaveAsync(t, repo, home, "start", "--issue", "1", "--", "bash", "-c", "sleep 600")
	// Wait for the wrapper pid, then kill it RAW (kill -9, bypassing
	// weave) — the crash case stale detection exists for.
	var wrapperPid int
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		out, _ := runWeave(t, repo, home, "list", "--json")
		items := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)
		if len(items) > 0 {
			if pidF, ok := items[0].(map[string]any)["wrapper_pid"].(float64); ok && pidF > 0 {
				wrapperPid = int(pidF)
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if wrapperPid == 0 {
		_ = bg.Process.Kill()
		bg.Wait()
		t.Fatalf("wrapper pid never appeared")
	}
	_ = syscall.Kill(wrapperPid, syscall.SIGKILL)
	_ = bg.Wait()
	time.Sleep(500 * time.Millisecond)
	out, _ := runWeave(t, repo, home, "list", "--json")
	item := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if item["state"] != "working" {
		t.Fatalf("expected state=working after raw wrapper kill, got %v", item["state"])
	}
	if stale, _ := item["stale"].(bool); !stale {
		t.Fatalf("expected stale=true for working item with dead wrapper; item=%v", item)
	}
}

// TestWeaveE2E_MaxRuntime_KillsSpinningSubagent: --idle-timeout is
// useless against a runaway TUI whose spinner keeps emitting output
// (the dogfood OOM ran 16 minutes under --idle-timeout 7m).
// --max-runtime must kill it regardless of output activity.
func TestWeaveE2E_MaxRuntime_KillsSpinningSubagent(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "spinner"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	bg := startWeaveAsync(t, repo, home, "start", "--issue", "1", "--max-runtime", "3s",
		"--", "bash", "-c", "while :; do echo spin; sleep 0.1; done")
	defer func() { _ = bg.Process.Kill(); _ = bg.Wait() }()

	out, ee := runWeave(t, repo, home, "wait", "--issue", "1", "--timeout", "60s", "--json")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("wait exited %d; out=%s", got, out)
	}
	ready := parseEnvelope(t, out)["result"].(map[string]any)["ready"].([]any)[0].(map[string]any)
	if ready["state"] != "killed" {
		t.Fatalf("expected state=killed after max-runtime kill, got %v", ready["state"])
	}
	_ = bg.Wait()
}

// TestWeaveE2E_MemLimit_KillsRunawaySubagent: the OOM backstop. A
// subagent that balloons past --mem-limit must die with the reason
// recorded in its log, instead of eating the machine.
func TestWeaveE2E_MemLimit_KillsRunawaySubagent(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "memory hog"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	// 1MB seed doubled 9 times ≈ 512MB resident, then hold. Bounded
	// by construction — even if the watchdog were broken, the test
	// costs ~0.5GB, not the machine.
	hog := `x=$(head -c 1000000 /dev/zero | tr '\0' a)
for i in 1 2 3 4 5 6 7 8 9; do x="$x$x"; done
sleep 600`
	bg := startWeaveAsync(t, repo, home, "start", "--issue", "1", "--mem-limit", "64m",
		"--", "bash", "-c", hog)
	defer func() { _ = bg.Process.Kill(); _ = bg.Wait() }()

	out, ee := runWeave(t, repo, home, "wait", "--issue", "1", "--timeout", "60s", "--json")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("wait exited %d; out=%s", got, out)
	}
	ready := parseEnvelope(t, out)["result"].(map[string]any)["ready"].([]any)[0].(map[string]any)
	if ready["state"] != "killed" {
		t.Fatalf("expected state=killed after mem-limit kill, got %v", ready["state"])
	}
	if logPath, _ := ready["log_path"].(string); logPath != "" {
		b, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatalf("read log: %v", err)
		}
		if !strings.Contains(string(b), "mem-limit") {
			t.Fatalf("expected mem-limit kill reason in log %s", logPath)
		}
		if !strings.Contains(string(b), "top system procs") {
			t.Fatalf("expected forensic top-procs snapshot in log %s", logPath)
		}
	}
	_ = bg.Wait()
}

// TestWeaveE2E_Log_PrintsAndTailsCapture: `weave log` prints the
// PTY capture recorded by a non-interactive start, honors --tail,
// and returns metadata (not the raw stream) in agent/JSON mode.
func TestWeaveE2E_Log_PrintsAndTailsCapture(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "log capture check"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	// Non-TTY parent → PTY output captured to the per-issue log.
	script := `for i in 1 2 3 4 5; do echo "line-$i"; done`
	if out, ee := runWeave(t, repo, home, "start", "--issue", "1", "--", "bash", "-c", script); envExitCode(ee) != 0 {
		t.Fatalf("start failed: %s", out)
	}
	// JSON mode (YCODE_AGENT=1 in runWeave env) → metadata envelope.
	out, ee := runWeave(t, repo, home, "log", "1")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("log exited %d; out=%s", got, out)
	}
	res := parseEnvelope(t, out)["result"].(map[string]any)
	logPath, _ := res["log_path"].(string)
	if logPath == "" {
		t.Fatalf("expected log_path in result; got %v", res)
	}
	if size, _ := res["size_bytes"].(float64); size <= 0 {
		t.Fatalf("expected non-empty capture; got %v", res)
	}
	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	for _, want := range []string{"line-1", "line-5"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("capture missing %q: %q", want, b)
		}
	}
	// Human mode → raw stream on stdout, --tail limits it. Run the
	// binary directly: YCODE_AGENT=1 (set by runWeave) forces JSON
	// regardless of flags, so this leg needs a clean env.
	binAbs, err := filepath.Abs(weaveE2EBinary)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	raw := exec.Command(binAbs, "weave", "log", "1", "--plain", "-n", "2")
	raw.Dir = repo
	raw.Env = append(os.Environ(), "HOME="+home, "TERM=dumb", "YCODE_NO_SERVER=1", "YCODE_AGENT=")
	rawOut, err := raw.CombinedOutput()
	if err != nil {
		t.Fatalf("log --plain: %v\n%s", err, rawOut)
	}
	if strings.Contains(string(rawOut), "line-1") || !strings.Contains(string(rawOut), "line-5") {
		t.Fatalf("tail window wrong; out=%q", rawOut)
	}
}

// TestWeaveE2E_Log_NoCapture: an issue that never started has no
// log_path → state_conflict, not a crash.
func TestWeaveE2E_Log_NoCapture(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "never started"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	out, ee := runWeave(t, repo, home, "log", "1")
	if got := envExitCode(ee); got != 4 {
		t.Fatalf("expected state_conflict (4), got %d; out=%s", got, out)
	}
}

// TestWeaveE2E_Sandbox_OriginRemoved: the sandbox clone must not
// carry a usable `origin` remote — in dogfooding a subagent followed
// origin's URL back to the user's checkout and committed to its
// master directly. Escape-hatch closed = `git remote` empty.
func TestWeaveE2E_Sandbox_OriginRemoved(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "origin removal check"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	out, ee := runWeave(t, repo, home, "start", "--no-spawn", "--issue", "1")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("start --no-spawn exited %d; out=%s", got, out)
	}
	sandbox, _ := parseEnvelope(t, out)["result"].(map[string]any)["sandbox"].(string)
	remotes, err := exec.Command("git", "-C", sandbox, "remote").CombinedOutput()
	if err != nil {
		t.Fatalf("git remote: %v", err)
	}
	if strings.TrimSpace(string(remotes)) != "" {
		t.Fatalf("sandbox still has remotes (escape hatch open): %q", remotes)
	}
}

// TestWeaveE2E_Resume_AfterFailedRun: a non-zero tool exit leaves
// state=failed with the sandbox preserved — the documented retry
// path (`weave kill` → `weave start --resume`) must accept it,
// flip the queue back to working for the duration of the retry
// (so list/wait don't act on the stale terminal state), and land
// on submitted when the retry exits 0.
func TestWeaveE2E_Resume_AfterFailedRun(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "retry path"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	// First run fails (exit 7) → state=failed, sandbox kept.
	if out, ee := runWeave(t, repo, home, "start", "--issue", "1", "--", "bash", "-c", "echo partial > work.txt; exit 7"); envExitCode(ee) == 0 {
		t.Fatalf("expected non-zero wrapper exit; out=%s", out)
	}
	// Resume in the same sandbox. The script holds the run open
	// long enough for the test to observe the mid-run state, then
	// commits the prior partial work and exits 0.
	script := `test -f work.txt || exit 9
sleep 3
git add work.txt
git -c user.name=r -c user.email=r@r commit -qm retry
exit 0`
	bg := startWeaveAsync(t, repo, home, "start", "--issue", "1", "--resume", "--", "bash", "-c", script)

	// Mid-run: the stale failed state must flip back to working.
	deadline := time.Now().Add(15 * time.Second)
	sawWorking := false
	for time.Now().Before(deadline) {
		out, _ := runWeave(t, repo, home, "list", "--json")
		items := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)
		if len(items) > 0 && items[0].(map[string]any)["state"] == "working" {
			sawWorking = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !sawWorking {
		_ = bg.Process.Kill()
		bg.Wait()
		t.Fatalf("resumed issue never flipped back to state=working")
	}
	_ = bg.Wait()

	out, _ := runWeave(t, repo, home, "list")
	items := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)
	item := items[0].(map[string]any)
	if item["state"] != "submitted" {
		t.Fatalf("expected state=submitted after resumed retry, got %v", item["state"])
	}
	if _, hasExit := item["exit_code"]; hasExit {
		if ec := item["exit_code"].(float64); ec != 0 {
			t.Fatalf("expected exit_code=0 after resumed retry, got %v", ec)
		}
	}
}

// TestWeaveE2E_Say_InjectsIntoSubagentPTY: `weave say` writes a line
// into the running subagent's PTY via the wrapper's control socket.
// The subagent is a bash script that blocks on `read` — the injected
// text arrives as keystrokes, the script echoes it back, and the
// echo lands in the PTY capture.
func TestWeaveE2E_Say_InjectsIntoSubagentPTY(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "say check"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	script := `echo ready
read -r line
echo "received:$line"
exit 0`
	bg := startWeaveAsync(t, repo, home, "start", "--issue", "1", "--", "bash", "-c", script)

	// Wait until the wrapper records its control socket.
	deadline := time.Now().Add(15 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		out, _ := runWeave(t, repo, home, "list", "--json")
		items := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)
		if len(items) > 0 {
			item := items[0].(map[string]any)
			sock, _ := item["ctl_sock"].(string)
			if pidF, ok := item["wrapper_pid"].(float64); ok && pidF > 0 && sock != "" {
				if _, err := os.Stat(sock); err == nil {
					ready = true
					break
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !ready {
		_ = bg.Process.Kill()
		bg.Wait()
		t.Fatalf("control socket never appeared in queue")
	}

	if out, ee := runWeave(t, repo, home, "say", "1", "hello", "from", "the", "outside"); envExitCode(ee) != 0 {
		_ = bg.Process.Kill()
		bg.Wait()
		t.Fatalf("say failed: %s", out)
	}
	_ = bg.Wait()

	// The echo must be in the capture, and the run must have ended
	// submitted (read returned, script exited 0).
	out, _ := runWeave(t, repo, home, "list", "--json")
	item := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if item["state"] != "submitted" {
		t.Fatalf("expected submitted after say-unblocked exit, got %v", item["state"])
	}
	b, err := os.ReadFile(item["log_path"].(string))
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	if !strings.Contains(string(b), "received:hello from the outside") {
		t.Fatalf("injected line never echoed back; capture=%q", b)
	}
}

// TestWeaveE2E_Say_RequiresLiveSubagent: say against a never-started
// issue is a state_conflict, not a hang or crash.
func TestWeaveE2E_Say_RequiresLiveSubagent(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "nobody home"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	out, ee := runWeave(t, repo, home, "say", "1", "anyone there?")
	if got := envExitCode(ee); got != 4 {
		t.Fatalf("expected state_conflict (4), got %d; out=%s", got, out)
	}
}

// TestWeaveE2E_Sandbox_NoOriginBreadcrumbs: enforcement, not advice —
// a subagent must not be able to LEARN the origin repo's path from
// anything weave hands it. Two escapes taught us the vectors: the
// origin remote (removed earlier), the clone reflog ("clone: from
// <path>"), the inherited PWD/OLDPWD env, and the queue tag spelling
// the path. The subagent here dumps its env; the test then scans the
// dump plus every byte of the sandbox (including .git) and the
// sandbox's own path for the origin location.
func TestWeaveE2E_Sandbox_NoOriginBreadcrumbs(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "containment probe"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	// The subagent records its environment from inside the sandbox.
	if out, ee := runWeave(t, repo, home, "start", "--issue", "1", "--", "bash", "-c", "env > envdump.txt; pwd >> envdump.txt"); envExitCode(ee) != 0 {
		t.Fatalf("start failed: %s", out)
	}
	out, _ := runWeave(t, repo, home, "list", "--json")
	item := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)[0].(map[string]any)
	sandbox := item["sandbox"].(string)

	// Vector 0: the sandbox path itself must not contain the origin
	// path (the queue tag used to spell it out).
	if strings.Contains(sandbox, strings.TrimPrefix(repo, string(filepath.Separator))) ||
		strings.Contains(sandbox, repo) {
		t.Fatalf("sandbox path leaks origin location: %s (origin %s)", sandbox, repo)
	}

	// Vectors 1-3: no file in the sandbox — env dump, git config,
	// reflogs, packed refs, anything — may contain the origin path.
	leaks := 0
	_ = filepath.Walk(sandbox, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if strings.Contains(string(b), repo) {
			t.Errorf("origin path found in %s", path)
			leaks++
		}
		return nil
	})
	if leaks > 0 {
		t.Fatalf("%d files leak the origin path", leaks)
	}
}

// TestWeaveE2E_Pull_RemovesSandboxAfterMerge verifies that after a
// successful pull, the merged sandbox directory is properly removed
// (not using git worktree remove, which doesn't work for full clones).
func TestWeaveE2E_Pull_RemovesSandboxAfterMerge(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "add a file for merge"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}

	// Tool creates a commit in the sandbox
	script := `set -e; echo merged-content > merged.txt; git add merged.txt; git commit -qm "feat: add merged.txt"`
	out, ee := runWeave(t, repo, home, "start", "--issue", "1", "--", "bash", "-c", script)
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("start with committer exited %d; out=%s", got, out)
	}

	// Get sandbox path before pull
	out, _ = runWeave(t, repo, home, "list", "--history")
	item := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)[0].(map[string]any)
	sandbox := item["sandbox"].(string)
	if sandbox == "" {
		t.Fatalf("expected sandbox path on item; got %v", item)
	}

	// Verify sandbox exists before pull
	if _, err := os.Stat(sandbox); err != nil {
		t.Fatalf("sandbox should exist before pull: %v", err)
	}

	// Pull merges the work
	out, ee = runWeave(t, repo, home, "pull")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("pull exited %d; out=%s", got, out)
	}

	// Verify sandbox is GONE after pull
	if _, err := os.Stat(sandbox); !os.IsNotExist(err) {
		t.Fatalf("sandbox dir should be removed after pull, but still exists: %s", sandbox)
	}

	// Verify the merged file is on main
	if _, err := os.Stat(filepath.Join(repo, "merged.txt")); err != nil {
		t.Fatalf("expected merged.txt on main after pull: %v", err)
	}

	// Verify item is now "done"
	out, _ = runWeave(t, repo, home, "list", "--history")
	item = parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if item["state"] != "done" {
		t.Fatalf("expected state=done after pull, got %v", item["state"])
	}
}

// TestWeaveE2E_Pull_DoesNotClobberConcurrentStartAllocation pins a
// production data-loss race: pull loaded queue.json, merged one
// submitted issue, then saved that stale full queue after a concurrent
// start --no-spawn had recorded another issue's sandbox.
func TestWeaveE2E_Pull_DoesNotClobberConcurrentStartAllocation(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "allocate while pull runs"); envExitCode(ee) != 0 {
		t.Fatalf("add 1 failed")
	}
	if _, ee := runWeave(t, repo, home, "add", "submitted merge target"); envExitCode(ee) != 0 {
		t.Fatalf("add 2 failed")
	}
	script := `set -e; echo merged > pull-race.txt; git add pull-race.txt; git commit -qm "feat: pull race"`
	if out, ee := runWeave(t, repo, home, "start", "--issue", "2", "--", "bash", "-c", script); envExitCode(ee) != 0 {
		t.Fatalf("start issue 2 failed: %s", out)
	}

	pause := filepath.Join(t.TempDir(), "pull-after-load.pause")
	if err := os.WriteFile(pause, []byte("pause"), 0o644); err != nil {
		t.Fatalf("write pause file: %v", err)
	}
	pullDone := runWeaveAsyncEnv(t, repo, home, []string{"YCODE_WEAVE_TEST_PULL_AFTER_LOAD_FILE=" + pause}, "pull")
	ready := pause + ".ready"
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pull did not reach after-load pause")
		}
		time.Sleep(50 * time.Millisecond)
	}

	startDone := runWeaveAsyncEnv(t, repo, home, nil, "start", "--no-spawn", "--issue", "1")
	var startRes struct {
		out string
		ee  *exec.ExitError
	}
	startFinished := false
	select {
	case startRes = <-startDone:
		startFinished = true
	case <-time.After(10 * time.Second):
	}
	if err := os.Remove(pause); err != nil && !os.IsNotExist(err) {
		t.Fatalf("release pause: %v", err)
	}

	if !startFinished {
		startRes = <-startDone
	}
	if got := envExitCode(startRes.ee); got != 0 {
		t.Fatalf("start --no-spawn exited %d; out=%s", got, startRes.out)
	}
	pullRes := <-pullDone
	if got := envExitCode(pullRes.ee); got != 0 {
		t.Fatalf("pull exited %d; out=%s", got, pullRes.out)
	}

	out, _ := runWeave(t, repo, home, "list", "--history")
	items := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)
	var issue1 map[string]any
	for _, raw := range items {
		item := raw.(map[string]any)
		if int(item["id"].(float64)) == 1 {
			issue1 = item
			break
		}
	}
	if issue1 == nil {
		t.Fatalf("issue 1 missing from queue: %v", items)
	}
	if issue1["state"] != "allocated" {
		t.Fatalf("expected issue 1 to stay allocated, got %v; item=%v", issue1["state"], issue1)
	}
	sandbox, _ := issue1["sandbox"].(string)
	if sandbox == "" {
		t.Fatalf("concurrent pull clobbered issue 1 sandbox: item=%v", issue1)
	}
	if _, err := os.Stat(sandbox); err != nil {
		t.Fatalf("issue 1 sandbox missing after concurrent pull: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "pull-race.txt")); err != nil {
		t.Fatalf("expected issue 2 merge result on main: %v", err)
	}
}

// TestWeaveE2E_AddPointsSerializesWithConcurrentStartAllocation pins
// a whole-queue writer recurrence: add --points must not perform an
// unlocked post-add save that can race with a wrapper allocation and
// erase its sandbox field.
func TestWeaveE2E_AddPointsSerializesWithConcurrentStartAllocation(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "allocate while pointed add runs"); envExitCode(ee) != 0 {
		t.Fatalf("seed add failed")
	}

	pause := filepath.Join(t.TempDir(), "add-inside-lock.pause")
	if err := os.WriteFile(pause, []byte("pause"), 0o644); err != nil {
		t.Fatalf("write pause file: %v", err)
	}
	addDone := runWeaveAsyncEnv(t, repo, home, []string{"YCODE_WEAVE_TEST_ADD_INSIDE_LOCK_FILE=" + pause}, "add", "pointed add", "--points", "5")
	ready := pause + ".ready"
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("add --points did not reach inside-lock pause")
		}
		time.Sleep(50 * time.Millisecond)
	}

	startDone := runWeaveAsyncEnv(t, repo, home, nil, "start", "--no-spawn", "--issue", "1")
	select {
	case res := <-startDone:
		t.Fatalf("start allocation completed while add --points held queue lock; exit=%d out=%s", envExitCode(res.ee), res.out)
	case <-time.After(300 * time.Millisecond):
	}
	if err := os.Remove(pause); err != nil && !os.IsNotExist(err) {
		t.Fatalf("release pause: %v", err)
	}
	addRes := <-addDone
	if got := envExitCode(addRes.ee); got != 0 {
		t.Fatalf("add --points exited %d; out=%s", got, addRes.out)
	}
	startRes := <-startDone
	if got := envExitCode(startRes.ee); got != 0 {
		t.Fatalf("start --no-spawn exited %d; out=%s", got, startRes.out)
	}

	out, _ := runWeave(t, repo, home, "list", "--history")
	items := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)
	var issue1, issue2 map[string]any
	for _, raw := range items {
		item := raw.(map[string]any)
		switch int(item["id"].(float64)) {
		case 1:
			issue1 = item
		case 2:
			issue2 = item
		}
	}
	if issue1 == nil || issue2 == nil {
		t.Fatalf("expected issues 1 and 2, got %v", items)
	}
	if issue1["state"] != "allocated" {
		t.Fatalf("expected issue 1 to stay allocated, got %v; item=%v", issue1["state"], issue1)
	}
	sandbox, _ := issue1["sandbox"].(string)
	if sandbox == "" {
		t.Fatalf("concurrent add --points clobbered issue 1 sandbox: item=%v", issue1)
	}
	if _, err := os.Stat(sandbox); err != nil {
		t.Fatalf("issue 1 sandbox missing after concurrent add --points: %v", err)
	}
	if p, _ := issue2["points"].(float64); int(p) != 5 {
		t.Fatalf("expected issue 2 points=5, got %v; item=%v", issue2["points"], issue2)
	}
}

// TestWeaveE2E_Prune_RemovesFailedSandbox verifies that a failed item's
// sandbox survives pull (failed items are not merged), and then prune --yes
// properly removes the lingering sandbox and branch.
func TestWeaveE2E_Prune_RemovesFailedSandbox(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "failing task"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}

	// Start a tool that creates a commit but exits non-zero
	script := `set -e; echo partial-work > partial.txt; git add partial.txt; git commit -qm "partial work"; exit 7`
	out, ee := runWeave(t, repo, home, "start", "--issue", "1", "--", "bash", "-c", script)
	if got := envExitCode(ee); got == 0 {
		t.Fatalf("expected non-zero exit for failing tool; out=%s", out)
	}

	// Verify state is failed
	out, _ = runWeave(t, repo, home, "list", "--history")
	item := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if item["state"] != "failed" {
		t.Fatalf("expected state=failed, got %v", item["state"])
	}

	sandbox := item["sandbox"].(string)
	if sandbox == "" {
		t.Fatalf("expected sandbox path on failed item; got %v", item)
	}

	// Verify sandbox still exists (failed items are not touched by pull)
	if _, err := os.Stat(sandbox); err != nil {
		t.Fatalf("sandbox should exist for failed item: %v", err)
	}

	// Pull should skip failed items
	out, ee = runWeave(t, repo, home, "pull")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("pull exited %d; out=%s", got, out)
	}

	// Sandbox should still exist after pull (failed items not merged)
	if _, err := os.Stat(sandbox); err != nil {
		t.Fatalf("sandbox should still exist after pull for failed item: %v", err)
	}

	// Now prune --yes should clean it up
	out, ee = runWeave(t, repo, home, "prune", "--yes")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("prune --yes exited %d; out=%s", got, out)
	}

	// Verify sandbox is now gone
	if _, err := os.Stat(sandbox); !os.IsNotExist(err) {
		t.Fatalf("sandbox should be removed after prune, but still exists: %s", sandbox)
	}

	// Verify prune envelope
	res := parseEnvelope(t, out)["result"].(map[string]any)
	if int(res["removed"].(float64)) < 1 {
		t.Fatalf("expected at least 1 item pruned; got %v", res["removed"])
	}
}

// TestWeaveE2E_Prune_SkipsSubmittedNotMerged verifies that prune skips
// submitted items (they're not terminal yet - they need pull first).
func TestWeaveE2E_Prune_SkipsSubmittedNotMerged(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "submitted task"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}

	// Start a tool that succeeds
	script := `set -e; echo done > done.txt; git add done.txt; git commit -qm "done work"`
	out, ee := runWeave(t, repo, home, "start", "--issue", "1", "--", "bash", "-c", script)
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("start exited %d; out=%s", got, out)
	}

	// Verify state is submitted (not terminal for prune)
	out, _ = runWeave(t, repo, home, "list", "--history")
	item := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if item["state"] != "submitted" {
		t.Fatalf("expected state=submitted, got %v", item["state"])
	}

	sandbox := item["sandbox"].(string)

	// Prune should skip submitted items
	out, ee = runWeave(t, repo, home, "prune", "--yes")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("prune exited %d; out=%s", got, out)
	}

	// Sandbox should still exist (submitted is not pruned)
	if _, err := os.Stat(sandbox); err != nil {
		t.Fatalf("sandbox should still exist for submitted item (not pruned): %v", err)
	}

	res := parseEnvelope(t, out)["result"].(map[string]any)
	if int(res["removed"].(float64)) != 0 {
		t.Fatalf("expected 0 items pruned for submitted-only queue; got %v", res["removed"])
	}
}

// TestWeaveE2E_Kill_GracefulExit: `weave kill` first asks the tool to
// leave via the control socket (/exit, then /quit). A tool that obeys
// exits 0, so the WRAPPER records submitted from a real exit code —
// the accurate, no-inference outcome. The forced path (and its
// distinct killed state) is only for tools that don't respond.
func TestWeaveE2E_Kill_GracefulExit(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "graceful exit check"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	// The tool commits its work, then waits on stdin like a TUI; it
	// honors /exit by leaving with code 0.
	script := `echo done-work > work.txt
git add work.txt
git -c user.name=g -c user.email=g@g commit -qm "work"
while read -r line; do
	[ "$line" = "/exit" ] && exit 0
done`
	bg := startWeaveAsync(t, repo, home, "start", "--issue", "1", "--", "bash", "-c", script)
	deadline := time.Now().Add(15 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		out, _ := runWeave(t, repo, home, "list", "--json")
		items := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)
		if len(items) > 0 {
			item := items[0].(map[string]any)
			sock, _ := item["ctl_sock"].(string)
			if pidF, ok := item["wrapper_pid"].(float64); ok && pidF > 0 && sock != "" {
				ready = true
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !ready {
		_ = bg.Process.Kill()
		bg.Wait()
		t.Fatalf("wrapper/ctl_sock never appeared")
	}
	// Give the script a beat to finish its commit before killing.
	time.Sleep(1 * time.Second)
	out, ee := runWeave(t, repo, home, "kill", "1", "--reason", "graceful-test")
	if got := envExitCode(ee); got != 0 {
		_ = bg.Process.Kill()
		bg.Wait()
		t.Fatalf("kill exited %d; out=%s", got, out)
	}
	res := parseEnvelope(t, out)["result"].(map[string]any)
	if res["state"] != "submitted" {
		t.Fatalf("expected graceful kill to land submitted (tool exited 0), got %v", res["state"])
	}
	if g, _ := res["graceful"].(bool); !g {
		t.Fatalf("expected graceful=true in envelope, got %v", res)
	}
	_ = bg.Wait()
}

// ─── Verify-command tests (substrate-verified outcomes) ───────────
//
// `weave add --verify "<cmd>"` stores a per-issue acceptance command
// that the WRAPPER runs in the sandbox at terminal time — claims are
// measured by weave, not asserted by agents. The verify result never
// changes the terminal state (evidence, not verdict); `weave pull`
// is the consumer that refuses verify-failed branches.

// TestWeaveE2E_Verify_PassRecordedAndPullMerges: a committing tool
// with --verify "exit 0" lands submitted with verify_exit=0, and
// pull merges the branch as usual.
func TestWeaveE2E_Verify_PassRecordedAndPullMerges(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "verified work", "--verify", "exit 0"); envExitCode(ee) != 0 {
		t.Fatalf("add --verify failed")
	}
	script := `set -e; echo ok > verified.txt; git add verified.txt; git commit -qm "feat: verified"`
	if out, ee := runWeave(t, repo, home, "start", "--issue", "1", "--", "bash", "-c", script); envExitCode(ee) != 0 {
		t.Fatalf("start exited non-zero; out=%s", out)
	}
	out, _ := runWeave(t, repo, home, "list", "--json")
	item := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if item["state"] != "submitted" {
		t.Fatalf("expected state=submitted, got %v", item["state"])
	}
	if item["verify_command"] != "exit 0" {
		t.Fatalf("expected verify_command persisted, got %v", item["verify_command"])
	}
	ve, ok := item["verify_exit"].(float64)
	if !ok || int(ve) != 0 {
		t.Fatalf("expected verify_exit=0, got %v (item=%v)", item["verify_exit"], item)
	}
	out, ee := runWeave(t, repo, home, "pull")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("pull exited %d; out=%s", got, out)
	}
	results := parseEnvelope(t, out)["result"].(map[string]any)["results"].([]any)
	if len(results) != 1 || results[0].(map[string]any)["status"] != "merged" {
		t.Fatalf("expected status=merged for verify-passing item, got %v", results)
	}
	if _, err := os.Stat(filepath.Join(repo, "verified.txt")); err != nil {
		t.Fatalf("expected verified.txt on main after pull: %v", err)
	}
}

// TestWeaveE2E_Verify_DirtyWorkingTreeBlocksPull reproduces the
// dogfood regression where verify attested uncommitted tracked
// changes, but pull merged only HEAD and then deleted the sandbox.
// The dirty attestation must be explicit, pull must refuse it, and a
// resume that commits the improvement must re-verify cleanly before
// merge.
func TestWeaveE2E_Verify_DirtyWorkingTreeBlocksPull(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	verify := `echo verified-tree=$(cat artifact.txt); test "$(cat artifact.txt)" = B`
	if _, ee := runWeave(t, repo, home, "add", "dirty verify evidence", "--verify", verify); envExitCode(ee) != 0 {
		t.Fatalf("add --verify failed")
	}
	script := `set -e
echo A > artifact.txt
git add artifact.txt
git commit -qm "feat: state a"
echo B > artifact.txt
echo litter > .aider-log
exit 0`
	if out, ee := runWeave(t, repo, home, "start", "--issue", "1", "--", "bash", "-c", script); envExitCode(ee) != 0 {
		t.Fatalf("start exited non-zero; out=%s", out)
	}

	out, _ := runWeave(t, repo, home, "list", "--json")
	item := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if item["state"] != "submitted" {
		t.Fatalf("expected state=submitted, got %v", item["state"])
	}
	if item["dirty"] != true {
		t.Fatalf("expected dirty=true after tracked uncommitted improvement, item=%v", item)
	}
	if got := int(item["dirty_files"].(float64)); got != 1 {
		t.Fatalf("expected dirty_files=1, got %v (item=%v)", item["dirty_files"], item)
	}
	if got := int(item["untracked_files"].(float64)); got != 1 {
		t.Fatalf("expected untracked_files=1, got %v (item=%v)", item["untracked_files"], item)
	}
	if item["verify_tree"] != "working-tree-dirty" {
		t.Fatalf("expected verify_tree=working-tree-dirty, got %v", item["verify_tree"])
	}
	if !strings.Contains(fmt.Sprint(item["verify_output"]), "verified-tree=B") ||
		!strings.Contains(fmt.Sprint(item["verify_output"]), "working tree had tracked uncommitted changes") {
		t.Fatalf("verify_output did not record dirty attestation: %q", item["verify_output"])
	}
	sandbox := fmt.Sprint(item["sandbox"])

	out, ee := runWeave(t, repo, home, "pull")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("pull exited %d; out=%s", got, out)
	}
	results := parseEnvelope(t, out)["result"].(map[string]any)["results"].([]any)
	if len(results) != 1 || results[0].(map[string]any)["status"] != "dirty" {
		t.Fatalf("expected status=dirty, got %v", results)
	}
	if !strings.Contains(fmt.Sprint(results[0].(map[string]any)["detail"]), "resume the agent") {
		t.Fatalf("dirty refusal did not tell operator how to recover: %v", results[0])
	}
	if _, err := os.Stat(sandbox); err != nil {
		t.Fatalf("dirty sandbox must survive refused pull: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "artifact.txt")); err == nil {
		t.Fatalf("dirty verified work was merged before the improvement was committed")
	}

	resumeScript := `set -e
test "$(cat artifact.txt)" = B
git add artifact.txt
git commit -qm "feat: state b"
exit 0`
	if out, ee := runWeave(t, repo, home, "start", "--issue", "1", "--resume", "--", "bash", "-c", resumeScript); envExitCode(ee) != 0 {
		t.Fatalf("resume exited non-zero; out=%s", out)
	}
	out, _ = runWeave(t, repo, home, "list", "--json")
	item = parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if item["dirty"] != false {
		t.Fatalf("expected dirty=false after resume commit, item=%v", item)
	}
	if item["verify_tree"] != "head" {
		t.Fatalf("expected verify_tree=head after clean reverify, got %v", item["verify_tree"])
	}

	out, ee = runWeave(t, repo, home, "pull")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("pull after clean reverify exited %d; out=%s", got, out)
	}
	results = parseEnvelope(t, out)["result"].(map[string]any)["results"].([]any)
	if len(results) != 1 || results[0].(map[string]any)["status"] != "merged" {
		t.Fatalf("expected status=merged after clean reverify, got %v", results)
	}
	got, err := os.ReadFile(filepath.Join(repo, "artifact.txt"))
	if err != nil {
		t.Fatalf("expected artifact.txt on main after pull: %v", err)
	}
	if strings.TrimSpace(string(got)) != "B" {
		t.Fatalf("merged tree diverged from verified tree: artifact.txt=%q", got)
	}
	if _, err := os.Stat(sandbox); !os.IsNotExist(err) {
		t.Fatalf("cleanly merged sandbox should be removed, stat err=%v", err)
	}
}

// TestWeaveE2E_Verify_FailBlocksPull: a committing tool with
// --verify "exit 1" still lands submitted (verify is evidence, not
// verdict) with verify_exit=1, but pull reports verify-failed and
// does NOT merge — the agent branch must be absent from the user
// repo entirely (the skip happens before the fetch).
func TestWeaveE2E_Verify_FailBlocksPull(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "fails its check", "--verify", "exit 1"); envExitCode(ee) != 0 {
		t.Fatalf("add --verify failed")
	}
	script := `set -e; echo bad > unverified.txt; git add unverified.txt; git commit -qm "feat: unverified"`
	if out, ee := runWeave(t, repo, home, "start", "--issue", "1", "--", "bash", "-c", script); envExitCode(ee) != 0 {
		t.Fatalf("start exited non-zero; out=%s", out)
	}
	out, _ := runWeave(t, repo, home, "list", "--json")
	item := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if item["state"] != "submitted" {
		t.Fatalf("verify failure must not change the terminal state; expected submitted, got %v", item["state"])
	}
	ve, ok := item["verify_exit"].(float64)
	if !ok || int(ve) != 1 {
		t.Fatalf("expected verify_exit=1, got %v (item=%v)", item["verify_exit"], item)
	}
	out, ee := runWeave(t, repo, home, "pull")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("pull exited %d; out=%s", got, out)
	}
	results := parseEnvelope(t, out)["result"].(map[string]any)["results"].([]any)
	if len(results) != 1 || results[0].(map[string]any)["status"] != "verify-failed" {
		t.Fatalf("expected status=verify-failed, got %v", results)
	}
	// Not merged: the file is absent from main, and the agent branch
	// never landed in the user repo at all.
	if _, err := os.Stat(filepath.Join(repo, "unverified.txt")); err == nil {
		t.Fatalf("verify-failed work was merged onto main")
	}
	branches, _ := exec.Command("git", "-C", repo, "branch", "--list", "agent/weave-issue-1").CombinedOutput()
	if strings.Contains(string(branches), "agent/weave-issue-1") {
		t.Fatalf("verify-failed branch leaked into user repo: %q", branches)
	}
	// The item stays submitted (not done) so a fix + re-pull or a
	// future --force can pick it up.
	out, _ = runWeave(t, repo, home, "list", "--json")
	item = parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if item["state"] != "submitted" {
		t.Fatalf("expected item to remain submitted after verify-failed pull, got %v", item["state"])
	}
}

// TestWeaveE2E_Say_Raw_SendsLiteralTab verifies that --raw sends
// C-style decoded bytes verbatim to the subagent's PTY.
func TestWeaveE2E_Say_Raw_SendsLiteralTab(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "tab check"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	// Script reads one character with IFS= read -r and writes what it received.
	script := `IFS= read -r -n 1 ch < /dev/tty
echo "$ch" > tab-received.txt
echo done`
	bg := startWeaveAsync(t, repo, home, "start", "--issue", "1", "--", "bash", "-c", script)
	defer func() { _ = bg.Process.Kill(); _ = bg.Wait() }()

	// Wait for the control socket to appear.
	deadline := time.Now().Add(15 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		out, _ := runWeave(t, repo, home, "list", "--json")
		items := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)
		if len(items) > 0 {
			if sock, _ := items[0].(map[string]any)["ctl_sock"].(string); sock != "" {
				if _, err := os.Stat(sock); err == nil {
					ready = true
					break
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("control socket never appeared in queue")
	}

	// Send a literal tab via --raw, plus an explicit Enter so the
	// terminal line discipline delivers the byte to the waiting read.
	if out, ee := runWeave(t, repo, home, "say", "1", "--raw", "\\t\\r"); envExitCode(ee) != 0 {
		t.Fatalf("say --raw exited %d; out=%s", envExitCode(ee), out)
	}

	// Wait for the subagent to finish writing the file.
	deadline = time.Now().Add(10 * time.Second)
	var got []byte
	for time.Now().Before(deadline) {
		out, _ := runWeave(t, repo, home, "list", "--json")
		items := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)
		if len(items) > 0 {
			sandbox, _ := items[0].(map[string]any)["sandbox"].(string)
			if sandbox != "" {
				if b, err := os.ReadFile(filepath.Join(sandbox, "tab-received.txt")); err == nil {
					got = b
					if len(got) > 0 {
						break
					}
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	// The received character should be a literal tab (ASCII 9).
	if len(got) == 0 || got[0] != '\t' {
		t.Fatalf("expected literal tab, got %q (bytes=%v)", string(got), got)
	}
}

// TestWeaveE2E_Say_Enter_SendsBareEnter verifies that --enter sends
// a bare Enter keystroke that unblocks a `read` waiting for input.
func TestWeaveE2E_Say_Enter_SendsBareEnter(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "enter check"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	// Script reads a line and writes "RECEIVED:$line".
	script := `IFS= read -r line < /dev/tty
echo "RECEIVED:$line" > enter-received.txt
echo done`
	bg := startWeaveAsync(t, repo, home, "start", "--issue", "1", "--", "bash", "-c", script)
	defer func() { _ = bg.Process.Kill(); _ = bg.Wait() }()

	// Wait for the control socket to appear.
	deadline := time.Now().Add(15 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		out, _ := runWeave(t, repo, home, "list", "--json")
		items := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)
		if len(items) > 0 {
			if sock, _ := items[0].(map[string]any)["ctl_sock"].(string); sock != "" {
				if _, err := os.Stat(sock); err == nil {
					ready = true
					break
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("control socket never appeared in queue")
	}

	// Send a bare Enter via --enter (no text argument).
	if out, ee := runWeave(t, repo, home, "say", "1", "--enter"); envExitCode(ee) != 0 {
		t.Fatalf("say --enter exited %d; out=%s", envExitCode(ee), out)
	}

	// Wait for the subagent to finish writing the file.
	deadline = time.Now().Add(10 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		out, _ := runWeave(t, repo, home, "list", "--json")
		items := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)
		if len(items) > 0 {
			sandbox, _ := items[0].(map[string]any)["sandbox"].(string)
			if sandbox != "" {
				if b, err := os.ReadFile(filepath.Join(sandbox, "enter-received.txt")); err == nil {
					got = strings.TrimSpace(string(b))
					if got != "" {
						break
					}
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	// The received line should be empty (just Enter was sent).
	if got != "RECEIVED:" {
		t.Fatalf("expected empty line (RECEIVED:), got %q", got)
	}
}

// TestWeaveE2E_List_All_GlobalView: --all renders every queue on the
// machine grouped by repo (JSON: queues[]), and a repo with an empty
// queue gets per-queue summaries naming other repos by basename.
func TestWeaveE2E_List_All_GlobalView(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "global view seed"); envExitCode(ee) != 0 {
		t.Fatalf("add failed")
	}
	// Second repo sharing the same HOME: its queue is empty.
	repo2 := t.TempDir()
	for _, c := range [][]string{
		{"git", "init", "-q", "-b", "main"},
		{"git", "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-qm", "seed"},
	} {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = repo2
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}
	out, ee := runWeave(t, repo2, home, "list", "--all")
	if got := envExitCode(ee); got != 0 {
		t.Fatalf("list --all exited %d; out=%s", got, out)
	}
	queues := parseEnvelope(t, out)["result"].(map[string]any)["queues"].([]any)
	if len(queues) != 1 {
		t.Fatalf("expected 1 queue in global view, got %d (%v)", len(queues), queues)
	}
	q0 := queues[0].(map[string]any)
	wantRoot, _ := filepath.EvalSymlinks(repo)
	gotRoot, _ := filepath.EvalSymlinks(q0["root"].(string))
	if gotRoot != wantRoot {
		t.Fatalf("expected root=%s, got %v", wantRoot, gotRoot)
	}
	if len(q0["items"].([]any)) != 1 {
		t.Fatalf("expected 1 item, got %v", q0["items"])
	}
}

// TestWeaveE2E_Points: story points via add --points and the point
// verb; Fibonacci-only validation; visible in list JSON.
func TestWeaveE2E_Points(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e skipped in -short")
	}
	repo, home := weaveSetupRepo(t)
	if _, ee := runWeave(t, repo, home, "add", "pointed", "--points", "5"); envExitCode(ee) != 0 {
		t.Fatalf("add --points failed")
	}
	out, _ := runWeave(t, repo, home, "list", "--json")
	item := parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if p, _ := item["points"].(float64); int(p) != 5 {
		t.Fatalf("expected points=5, got %v", item["points"])
	}
	if _, ee := runWeave(t, repo, home, "point", "1", "8"); envExitCode(ee) != 0 {
		t.Fatalf("point verb failed")
	}
	out, _ = runWeave(t, repo, home, "list", "--json")
	item = parseEnvelope(t, out)["result"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if p, _ := item["points"].(float64); int(p) != 8 {
		t.Fatalf("expected points=8 after point verb, got %v", item["points"])
	}
	// Non-Fibonacci refused.
	if out, ee := runWeave(t, repo, home, "point", "1", "4"); envExitCode(ee) != 2 {
		t.Fatalf("expected invalid_arg for points=4, got %d; out=%s", envExitCode(ee), out)
	}
}
