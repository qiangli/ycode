package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"time"
)

// newQuickHTTPClient returns a tight-timeout client for the doctor
// command's localhost probes.
func newQuickHTTPClient() *http.Client {
	return &http.Client{Timeout: 500 * time.Millisecond}
}

// liveExtensionConnected pokes the running hub's /dispatch with a
// no-op method (_ping). The hub returns a 503 when no extension is
// attached, or a 200 with an "extension disconnected"-style error.
// Either is a reachable signal. We deem the extension "connected"
// when the dispatch round-trips a non-503 status.
func liveExtensionConnected(port int) bool {
	url := fmt.Sprintf("http://127.0.0.1:%d/dispatch", port)
	body, _ := json.Marshal(map[string]any{"method": "_ping"})
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return false
	}
	req.Body = http.NoBody
	req.Body = http.MaxBytesReader(nil, nil, 0)
	req, _ = http.NewRequest(http.MethodPost, url, bytesReader(body))
	req.Header.Set("Content-Type", "application/json")
	c := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := c.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode != http.StatusServiceUnavailable
}

func bytesReader(b []byte) *bytesReadCloser { return &bytesReadCloser{b: b} }

type bytesReadCloser struct {
	b   []byte
	pos int
}

func (r *bytesReadCloser) Read(p []byte) (int, error) {
	if r.pos >= len(r.b) {
		return 0, http.ErrBodyReadAfterClose
	}
	n := copy(p, r.b[r.pos:])
	r.pos += n
	return n, nil
}

func (r *bytesReadCloser) Close() error { return nil }

// runDetached starts a command without waiting for it. Used to open
// URLs in the user's default browser via OS-specific tools.
func runDetached(name string, args []string) error {
	c := exec.Command(name, args...)
	if err := c.Start(); err != nil {
		return err
	}
	go func() { _ = c.Wait() }()
	return nil
}

// openInFileManager pops a Finder / file-manager window at the given
// path. Works for hidden directories on macOS (`open` accepts paths
// regardless of whether they would be visible in Finder browsing).
// Returns an error when no platform-appropriate command is found or
// fails to launch.
func openInFileManager(path string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{path}
	case "linux":
		name, args = "xdg-open", []string{path}
	case "windows":
		name, args = "explorer", []string{path}
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	return runDetached(name, args)
}
