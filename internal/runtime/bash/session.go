package bash

import (
	"sync"
)

// ShellSession tracks persistent shell state (working directory) across
// sequential bash invocations. Each conversation gets one ShellSession.
type ShellSession struct {
	mu      sync.Mutex
	workDir string
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
