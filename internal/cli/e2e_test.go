//go:build e2e

package cli

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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

// TestE2E_PTY_InitStreamsScaffoldOutput verifies that /init in the TUI
// produces visible progress output before any LLM call. This is the e2e
// counterpart to the unit-level repaint test — it drives the actual
// binary in a PTY and checks that the deterministic InitializeRepo phase
// streams to the screen.
//
// Pre-fix symptom: "Sending to LLM..." was the only line shown; the
// scaffold output never rendered because the bus-event handlers didn't
// repaint. Post-fix: the user sees the scaffold immediately.
//
// Runs in YCODE_NO_SERVER=1 (direct mode) inside a fresh tempdir so it
// can't accidentally scaffold the live ycode repo. No API key is set;
// the LLM-enhancement phase prints "Skipped LLM enhancement" or
// generates output via the stub-on-failure path. Either is fine — we
// only assert that *some* scaffold-shaped progress line lands.
func TestE2E_PTY_InitStreamsScaffoldOutput(t *testing.T) {
	if _, err := os.Stat(e2eBinaryPath); os.IsNotExist(err) {
		t.Skipf("binary not found at %s; run 'make compile' first", e2eBinaryPath)
	}
	if testing.Short() {
		t.Skip("e2e test skipped in -short")
	}

	tmp := t.TempDir()
	binAbs, err := filepath.Abs(e2eBinaryPath)
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	cmd := exec.Command(binAbs)
	cmd.Dir = tmp
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"YCODE_NO_SERVER=1",
		"ANTHROPIC_API_KEY=",
		"OPENAI_API_KEY=",
		"KIMI_API_KEY=",
		"MOONSHOT_API_KEY=",
	)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatalf("pty.StartWithSize: %v", err)
	}
	t.Cleanup(func() {
		ptmx.Close()
		cmd.Process.Kill()
		cmd.Wait()
	})

	// Let the TUI initialize. bubbletea renders the welcome banner and
	// alt-screen header during this window.
	time.Sleep(1500 * time.Millisecond)

	// Type /init and submit.
	if _, err := ptmx.Write([]byte("/init\r")); err != nil {
		t.Fatalf("write /init: %v", err)
	}

	// Read for up to 15 seconds, accumulating output. We expect at least one
	// of the deterministic scaffold markers to appear: a checkmark, the
	// hourglass, the "Initialized" line from InitializeRepo.Render, "AGENTS.md"
	// anywhere (the scaffold report names files it touched), or ".agents/"
	// (the directory it creates). Skipping the LLM phase entirely is fine —
	// no API key is set, the scaffold itself still emits before LLM is tried.
	deadline := time.Now().Add(15 * time.Second)
	var acc bytes.Buffer
	buf := make([]byte, e2eReadSize)
	want := []string{"⧗", "✓", "Initialized", "AGENTS.md", ".agents"}
	matched := ""
	for time.Now().Before(deadline) && matched == "" {
		ptmx.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		n, _ := ptmx.Read(buf)
		if n > 0 {
			acc.Write(buf[:n])
			for _, w := range want {
				if strings.Contains(acc.String(), w) {
					matched = w
					break
				}
			}
		}
	}

	if matched == "" {
		// Trim ANSI for a more readable failure message.
		dump := acc.String()
		if len(dump) > 4000 {
			dump = dump[:2000] + "\n...[truncated]...\n" + dump[len(dump)-2000:]
		}
		t.Errorf("no scaffold output appeared within timeout; expected one of %v\nbuffer:\n%s",
			want, dump)
	} else {
		t.Logf("found scaffold marker %q in PTY output", matched)
	}

	// Send Ctrl-D to exit. Don't fail on non-zero exit — bubbletea may
	// not unwind cleanly when killed via signal.
	_, _ = ptmx.Write([]byte{4})
	time.Sleep(500 * time.Millisecond)
	cmd.Process.Kill()
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

func TestE2E_ModelListCommand(t *testing.T) {
	if _, err := os.Stat(e2eBinaryPath); os.IsNotExist(err) {
		t.Skipf("binary not found at %s; run 'make compile' first", e2eBinaryPath)
	}

	// "model list" should run without an API key. If Ollama is not running,
	// it prints an error or empty list — either way it should exit cleanly.
	cmd := exec.Command(e2eBinaryPath, "model", "list")
	cmd.Env = append(os.Environ(),
		"ANTHROPIC_API_KEY=",
		"OPENAI_API_KEY=",
	)
	out, err := cmd.CombinedOutput()
	output := string(out)

	// The command might fail if Ollama is not reachable, which is fine.
	// We just want to verify the binary handles it gracefully (non-panic).
	t.Logf("model list output (%d bytes): %s", len(out), output)

	if err != nil {
		// Graceful error about Ollama not running is acceptable.
		if strings.Contains(output, "panic") || strings.Contains(output, "SIGSEGV") {
			t.Fatalf("model list panicked: %s", output)
		}
		t.Logf("model list exited with error (expected if no Ollama): %v", err)
	}
}

func TestE2E_ModelListCommand_OutputFormat(t *testing.T) {
	if _, err := os.Stat(e2eBinaryPath); os.IsNotExist(err) {
		t.Skipf("binary not found at %s; run 'make compile' first", e2eBinaryPath)
	}

	// If Ollama IS running, model list should produce a table with headers.
	cmd := exec.Command(e2eBinaryPath, "model", "list")
	out, err := cmd.CombinedOutput()
	output := string(out)

	if err != nil {
		t.Skipf("model list failed (Ollama likely not running): %v", err)
	}

	// When successful, output should contain table-like content.
	if len(output) == 0 {
		t.Error("expected non-empty output when model list succeeds")
	}

	t.Logf("model list output: %s", output)
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
