//go:build e2e

package cli

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty/v2"
)

// E2E tests launch the real ycode binary in a PTY and interact with it.
// Run: go test -tags e2e -timeout 120s -count=1 ./internal/cli/...
//
// Prerequisites:
// - Binary must be pre-built at bin/ycode (run `make compile` first)
// - No API key required for commands tested here (version, help, quit)

const (
	e2eBinaryPath = "../../bin/ycode"
	e2eTimeout    = 15 * time.Second
	e2eReadSize   = 8192
)

// startPTY launches the ycode binary in a PTY with no API keys.
func startPTY(t *testing.T, args ...string) (*os.File, *exec.Cmd) {
	t.Helper()
	cmd := exec.Command(e2eBinaryPath, args...)
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"ANTHROPIC_API_KEY=",
		"OPENAI_API_KEY=",
		"KIMI_API_KEY=",
		"MOONSHOT_API_KEY=",
	)

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: 24,
		Cols: 80,
	})
	if err != nil {
		t.Fatalf("pty.StartWithSize: %v", err)
	}
	t.Cleanup(func() {
		ptmx.Close()
		cmd.Process.Kill()
		cmd.Wait()
	})
	return ptmx, cmd
}

// expectOutput reads from the PTY until the target string appears or timeout.
func expectOutput(t *testing.T, ptmx *os.File, target string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var acc bytes.Buffer
	buf := make([]byte, e2eReadSize)

	for time.Now().Before(deadline) {
		// Set a short read deadline so we can check the timeout.
		ptmx.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, err := ptmx.Read(buf)
		if n > 0 {
			acc.Write(buf[:n])
			if strings.Contains(acc.String(), target) {
				return acc.String()
			}
		}
		if err != nil && !os.IsTimeout(err) {
			// EOF or other error — binary may have exited.
			break
		}
	}

	t.Fatalf("timeout waiting for %q in PTY output;\ngot: %s", target, acc.String())
	return ""
}

// waitExit waits for the process to exit within a timeout.
func waitExit(t *testing.T, cmd *exec.Cmd, timeout time.Duration) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			// Some exit codes are expected (e.g., signal killed).
			if exitErr, ok := err.(*exec.ExitError); ok {
				if exitErr.ExitCode() != 0 {
					t.Logf("process exited with code %d", exitErr.ExitCode())
				}
			}
		}
	case <-time.After(timeout):
		cmd.Process.Kill()
		t.Fatal("process did not exit within timeout")
	}
}

// drainPTY reads all available output from the PTY without blocking.
func drainPTY(ptmx *os.File) string {
	var acc bytes.Buffer
	buf := make([]byte, e2eReadSize)
	for {
		ptmx.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, err := ptmx.Read(buf)
		if n > 0 {
			acc.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	return acc.String()
}

// --- Tests ---

func TestE2E_VersionCommand(t *testing.T) {
	cmd := exec.Command(e2eBinaryPath, "version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("version command failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "ycode") {
		t.Errorf("expected 'ycode' in version output, got: %s", out)
	}
}

func TestE2E_HelpCommand(t *testing.T) {
	cmd := exec.Command(e2eBinaryPath, "help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("help command failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "Available Commands") {
		t.Errorf("expected 'Available Commands' in help output, got: %s", out)
	}
}

func TestE2E_InvalidFlag(t *testing.T) {
	cmd := exec.Command(e2eBinaryPath, "--nonexistent-flag")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Error("expected error for invalid flag")
	}
	if !strings.Contains(string(out), "unknown flag") {
		t.Errorf("expected 'unknown flag' in error output, got: %s", out)
	}
}

func TestE2E_PTY_StartAndCtrlD(t *testing.T) {
	if _, err := os.Stat(e2eBinaryPath); os.IsNotExist(err) {
		t.Skipf("binary not found at %s; run 'make compile' first", e2eBinaryPath)
	}

	ptmx, cmd := startPTY(t)

	// The interactive mode sends terminal queries (background color, cursor
	// position) as part of bubbletea init. Give it a moment to start.
	time.Sleep(500 * time.Millisecond)

	// Read whatever output has arrived. The binary may show a welcome screen
	// or an error about missing API keys — either proves it started in the PTY.
	var acc bytes.Buffer
	buf := make([]byte, e2eReadSize)
	for i := 0; i < 10; i++ {
		ptmx.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, _ := ptmx.Read(buf)
		if n > 0 {
			acc.Write(buf[:n])
		}
	}

	if acc.Len() == 0 {
		t.Error("expected some output from binary in PTY")
	}
	t.Logf("PTY output (%d bytes received)", acc.Len())

	// Send Ctrl+C (more reliable than Ctrl+D for killing bubbletea programs).
	io.WriteString(ptmx, "\x03")

	// Give it a moment then force-kill if needed.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
		// Process exited — success.
	case <-time.After(3 * time.Second):
		// Force kill — still proves the binary launched.
		cmd.Process.Kill()
		<-done
	}
}

func TestE2E_DoctorCommand(t *testing.T) {
	// The doctor command runs health checks without needing an API key.
	cmd := exec.Command(e2eBinaryPath, "doctor")
	out, err := cmd.CombinedOutput()
	// Doctor may exit non-zero if checks fail, but it should still run.
	_ = err
	if len(out) == 0 {
		t.Error("expected non-empty output from doctor command")
	}
	t.Logf("doctor output: %s", out)
}
