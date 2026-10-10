//go:build unix

package bashy

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	harnessrunner "github.com/qiangli/bashy/pkg/harnessrunner"

	"github.com/qiangli/ycode/internal/harness/stages/hitl"
)

// Sprint 412 Story #1172 (6fbc6cce): genie's tool call runs through
// Executor.Execute, whose wall time is the minimum of the compiled
// bashy.execution.timeoutMs and the model's per-call timeout_ms. A call whose
// command leaves a grandchild behind (a reproduction script that starts a
// real server and then waits) must still come back at that deadline: the whole
// process tree is stopped, the result is marked timed out, and the model can
// read what happened. The bashy-level proofs live in bashy's
// pkg/harnessrunner/timeout_test.go; this one guards the same contract at the
// ycode layer genie actually uses, so a regression in the adapter (dropping
// the per-call timeout, mishandling the timed-out result as an error, losing
// the partial output) fails here instead of hanging the agent for hours.
func TestExecuteHonorsCallTimeoutWhenAGrandchildKeepsThePipeOpen(t *testing.T) {
	// Keep the session preamble from re-entering a host bashy: this process is
	// the ycode test binary, not bashy.
	t.Setenv("BASHY_SELF", filepath.Join(t.TempDir(), "no-bashy-self"))
	t.Setenv("BASHY_HOME", filepath.Join(t.TempDir(), "bashy-home"))
	t.Setenv("BASHY_KB_DIR", filepath.Join(t.TempDir(), "host-kb"))
	t.Setenv("BASHY_SKILLS_DIR", filepath.Join(t.TempDir(), "skills"))
	t.Setenv("YCODE_DATA_DIR", filepath.Join(t.TempDir(), "ycode-data"))
	executor, cwd := testExecutor(t, nil, 0)
	pidFile := filepath.Join(cwd, "grandchild.pid")
	// 'sh -c 'sleep 300 & wait'': the external sh exits only after its child,
	// and the grandchild inherits the job's output pipe, so a timeout that
	// only signalled the direct child would block on that pipe forever.
	script := "@effects(\"read,write,exec\")\nfunction serve() {\n  echo started\n  /bin/sh -c 'sleep 300 & echo $! > \"$1\"; wait' serve " + strconv.Quote(pidFile) + "\n}\nserve"
	call := hitl.Call{ID: "call-1", Name: "bashy", Script: script, TimeoutMS: 1000}
	// The model-visible timeout notice is what the observation stage renders
	// from these fields, so assert them directly (exit status + outcome +
	// partial stdout) rather than re-testing that package's formatting.
	done := make(chan harnessrunner.Result, 1)
	begin := time.Now()
	go func() {
		value, err := executor.Execute(context.Background(), testMeta(), call, digest("allow"))
		if err != nil {
			t.Errorf("execute: %v", err)
			close(done)
			return
		}
		result, ok := value.(harnessrunner.Result)
		if !ok {
			t.Errorf("execute returned %T, want harnessrunner.Result", value)
			close(done)
			return
		}
		done <- result
	}()
	var result harnessrunner.Result
	select {
	case result = <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("execute did not return after its wall time: the timeout was not enforced")
	}
	if elapsed := time.Since(begin); elapsed > 8*time.Second {
		t.Fatalf("execute returned after %v, want wall time + a short grace", elapsed)
	}
	if result.Protocol != "ok" || result.Outcome != harnessrunner.OutcomeTimedOut {
		t.Fatalf("outcome = %q/%q, want ok/timedOut (%#v)", result.Protocol, result.Outcome, result)
	}
	if result.Process == nil || result.Process.ExitCode == nil {
		t.Fatalf("process result missing: %#v", result.Process)
	}
	// The per-call timeout_ms (1000) is the one that must bind, not the
	// compiled bashy.execution.timeoutMs (testConfig's 2000): that min is
	// computed in this package, so it is part of what this test protects.
	if result.Intent == nil || result.Intent.Limits.WallTimeMs != 1000 {
		t.Fatalf("bound wall time = %v, want the call's 1000ms", result.Intent.Limits.WallTimeMs)
	}
	if got := string(outputBytes(t, result)); got != "started\n" {
		t.Fatalf("partial stdout = %q, want the output produced before the deadline", got)
	}
	// The grandchild must not outlive the call it was started by.
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("grandchild pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		t.Fatalf("grandchild pid = %q", raw)
	}
	deadline := time.Now().Add(3 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("grandchild %d survived the timeout (leaked process)", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
