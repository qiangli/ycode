// Package browseruse provides browser automation via the browser-use Python
// library running inside a container. browser-use (MIT license) provides
// LLM-optimized DOM extraction, automatic retry/error recovery, multi-tab
// support, and structured element interaction.
//
// The browser runs as a long-lived container for the session duration.
// Each tool call uses container.Exec to dispatch actions to the running browser.
package browseruse

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/qiangli/ycode/internal/container"
	"github.com/qiangli/ycode/internal/runtime/containertool"
)

const (
	imageName = "ycode-browser:latest"

	// actionTimeout is the max time for a single browser action.
	actionTimeout = 30 * time.Second

	// buildTimeout is the max time for building the browser image.
	buildTimeout = 10 * time.Minute
)

// Action represents a browser action to execute.
type Action struct {
	Type      string `json:"action"`
	URL       string `json:"url,omitempty"`
	ElementID int    `json:"element_id,omitempty"`
	Selector  string `json:"selector,omitempty"`
	Text      string `json:"text,omitempty"`
	Direction string `json:"direction,omitempty"`
	Amount    int    `json:"amount,omitempty"`
	Goal      string `json:"goal,omitempty"`
	TabID     int    `json:"tab_id,omitempty"`
	TabAction string `json:"tab_action,omitempty"`
}

// Result represents the output from a browser action.
type Result struct {
	Success  bool   `json:"success"`
	Title    string `json:"title,omitempty"`
	URL      string `json:"url,omitempty"`
	Content  string `json:"content,omitempty"`
	Elements string `json:"elements,omitempty"`
	Data     string `json:"data,omitempty"`
	Image    string `json:"image,omitempty"` // base64 screenshot
	Error    string `json:"error,omitempty"`
}

// Service manages the browser container lifecycle.
type Service struct {
	engine    *container.Engine
	sessionID string
	network   string
	domains   []string // allowed domains (empty = all)

	mu      sync.Mutex
	ctr     *container.Container
	started bool
	builder *containertool.Tool // for image build only
}

// NewService creates a new browser-use service manager.
func NewService(engine *container.Engine, sessionID string, network string, allowedDomains []string) *Service {
	return &Service{
		engine:    engine,
		sessionID: sessionID,
		network:   network,
		domains:   allowedDomains,
		builder: &containertool.Tool{
			Name:         "browser-use",
			Image:        imageName,
			Dockerfile:   dockerfile,
			Sources:      map[string]string{"entrypoint.py": entrypointPy},
			BuildTimeout: buildTimeout,
			Engine:       engine,
		},
	}
}

// Start builds the image (if needed) and starts the browser container.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return nil
	}

	// Build image using the containertool pattern (cached, mutex-protected).
	if err := s.builder.EnsureImage(ctx); err != nil {
		return fmt.Errorf("browseruse: build image: %w", err)
	}

	name := fmt.Sprintf("ycode-browser-%s", s.sessionID)
	env := map[string]string{
		"PYTHONUNBUFFERED": "1",
	}
	if len(s.domains) > 0 {
		domainsJSON, _ := json.Marshal(s.domains)
		env["ALLOWED_DOMAINS"] = string(domainsJSON)
	}

	cfg := &container.ContainerConfig{
		Name:    name,
		Image:   imageName,
		Network: s.network,
		Labels: map[string]string{
			"ycode.session":   s.sessionID,
			"ycode.component": "browser",
		},
		Env:     env,
		Init:    true,
		Tmpfs:   []string{"/tmp"},
		Command: []string{"python", "-c", "import time; time.sleep(86400)"}, // keep alive
		Resources: container.Resources{
			CPUs:   "2.0",
			Memory: "4g",
		},
	}

	ctr, err := s.engine.CreateContainer(ctx, cfg)
	if err != nil {
		return fmt.Errorf("browseruse: create container: %w", err)
	}

	if err := ctr.Start(ctx); err != nil {
		ctr.Remove(ctx, true)
		return fmt.Errorf("browseruse: start container: %w", err)
	}

	s.ctr = ctr
	s.started = true
	slog.Info("browseruse: container started", "name", name)
	return nil
}

// Stop stops and removes the browser container.
func (s *Service) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started || s.ctr == nil {
		return nil
	}

	slog.Info("browseruse: stopping container")
	s.ctr.Stop(ctx, 10*time.Second)
	s.ctr.Remove(ctx, true)
	s.started = false
	s.ctr = nil
	return nil
}

// Available returns true if the browser container is running.
func (s *Service) Available() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started && s.ctr != nil
}

// Execute runs a browser action in the container and returns the result.
func (s *Service) Execute(ctx context.Context, action Action) (*Result, error) {
	if !s.Available() {
		return nil, fmt.Errorf("browseruse: service not running")
	}

	actionJSON, err := json.Marshal(action)
	if err != nil {
		return nil, fmt.Errorf("browseruse: marshal action: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, actionTimeout)
	defer cancel()

	// Write action JSON and execute entrypoint in the running container.
	cmd := fmt.Sprintf("echo '%s' | python /app/entrypoint.py", escapeShell(string(actionJSON)))
	execResult, err := s.ctr.Exec(ctx, cmd, "/app")
	if err != nil {
		return nil, fmt.Errorf("browseruse: exec: %w", err)
	}

	if execResult.ExitCode != 0 {
		return &Result{
			Success: false,
			Error:   fmt.Sprintf("exit code %d: %s", execResult.ExitCode, execResult.Stderr),
		}, nil
	}

	var result Result
	if err := json.Unmarshal([]byte(execResult.Stdout), &result); err != nil {
		// If output is not JSON, return it as content.
		return &Result{
			Success: true,
			Content: execResult.Stdout,
		}, nil
	}

	return &result, nil
}

// escapeShell escapes a string for safe use in a shell command.
func escapeShell(s string) string {
	// Replace single quotes with escaped version.
	return fmt.Sprintf("%s", replaceQuotes(s))
}

func replaceQuotes(s string) string {
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			result = append(result, '\'', '\\', '\'', '\'')
		} else {
			result = append(result, s[i])
		}
	}
	return string(result)
}
