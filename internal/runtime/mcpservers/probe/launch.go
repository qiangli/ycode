//go:build experimental

package probe

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"time"

	telotel "github.com/qiangli/ycode/internal/telemetry/otel"
)

// LaunchChrome starts the user's installed Chrome with
// --remote-debugging-port=<port> and --user-data-dir=<dir> so probe
// mode can attach. The function:
//  1. Refuses if any process is already listening on the port —
//     per the plan, no kill-and-restart.
//  2. Auto-detects Chrome on darwin/linux/windows; the caller may
//     override via chromePath.
//  3. Creates the user-data dir if missing.
//  4. Detaches the spawned Chrome from ycode so closing ycode does
//     not close the browser.
//
// Returns the Chrome PID and the resolved chromePath.
func LaunchChrome(chromePath string, port int, userDataDir string) (pid int, resolved string, err error) {
	if port <= 0 {
		port = 9222
	}
	if portInUse(port) {
		return 0, "", fmt.Errorf("port %d is already in use — pick a different port via settings.json `browser.probeURL`", port)
	}

	resolved = chromePath
	if resolved == "" {
		resolved = DetectChrome()
	}
	if resolved == "" {
		return 0, "", fmt.Errorf("Chrome not found on PATH or in standard locations — set browser.soloChromePath or install Chrome")
	}
	if _, err := os.Stat(resolved); err != nil {
		return 0, "", fmt.Errorf("Chrome at %q: %w", resolved, err)
	}

	if err := os.MkdirAll(userDataDir, 0o755); err != nil {
		return 0, "", fmt.Errorf("user-data-dir: %w", err)
	}

	args := []string{
		fmt.Sprintf("--remote-debugging-port=%d", port),
		fmt.Sprintf("--user-data-dir=%s", userDataDir),
		"--no-first-run",
		"--no-default-browser-check",
	}
	cmd := exec.Command(resolved, args...)
	// Record only the launch event (Start). The browser keeps running
	// detached; ycode doesn't observe its eventual exit. A successful
	// Start means the spawn worked, not that the browser stays up.
	_, finish := telotel.StartExecSpan(context.Background(), telotel.ExecScopeProbeLaunch, resolved, args)
	if err := cmd.Start(); err != nil {
		finish(1, err)
		return 0, "", fmt.Errorf("launch chrome: %w", err)
	}
	finish(0, nil)

	// Give Chrome a moment to bind the debug port.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if portInUse(port) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Detach the child so ycode can exit without killing it. We
	// intentionally do NOT wait on cmd; let the OS reap it.
	go func() { _ = cmd.Wait() }()

	return cmd.Process.Pid, resolved, nil
}

func portInUse(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return true
	}
	ln.Close()
	return false
}

// DetectChrome returns the first Chrome binary found on the host.
// Order: $CHROME, then platform defaults, then PATH. Returns "" when
// nothing usable is found. Exported so solo mode can share the
// detection logic without duplicating per-OS path tables.
func DetectChrome() string {
	if p := os.Getenv("CHROME"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, p := range platformChromePaths() {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, name := range []string{"google-chrome-stable", "google-chrome", "chromium", "chromium-browser", "chrome"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

func platformChromePaths() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
	case "linux":
		return []string{
			"/usr/bin/google-chrome",
			"/usr/bin/google-chrome-stable",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
			"/snap/bin/chromium",
		}
	case "windows":
		return []string{
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		}
	}
	return nil
}
