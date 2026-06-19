package main

import (
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/qiangli/ycode/internal/selfinit"
	"github.com/qiangli/ycode/internal/service"
)

const (
	serverHealthTimeout = 500 * time.Millisecond
)

// ErrServerNotRunning is returned by ensureServer when no `ycode serve`
// process is reachable. The TUI catches this and falls back to in-process
// mode; the piped-input path catches it and runs the prompt in-process.
// The bare CLI does not start a server on its own — that is the user's
// responsibility (or a system service / launchd job in the future).
var ErrServerNotRunning = fmt.Errorf("ycode serve is not running; start it from another terminal: `ycode serve` (or install it as a system service)")

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

// ensureServer returns the base URL of a reachable ycode server, or
// ErrServerNotRunning if none is reachable. It never spawns a server —
// the bare CLI used to auto-fork `ycode serve --auto` here, but that left
// orphaned daemons accumulating across sessions. Callers must handle
// ErrServerNotRunning by either degrading to in-process mode or surfacing
// a clear message to the user.
func ensureServer() (string, error) {
	if baseURL, ok := detectServer(); ok {
		return baseURL, nil
	}
	return "", ErrServerNotRunning
}

// runThinTUIAsync shows the TUI instantly and connects to an already-running
// `ycode serve` in the background. Callers must verify the server is running
// (via detectServer) before invoking this — runThinTUIAsync no longer starts
// one itself, and a missing server means the lazy client will surface an
// ErrServerNotRunning on first use.
func runThinTUIAsync() error {
	cwd, _ := os.Getwd()
	app, err := cli.NewThinApp(version, cwd)
	if err != nil {
		return fmt.Errorf("create thin app: %w", err)
	}

	// Create a lazy client that connects on first use.
	lazyClient := client.NewLazyClient(func(ctx context.Context) (*client.WSClient, error) {
		// This runs in background — detect or start server.
		baseURL, err := ensureServer()
		if err != nil {
			return nil, err
		}

		token := readTokenFile()
		apiBase := baseURL + "/ycode"

		// Create or reuse session for this project directory.
		c := client.NewWSClient(apiBase, token, "",
			client.WithWorkDir(cwd),
		)
		info, err := c.CreateSession(ctx)
		if err != nil {
			return nil, fmt.Errorf("create session: %w", err)
		}
		sessionID := info.ID

		// Connect WebSocket with project context.
		wsClient := client.NewWSClient(apiBase, token, sessionID,
			client.WithWorkDir(cwd),
		)
		if err := wsClient.Connect(ctx); err != nil {
			return nil, fmt.Errorf("websocket connect: %w", err)
		}
		return wsClient, nil
	})

	// Start connecting in background immediately.
	lazyClient.ConnectAsync()

	return app.RunInteractiveWithClient(context.Background(), lazyClient)
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
	return selfinit.DefaultPort
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
