//go:build e2e

package shell_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty/v2"
)

// E2E tests launch the real ycode binary in a PTY and drive `ycode shell`.
// Run: go test -tags e2e -timeout 60s -count=1 ./internal/shell/...
//
// Prerequisites:
//   - Binary must be pre-built at bin/ycode (run `make compile` first).
//
// These tests stay focused: prompt rendering, basic dispatch, exit. The
// PTY handoff to a full-screen app like `vi` is exercised manually for
// the skeleton — automating it cleanly is a follow-up (the test would
// have to deal with terminal control sequences).

const (
	e2eBinaryPath = "../../bin/ycode"
	e2eTimeout    = 10 * time.Second
	e2eReadSize   = 8192
)

func TestShellE2E_PromptAndExit(t *testing.T) {
	if _, err := os.Stat(e2eBinaryPath); err != nil {
		t.Skipf("binary not built at %s — run `make compile`", e2eBinaryPath)
	}

	abs, _ := filepath.Abs(e2eBinaryPath)
	cmd := exec.Command(abs, "shell", "--no-tui", "--workdir", t.TempDir())
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"YCODE_AUTO_INIT_GIT=0",
		"ANTHROPIC_API_KEY=",
		"OPENAI_API_KEY=",
	)

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatalf("pty.StartWithSize: %v", err)
	}
	t.Cleanup(func() {
		_ = ptmx.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	// Wait for the welcome banner.
	if got := readUntil(t, ptmx, "ycode shell (skeleton", e2eTimeout); !strings.Contains(got, "ycode shell") {
		t.Fatalf("did not see banner — got: %q", got)
	}

	// Drive a bash command and look for its output.
	if _, err := ptmx.Write([]byte("echo PTY_OK\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := readUntil(t, ptmx, "PTY_OK", e2eTimeout); !strings.Contains(got, "PTY_OK") {
		t.Fatalf("did not see echo output — got: %q", got)
	}

	// Exit cleanly.
	if _, err := ptmx.Write([]byte("\x04")); err != nil { // ^D
		t.Fatalf("write ^D: %v", err)
	}

	exitCh := make(chan error, 1)
	go func() { exitCh <- cmd.Wait() }()
	select {
	case err := <-exitCh:
		// Exit status 0 expected; nil error from Wait means rc=0.
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				t.Fatalf("shell exited non-zero: %v", exitErr)
			}
			t.Fatalf("Wait error: %v", err)
		}
	case <-time.After(e2eTimeout):
		t.Fatalf("shell did not exit within %v after ^D", e2eTimeout)
	}
}

// readUntil reads from ptmx until target appears, or until the timeout
// expires. Returns the accumulated output for diagnostics.
func readUntil(t *testing.T, ptmx *os.File, target string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var acc bytes.Buffer
	buf := make([]byte, e2eReadSize)

	for time.Now().Before(deadline) {
		_ = ptmx.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, err := ptmx.Read(buf)
		if n > 0 {
			acc.Write(buf[:n])
			if strings.Contains(acc.String(), target) {
				return acc.String()
			}
		}
		if err != nil && !os.IsTimeout(err) {
			break
		}
	}
	return acc.String()
}
