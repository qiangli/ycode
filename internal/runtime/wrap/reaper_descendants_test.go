//go:build !windows

package wrap

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestReapLeakedDescendants_KillsSetsidChild(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess test skipped under -short")
	}

	// Spawn a long-sleep child in its own session. A Setsid child is
	// not a member of the parent's process group, so it would survive
	// a pgroup tree-kill - exactly the leak scenario the reaper is
	// designed to catch.
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}

	childPid := cmd.Process.Pid
	t.Logf("child PID: %d", childPid)

	// Guard: only Wait once - the reaper SIGKILLs the child, then
	// we Wait to reap the zombie. If the test fails before then, the
	// defer does the cleanup.
	reaped := false
	defer func() {
		if !reaped {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()

	// Allow the child to start and settle into its new session.
	time.Sleep(100 * time.Millisecond)

	if !isProcessAlive(childPid) {
		t.Fatalf("child %d not alive before reaping", childPid)
	}
	descendants, err := enumerateDescendants(os.Getpid())
	if err != nil {
		t.Skipf("descendant enumeration unavailable: %v", err)
	}
	if !containsPID(descendants, childPid) {
		t.Fatalf("child %d missing from descendants before reaping: %v", childPid, descendants)
	}

	// Reap descendants of this process (analogous to the wrapper PID
	// in Run).
	reapLeakedDescendants(os.Getpid())

	// Wait for the child to exit. SIGKILL is immediate; keep a short
	// timeout so a failed reaper does not wait for the full sleep.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Logf("cmd.Wait error (expected for signal-killed child): %v", err)
		}
	case <-time.After(2 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		reaped = true
		t.Fatalf("child %d still alive after reaping", childPid)
	}
	reaped = true

	if cmd.ProcessState == nil {
		t.Fatal("ProcessState is nil after Wait")
	}

	// ExitCode returns -1 when the process was terminated by a signal.
	if cmd.ProcessState.ExitCode() != -1 {
		t.Errorf("child was not killed by reaper (exit code %d, state: %s)",
			cmd.ProcessState.ExitCode(), cmd.ProcessState.String())
	}
}

func containsPID(pids []int, want int) bool {
	for _, pid := range pids {
		if pid == want {
			return true
		}
	}
	return false
}
