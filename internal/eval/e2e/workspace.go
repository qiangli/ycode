//go:build eval_e2e

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Workspace manages a temporary directory with seeded files for E2E eval tasks.
type Workspace struct {
	Dir string
}

// NewWorkspace creates a temp directory and initializes it as a Go module.
func NewWorkspace(name string) (*Workspace, error) {
	dir, err := os.MkdirTemp("", "ycode-e2e-"+name+"-*")
	if err != nil {
		return nil, fmt.Errorf("create workspace: %w", err)
	}

	ws := &Workspace{Dir: dir}

	// Initialize as a Go module.
	if err := ws.Run("go", "mod", "init", "evaltest"); err != nil {
		ws.Cleanup()
		return nil, fmt.Errorf("go mod init: %w", err)
	}

	return ws, nil
}

// WriteFile creates a file in the workspace with the given content.
func (ws *Workspace) WriteFile(relPath, content string) error {
	fullPath := filepath.Join(ws.Dir, relPath)
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(fullPath, []byte(content), 0o644)
}

// ReadFile reads a file from the workspace.
func (ws *Workspace) ReadFile(relPath string) (string, error) {
	data, err := os.ReadFile(filepath.Join(ws.Dir, relPath))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// FileExists checks if a file exists in the workspace.
func (ws *Workspace) FileExists(relPath string) bool {
	_, err := os.Stat(filepath.Join(ws.Dir, relPath))
	return err == nil
}

// Run executes a command in the workspace directory.
func (ws *Workspace) Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = ws.Dir
	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v failed: %w\n%s", name, args, err, output)
	}
	return nil
}

// GoBuild runs go build in the workspace.
func (ws *Workspace) GoBuild() error {
	return ws.Run("go", "build", "./...")
}

// GoTest runs go test in the workspace.
func (ws *Workspace) GoTest() error {
	return ws.Run("go", "test", "./...")
}

// Cleanup removes the workspace.
func (ws *Workspace) Cleanup() {
	os.RemoveAll(ws.Dir)
}
