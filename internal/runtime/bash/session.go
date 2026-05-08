package bash

import (
	"sync"

	"mvdan.cc/sh/v3/interp"
)

// ShellSession tracks persistent shell state across sequential bash
// invocations. Each conversation gets one ShellSession.
//
// Two kinds of executor share a session:
//
//   - The agent-mode InterpreterExecutor builds a fresh interp.Runner per
//     call and only uses the session for cwd tracking. Validators V01–V12
//     are applied; this is the bash agent tool surface.
//
//   - The interactive `ycode shell` mode uses RunString (in persistent.go)
//     which keeps one long-lived interp.Runner so env, vars, functions,
//     `set -e`/`set -u`/`set -o pipefail` flags, and aliases survive
//     across submissions. Stdio is re-installed per call via the
//     supported `interp.StdIO(...)(r)` pattern. Validators are NOT
//     applied — the user is the operator.
type ShellSession struct {
	mu      sync.Mutex
	workDir string

	// Persistent runner for shell mode. Lazily created on first
	// RunString call; reused for all subsequent calls. nil while in
	// agent mode (the InterpreterExecutor never touches this field).
	runnerMu     sync.Mutex
	runner       *interp.Runner
	tty          TTYRunner
	extraExecMWs []func(interp.ExecHandlerFunc) interp.ExecHandlerFunc
}

// AddExecMiddleware appends an interp.ExecHandler middleware to the
// persistent runner's chain. The middleware runs BEFORE the standard
// shell-mode exec handler, so it can intercept commands like `yc <verb>`
// before they hit PATH lookup. Must be called before the first
// RunString; if the runner is already created, Reset() it first.
func (s *ShellSession) AddExecMiddleware(mw func(interp.ExecHandlerFunc) interp.ExecHandlerFunc) {
	s.runnerMu.Lock()
	defer s.runnerMu.Unlock()
	s.extraExecMWs = append(s.extraExecMWs, mw)
}

// NewShellSession creates a session starting at the given directory.
func NewShellSession(initialDir string) *ShellSession {
	return &ShellSession{workDir: initialDir}
}

// WorkDir returns the current working directory.
func (s *ShellSession) WorkDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.workDir
}

// SetWorkDir updates the current working directory.
func (s *ShellSession) SetWorkDir(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workDir = dir
}

// WrapCommand returns the command as-is. Directory tracking is handled
// by the in-process interpreter which updates the session directly.
func (s *ShellSession) WrapCommand(command string) string {
	return command
}

// ParseOutput returns stdout as-is. Directory tracking is handled
// by the in-process interpreter which updates the session directly.
func (s *ShellSession) ParseOutput(stdout string) string {
	return stdout
}
