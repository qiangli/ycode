package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/qiangli/ycode/internal/cli"
	"github.com/qiangli/ycode/internal/client"
)

// runRemoteTUI connects to a remote ycode server and runs the TUI.
func runRemoteTUI(url string) error {
	token := readTokenFile()

	switch {
	case strings.HasPrefix(url, "ws://") || strings.HasPrefix(url, "wss://"):
		return runWSRemoteTUI(url, token)
	case strings.HasPrefix(url, "nats://"):
		return runNATSRemoteTUI(url)
	case strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://"):
		return runWSRemoteTUI(url, token)
	default:
		return fmt.Errorf("unsupported URL scheme: %s (use ws://, nats://, or http://)", url)
	}
}

func runWSRemoteTUI(url string, token string) error {
	// Normalize http:// to base URL for REST.
	baseURL := url
	if strings.HasPrefix(baseURL, "ws://") {
		baseURL = "http://" + baseURL[5:]
	} else if strings.HasPrefix(baseURL, "wss://") {
		baseURL = "https://" + baseURL[6:]
	}
	baseURL = strings.TrimRight(baseURL, "/")

	// Get session ID from the server.
	c := client.NewWSClient(baseURL, token, "")
	status, err := c.GetStatus(context.Background())
	if err != nil {
		return fmt.Errorf("connect to server: %w", err)
	}

	sessionID := status.SessionID
	if sessionID == "" {
		// Create a new session.
		info, err := c.CreateSession(context.Background())
		if err != nil {
			return fmt.Errorf("create session: %w", err)
		}
		sessionID = info.ID
	}

	// Connect WebSocket for streaming events.
	wsClient := client.NewWSClient(baseURL, token, sessionID)
	if err := wsClient.Connect(context.Background()); err != nil {
		return fmt.Errorf("websocket connect: %w", err)
	}
	defer wsClient.Close()

	// Create thin app and run TUI in client mode.
	cwd, _ := os.Getwd()
	app, err := cli.NewThinApp(version, cwd)
	if err != nil {
		return fmt.Errorf("create app: %w", err)
	}

	return app.RunInteractiveWithClient(context.Background(), wsClient)
}

func runNATSRemoteTUI(url string) error {
	// TODO: Wire NATSClient into TUI when TUI refactor is complete.
	fmt.Printf("NATS remote mode not yet implemented (url: %s)\n", url)
	return nil
}

// readTokenFile reads the auth token from ~/.agents/ycode/server.token.
func readTokenFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".agents", "ycode", "server.token"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
