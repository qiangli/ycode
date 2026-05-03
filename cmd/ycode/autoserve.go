package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/cli"
	"github.com/qiangli/ycode/internal/client"
	"github.com/qiangli/ycode/internal/service"
)

const (
	defaultServerPort    = 58080
	serverHealthTimeout  = 500 * time.Millisecond
	serverStartupTimeout = 10 * time.Second
)

// detectServer checks whether a ycode server is already running.
// It uses dual verification: PID file existence + HTTP health check.
func detectServer() (baseURL string, ok bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}

	pidPath := filepath.Join(home, ".agents", "ycode", "serve.pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return "", false
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		os.Remove(pidPath)
		return "", false
	}

	// Verify the process is alive.
	proc, err := os.FindProcess(pid)
	if err != nil {
		os.Remove(pidPath)
		return "", false
	}
	if proc.Signal(syscall.Signal(0)) != nil {
		// Process is dead — stale PID file.
		os.Remove(pidPath)
		cleanAutoSentinel()
		return "", false
	}

	// Read port from discovery file, fall back to default.
	port := resolveServerPort()
	portPath := filepath.Join(home, ".agents", "ycode", "serve.port")
	if portData, err := os.ReadFile(portPath); err == nil {
		if p, err := strconv.Atoi(strings.TrimSpace(string(portData))); err == nil && p > 0 {
			port = p
		}
	}

	// Process alive — verify it's actually a ycode server via health check.
	baseURL = fmt.Sprintf("http://127.0.0.1:%d", port)

	if !healthCheck(baseURL) {
		return "", false
	}

	return baseURL, true
}

// ensureServer guarantees a ycode server is running, starting one if necessary.
func ensureServer() (string, error) {
	// Already running?
	if baseURL, ok := detectServer(); ok {
		return baseURL, nil
	}

	// Find an available port for the new server.
	port, err := findFreePort()
	if err != nil {
		return "", fmt.Errorf("find free port: %w", err)
	}

	// Start a new server in detached mode with --auto flag.
	if err := startAutoServer(port); err != nil {
		return "", fmt.Errorf("auto-start server: %w", err)
	}

	// Wait for server to become healthy.
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	deadline := time.Now().Add(serverStartupTimeout)
	delay := 50 * time.Millisecond
	for time.Now().Before(deadline) {
		time.Sleep(delay)
		if healthCheck(baseURL) {
			return baseURL, nil
		}
		// Exponential backoff: 50, 100, 200, 400, 800, 1000, 1000...
		delay = min(delay*2, time.Second)
	}

	return "", fmt.Errorf("server did not become healthy within %v", serverStartupTimeout)
}

// runThinTUI connects to a running server and starts the TUI as a thin client.
func runThinTUI(baseURL string) error {
	token := readTokenFile()

	// The API is mounted at /ycode/ on the proxy.
	apiBase := baseURL + "/ycode"

	// Get server status (active session).
	c := client.NewWSClient(apiBase, token, "")
	status, err := c.GetStatus(context.Background())
	if err != nil {
		return fmt.Errorf("connect to server: %w", err)
	}

	sessionID := status.SessionID
	if sessionID == "" {
		// Create a new session if none active.
		info, err := c.CreateSession(context.Background())
		if err != nil {
			return fmt.Errorf("create session: %w", err)
		}
		sessionID = info.ID
	}

	// Connect WebSocket for streaming events.
	wsClient := client.NewWSClient(apiBase, token, sessionID)
	if err := wsClient.Connect(context.Background()); err != nil {
		return fmt.Errorf("websocket connect: %w", err)
	}
	defer wsClient.Close()

	// Create thin app (renderer + TUI only, no heavy init).
	cwd, _ := os.Getwd()
	app, err := cli.NewThinApp(version, cwd)
	if err != nil {
		return fmt.Errorf("create thin app: %w", err)
	}

	return app.RunInteractiveWithClient(context.Background(), wsClient)
}

// startAutoServer spawns a detached ycode server with the --auto flag on the given port.
func startAutoServer(port int) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}

	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, ".agents", "ycode", "observability")
	_ = os.MkdirAll(logDir, 0o755)
	logFile, err := os.OpenFile(filepath.Join(logDir, "serve.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	args := []string{filepath.Base(exe), "serve", "--auto", "--port", strconv.Itoa(port)}

	attr := &os.ProcAttr{
		Dir:   ".",
		Files: []*os.File{os.Stdin, logFile, logFile},
		Sys:   &syscall.SysProcAttr{Setsid: true},
	}

	proc, err := os.StartProcess(exe, args, attr)
	if err != nil {
		logFile.Close()
		return err
	}
	logFile.Close()

	slog.Debug("auto-started server", "pid", proc.Pid, "port", port)
	_ = proc.Release()
	return nil
}

// healthCheck performs a quick HTTP health check against the server.
// The API is mounted at /ycode/ on the proxy.
func healthCheck(baseURL string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), serverHealthTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/ycode/api/health", nil)
	if err != nil {
		return false
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// resolveServerPort returns the configured server port or the default.
func resolveServerPort() int {
	// Check environment override.
	if p := os.Getenv("YCODE_PORT"); p != "" {
		if port, err := strconv.Atoi(p); err == nil && port > 0 {
			return port
		}
	}
	return defaultServerPort
}

// findFreePort finds an available TCP port on localhost.
func findFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port, nil
}

// runServerPrompt sends a one-shot prompt to the server and streams the response to stdout.
func runServerPrompt(baseURL, prompt string) error {
	token := readTokenFile()
	apiBase := baseURL + "/ycode"

	c := client.NewWSClient(apiBase, token, "")
	status, err := c.GetStatus(context.Background())
	if err != nil {
		return fmt.Errorf("connect to server: %w", err)
	}

	sessionID := status.SessionID
	if sessionID == "" {
		info, err := c.CreateSession(context.Background())
		if err != nil {
			return fmt.Errorf("create session: %w", err)
		}
		sessionID = info.ID
	}

	wsClient := client.NewWSClient(apiBase, token, sessionID)
	if err := wsClient.Connect(context.Background()); err != nil {
		return fmt.Errorf("websocket connect: %w", err)
	}
	defer wsClient.Close()

	// Subscribe and send.
	evCh, err := wsClient.Events(context.Background())
	if err != nil {
		return fmt.Errorf("subscribe events: %w", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- wsClient.SendMessage(context.Background(), sessionID, service.MessageInput{Text: prompt})
	}()

	// Stream text to stdout.
	for ev := range evCh {
		switch ev.Type {
		case bus.EventTextDelta:
			var data struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(ev.Data, &data) == nil {
				fmt.Print(data.Text)
			}
		case bus.EventTurnComplete:
			fmt.Println()
			return nil
		case bus.EventTurnError:
			var data struct {
				Error string `json:"error"`
			}
			json.Unmarshal(ev.Data, &data)
			return fmt.Errorf("agent error: %s", data.Error)
		}
	}

	if err := <-errCh; err != nil {
		return err
	}
	return nil
}

// cleanAutoSentinel removes the auto-start sentinel file.
func cleanAutoSentinel() {
	home, _ := os.UserHomeDir()
	os.Remove(filepath.Join(home, ".agents", "ycode", "serve.auto"))
}
