//go:build !windows

package wrap

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestForwardSignalsToChild_SIGTERMReachesChild(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess test skipped under -short")
	}
	// Launch a long-sleep child in its own process group, send the
	// parent a synthetic SIGTERM via the registered handler, and
	// assert the child exits within signalGraceWindow + slack.
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = newProcessGroupAttr()
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	stop := forwardSignalsToChild(cmd)
	defer stop()

	// Deliver SIGTERM to *this* process; the handler should forward
	// it to the child PG. Goroutine so the Wait below isn't blocked.
	go func() {
		// Tiny delay so the handler goroutine is parked on its
		// channel before we send.
		time.Sleep(50 * time.Millisecond)
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
	}()

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-done:
		// success — child exited from the forwarded signal
	case <-time.After(signalGraceWindow + 2*time.Second):
		// Force kill so the test doesn't leak the sleeper.
		_ = cmd.Process.Kill()
		t.Fatalf("child did not exit after SIGTERM forward")
	}
}

func TestForwardSignalsToChild_StopReleases(t *testing.T) {
	// Issuing stop() twice must be safe and must not panic. Also
	// verifies that the handler goroutine exits when stop is called
	// (otherwise this test would leak a goroutine per run).
	cmd := exec.Command("sleep", "0.1")
	cmd.SysProcAttr = newProcessGroupAttr()
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	stop := forwardSignalsToChild(cmd)
	_ = cmd.Wait()
	stop()
	stop() // second call must be idempotent
}
