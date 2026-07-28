package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	coreacp "github.com/qiangli/coreutils/pkg/acp"

	"github.com/qiangli/ycode/internal/runtime/origin"
)

// promptApp is the existing ycode turn surface used by the ACP adapter.
// Keeping the interface this narrow makes it explicit that ACP does not own a
// second agent loop: every prompt goes through cli.App.RunPrompt.
type promptApp interface {
	SetPrintMode(bool)
	SetOutput(io.Writer, io.Writer)
	RunPrompt(context.Context, string) error
	Close() error
}

type acpAppFactory func(cwd string) (promptApp, error)

type acpRunner struct {
	newApp acpAppFactory
	stderr io.Writer

	mu       sync.Mutex
	sessions map[string]*acpSession
}

type acpSession struct {
	mu     sync.Mutex
	cwd    string
	app    promptApp
	output bytes.Buffer
}

func newACPRunner(factory acpAppFactory, stderr io.Writer) *acpRunner {
	return &acpRunner{
		newApp:   factory,
		stderr:   stderr,
		sessions: make(map[string]*acpSession),
	}
}

func (r *acpRunner) Run(ctx context.Context, req coreacp.TurnRequest) (coreacp.TurnResponse, error) {
	state, err := r.session(req.SessionID, req.Cwd)
	if err != nil {
		return coreacp.TurnResponse{}, err
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	if state.cwd != req.Cwd {
		return coreacp.TurnResponse{}, fmt.Errorf(
			"ACP session %q changed working directory from %q to %q",
			req.SessionID, state.cwd, req.Cwd,
		)
	}
	if state.app == nil {
		state.app, err = r.newApp(req.Cwd)
		if err != nil {
			return coreacp.TurnResponse{}, fmt.Errorf("create ycode app: %w", err)
		}
		state.app.SetPrintMode(true)
		state.app.SetOutput(&state.output, r.stderr)
	}

	state.output.Reset()
	prompt := joinACPPrompt(req.Prompt)
	if strings.TrimSpace(prompt) == "" {
		return coreacp.TurnResponse{}, fmt.Errorf("ACP prompt contains no text")
	}
	if err := state.app.RunPrompt(ctx, prompt); err != nil {
		return coreacp.TurnResponse{}, err
	}
	return coreacp.TurnResponse{
		Text:       state.output.String(),
		StopReason: coreacp.StopReasonEndTurn,
	}, nil
}

func (r *acpRunner) session(id, cwd string) (*acpSession, error) {
	if id == "" {
		return nil, fmt.Errorf("ACP session ID is empty")
	}
	if cwd == "" {
		return nil, fmt.Errorf("ACP session %q has an empty working directory", id)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.sessions[id]
	if state == nil {
		state = &acpSession{cwd: cwd}
		r.sessions[id] = state
	}
	return state, nil
}

func (r *acpRunner) Close() error {
	r.mu.Lock()
	sessions := make([]*acpSession, 0, len(r.sessions))
	for _, state := range r.sessions {
		sessions = append(sessions, state)
	}
	r.sessions = make(map[string]*acpSession)
	r.mu.Unlock()

	var firstErr error
	for _, state := range sessions {
		state.mu.Lock()
		if state.app != nil {
			if err := state.app.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		state.mu.Unlock()
	}
	return firstErr
}

func joinACPPrompt(blocks []coreacp.ContentBlock) string {
	var prompt strings.Builder
	for _, block := range blocks {
		prompt.WriteString(block.Text)
	}
	return prompt.String()
}

func serveACP(input io.Reader, output, stderr io.Writer, factory acpAppFactory) error {
	runner := newACPRunner(factory, stderr)
	defer runner.Close()

	// NewAgent owns JSON-RPC framing and strict ACP v1 negotiation. Its
	// Initialize implementation rejects any protocol version other than
	// coreacp.ProtocolVersionNumber before a session can be created.
	agent := coreacp.NewAgent(runner, coreacp.AgentOptions{}, input, output)
	<-agent.Done()
	return nil
}

func newACPCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "acp",
		Short:        "Serve ycode as an ACP agent over stdio",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			origin.SetAgentTool("acp")
			return serveACP(os.Stdin, os.Stdout, os.Stderr, func(cwd string) (promptApp, error) {
				return newApp(cwd)
			})
		},
	}
}
