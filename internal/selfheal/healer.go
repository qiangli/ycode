// Package selfheal provides self-healing capabilities for ycode.
//
// When ycode encounters errors, instead of bailing out immediately,
// the self-heal system can attempt to diagnose and fix the problem,
// then rebuild and restart the application if needed.
//
// Inspired by aider's retry logic and claw-code's recovery recipes.
package selfheal

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// execCommand is a variable so tests can override it.
var execCommand = exec.CommandContext

// FailureType categorizes the type of failure encountered.
type FailureType string

const (
	// FailureTypeBuild indicates a compilation/build error.
	FailureTypeBuild FailureType = "build"
	// FailureTypeRuntime indicates a runtime panic or error.
	FailureTypeRuntime FailureType = "runtime"
	// FailureTypeConfig indicates a configuration error.
	FailureTypeConfig FailureType = "config"
	// FailureTypeAPI indicates an API communication error.
	FailureTypeAPI FailureType = "api"
	// FailureTypeTool indicates a tool execution error.
	FailureTypeTool FailureType = "tool"
	// FailureTypeUnknown indicates an uncategorized error.
	FailureTypeUnknown FailureType = "unknown"
)

// HealerState represents the current state of the healing process.
type HealerState string

const (
	// HealerStateIdle means no healing is in progress.
	HealerStateIdle HealerState = "idle"
	// HealerStateDiagnosing means analyzing the error.
	HealerStateDiagnosing HealerState = "diagnosing"
	// HealerStateFixing means attempting to fix the error.
	HealerStateFixing HealerState = "fixing"
	// HealerStateRebuilding means rebuilding the application.
	HealerStateRebuilding HealerState = "rebuilding"
	// HealerStateRestarting means restarting the application.
	HealerStateRestarting HealerState = "restarting"
	// HealerStateFailed means healing failed after max attempts.
	HealerStateFailed HealerState = "failed"
	// HealerStateSucceeded means healing succeeded.
	HealerStateSucceeded HealerState = "succeeded"
)

// Config controls self-healing behavior.
type Config struct {
	// Enabled enables or disables self-healing.
	Enabled bool `json:"enabled"`
	// MaxAttempts is the maximum number of healing attempts per error type.
	MaxAttempts int `json:"max_attempts"`
	// AutoRebuild enables automatic rebuild after fixing code errors.
	AutoRebuild bool `json:"auto_rebuild"`
	// AutoRestart enables automatic restart after successful rebuild.
	AutoRestart bool `json:"auto_restart"`
	// EscalationPolicy determines what happens when healing fails.
	EscalationPolicy EscalationPolicy `json:"escalation_policy"`
	// BuildCommand is the command to rebuild (defaults to "go build").
	BuildCommand string `json:"build_command"`
	// BuildTimeout is the maximum time allowed for rebuild.
	BuildTimeout time.Duration `json:"build_timeout"`
	// HealablePaths are path patterns that can be modified (e.g., ["*.go", "go.mod"]).
	HealablePaths []string `json:"healable_paths"`
	// ProtectedPaths are paths that should never be modified.
	ProtectedPaths []string `json:"protected_paths"`
}

// DefaultConfig returns the default self-heal configuration.
func DefaultConfig() *Config {
	return &Config{
		Enabled:          true,
		MaxAttempts:      3,
		AutoRebuild:      true,
		AutoRestart:      true,
		EscalationPolicy: EscalationPolicyAsk,
		BuildCommand:     "go build -o bin/ycode ./cmd/ycode/",
		BuildTimeout:     5 * time.Minute,
		HealablePaths:    []string{"*.go", "go.mod", "go.sum", "internal/**/*.go", "cmd/**/*.go", "pkg/**/*.go"},
		ProtectedPaths:   []string{".git/", "vendor/", "node_modules/", ".ycode/"},
	}
}

// EscalationPolicy determines behavior when healing fails.
type EscalationPolicy string

const (
	// EscalationPolicyAsk prompts the user for direction.
	EscalationPolicyAsk EscalationPolicy = "ask"
	// EscalationPolicyLog logs the error and continues.
	EscalationPolicyLog EscalationPolicy = "log"
	// EscalationPolicyAbort exits the application.
	EscalationPolicyAbort EscalationPolicy = "abort"
)

// ErrorInfo contains details about an error that can be healed.
type ErrorInfo struct {
	// Type is the category of error.
	Type FailureType
	// Error is the actual error that occurred.
	Error error
	// Message is a human-readable description.
	Message string
	// StackTrace contains the stack trace for runtime errors.
	StackTrace string
	// Context contains additional context (file paths, line numbers, etc.).
	Context map[string]string
	// Timestamp when the error occurred.
	Timestamp time.Time
}

// HealingAttempt tracks a single healing attempt.
type HealingAttempt struct {
	// AttemptNumber is the 1-indexed attempt number.
	AttemptNumber int
	// StartedAt is when the attempt started.
	StartedAt time.Time
	// CompletedAt is when the attempt finished.
	CompletedAt time.Time
	// Actions taken during this attempt.
	Actions []HealingAction
	// Success indicates if the attempt succeeded.
	Success bool
	// Error from this attempt, if any.
	Error error
}

// HealingAction represents a single healing action.
type HealingAction struct {
	// Type is the kind of action (edit, command, etc.).
	Type string
	// Description of what was done.
	Description string
	// Path affected (if any).
	Path string
	// Success indicates if the action succeeded.
	Success bool
}

// Healer handles self-healing of ycode errors.
type Healer struct {
	config   *Config
	state    HealerState
	attempts map[FailureType][]HealingAttempt
	history  []ErrorInfo
	aiHealer *AIHealer
}

// NewHealer creates a new healer with the given configuration.
func NewHealer(cfg *Config) *Healer {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &Healer{
		config:   cfg,
		state:    HealerStateIdle,
		attempts: make(map[FailureType][]HealingAttempt),
		history:  make([]ErrorInfo, 0),
	}
}

// SetAIHealer attaches an AI healer for AI-driven error fixing.
func (h *Healer) SetAIHealer(ah *AIHealer) {
	h.aiHealer = ah
}

// State returns the current healer state.
func (h *Healer) State() HealerState {
	return h.state
}

// Config returns the healer configuration.
func (h *Healer) Config() *Config {
	return h.config
}

// CanHeal determines if an error can be healed.
func (h *Healer) CanHeal(err error) bool {
	if !h.config.Enabled {
		return false
	}
	if err == nil {
		return false
	}

	// Check if we've exceeded max attempts for this error type
	errType := ClassifyError(err)
	attempts := h.attempts[errType]
	if len(attempts) >= h.config.MaxAttempts {
		return false
	}

	// Check if error type is healable
	switch errType {
	case FailureTypeBuild, FailureTypeRuntime, FailureTypeConfig:
		return true
	case FailureTypeAPI, FailureTypeTool:
		// These might be transient, healing might help
		return true
	default:
		return false
	}
}

// ClassifyError categorizes an error into a failure type.
func ClassifyError(err error) FailureType {
	if err == nil {
		return FailureTypeUnknown
	}

	errStr := strings.ToLower(err.Error())

	// Build errors
	if strings.Contains(errStr, "build") ||
		strings.Contains(errStr, "compile") ||
		strings.Contains(errStr, "syntax error") ||
		strings.Contains(errStr, "undefined:") ||
		strings.Contains(errStr, "cannot find package") ||
		strings.Contains(errStr, "import") && strings.Contains(errStr, "not found") {
		return FailureTypeBuild
	}

	// Config errors
	if strings.Contains(errStr, "config") ||
		strings.Contains(errStr, "configuration") ||
		strings.Contains(errStr, "settings") ||
		strings.Contains(errStr, "load") && strings.Contains(errStr, "config") {
		return FailureTypeConfig
	}

	// API errors
	if strings.Contains(errStr, "api") ||
		strings.Contains(errStr, "connection") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "rate limit") ||
		strings.Contains(errStr, "unauthorized") ||
		strings.Contains(errStr, "authentication") {
		return FailureTypeAPI
	}

	// Tool errors
	if strings.Contains(errStr, "tool") ||
		strings.Contains(errStr, "command") && strings.Contains(errStr, "failed") ||
		strings.Contains(errStr, "execution") && strings.Contains(errStr, "failed") {
		return FailureTypeTool
	}

	// Runtime errors (panics, etc.)
	if strings.Contains(errStr, "panic") ||
		strings.Contains(errStr, "runtime error") ||
		strings.Contains(errStr, "nil pointer") ||
		strings.Contains(errStr, "index out of range") ||
		strings.Contains(errStr, "divide by zero") {
		return FailureTypeRuntime
	}

	return FailureTypeUnknown
}

// AttemptHealing tries to heal an error.
// Returns true if healing succeeded, false otherwise.
func (h *Healer) AttemptHealing(ctx context.Context, errInfo ErrorInfo) (bool, error) {
	if !h.config.Enabled {
		return false, fmt.Errorf("self-healing is disabled")
	}

	h.state = HealerStateDiagnosing
	// Don't reset state to idle for terminal states
	defer func() {
		if h.state != HealerStateSucceeded && h.state != HealerStateFailed {
			h.state = HealerStateIdle
		}
	}()

	// Record the error
	h.history = append(h.history, errInfo)

	// Check max attempts
	attempts := h.attempts[errInfo.Type]
	if len(attempts) >= h.config.MaxAttempts {
		h.state = HealerStateFailed
		return false, fmt.Errorf("max healing attempts (%d) exceeded for %s errors", h.config.MaxAttempts, errInfo.Type)
	}

	// Create new attempt record
	attempt := HealingAttempt{
		AttemptNumber: len(attempts) + 1,
		StartedAt:     time.Now(),
		Actions:       make([]HealingAction, 0),
	}

	h.state = HealerStateFixing

	// Attempt to fix based on error type
	var fixErr error
	switch errInfo.Type {
	case FailureTypeBuild:
		fixErr = h.fixBuildError(ctx, &attempt, errInfo)
	case FailureTypeRuntime:
		fixErr = h.fixRuntimeError(ctx, &attempt, errInfo)
	case FailureTypeConfig:
		fixErr = h.fixConfigError(ctx, &attempt, errInfo)
	case FailureTypeAPI:
		fixErr = h.fixAPIError(ctx, &attempt, errInfo)
	case FailureTypeTool:
		fixErr = h.fixToolError(ctx, &attempt, errInfo)
	default:
		fixErr = h.fixGenericError(ctx, &attempt, errInfo)
	}

	attempt.CompletedAt = time.Now()

	if fixErr != nil {
		attempt.Error = fixErr
		attempt.Success = false
		h.attempts[errInfo.Type] = append(attempts, attempt)
		return false, fmt.Errorf("healing attempt failed: %w", fixErr)
	}

	// Rebuild if needed
	if h.config.AutoRebuild && errInfo.Type == FailureTypeBuild {
		h.state = HealerStateRebuilding
		if err := h.rebuild(ctx, &attempt); err != nil {
			attempt.Error = err
			attempt.Success = false
			h.attempts[errInfo.Type] = append(attempts, attempt)
			return false, fmt.Errorf("rebuild failed: %w", err)
		}
	}

	attempt.Success = true
	h.attempts[errInfo.Type] = append(attempts, attempt)

	// Set state based on whether auto-restart is enabled
	// Note: actual restart is handled by the caller (WrapMain)
	if h.config.AutoRestart {
		h.state = HealerStateRestarting
	} else {
		h.state = HealerStateSucceeded
	}

	return true, nil
}

// fixWithAI delegates to the AIHealer if available. Returns (delegated, error).
func (h *Healer) fixWithAI(ctx context.Context, attempt *HealingAttempt, errInfo ErrorInfo) (bool, error) {
	if h.aiHealer == nil {
		return false, nil
	}

	fixAttempt, err := h.aiHealer.AttemptAIFixing(ctx, errInfo)
	if fixAttempt != nil {
		attempt.Actions = append(attempt.Actions, HealingAction{
			Type:        "ai_fix",
			Description: fmt.Sprintf("AI fix (iteration %d): %s", fixAttempt.Iteration, fixAttempt.Analysis),
			Success:     fixAttempt.Success,
		})
	}
	return true, err
}

// fixBuildError attempts to fix a build error.
func (h *Healer) fixBuildError(ctx context.Context, attempt *HealingAttempt, errInfo ErrorInfo) error {
	if delegated, err := h.fixWithAI(ctx, attempt, errInfo); delegated {
		return err
	}
	attempt.Actions = append(attempt.Actions, HealingAction{
		Type:        "analyze",
		Description: fmt.Sprintf("Analyzed build error: %s", errInfo.Message),
		Success:     false,
	})
	return fmt.Errorf("build error healing requires AI integration (no AI provider configured)")
}

// fixRuntimeError attempts to fix a runtime error.
func (h *Healer) fixRuntimeError(ctx context.Context, attempt *HealingAttempt, errInfo ErrorInfo) error {
	if delegated, err := h.fixWithAI(ctx, attempt, errInfo); delegated {
		return err
	}
	attempt.Actions = append(attempt.Actions, HealingAction{
		Type:        "analyze",
		Description: fmt.Sprintf("Analyzed runtime error: %s", errInfo.Message),
		Success:     false,
	})
	return fmt.Errorf("runtime error healing requires AI integration (no AI provider configured)")
}

// fixConfigError attempts to fix a configuration error.
func (h *Healer) fixConfigError(ctx context.Context, attempt *HealingAttempt, errInfo ErrorInfo) error {
	if delegated, err := h.fixWithAI(ctx, attempt, errInfo); delegated {
		return err
	}
	attempt.Actions = append(attempt.Actions, HealingAction{
		Type:        "analyze",
		Description: fmt.Sprintf("Analyzed config error: %s", errInfo.Message),
		Success:     false,
	})
	return fmt.Errorf("config error healing requires AI integration (no AI provider configured)")
}

// fixAPIError handles transient API errors by marking them for retry.
// API errors are often transient (timeouts, rate limits) and benefit from retry.
func (h *Healer) fixAPIError(ctx context.Context, attempt *HealingAttempt, errInfo ErrorInfo) error {
	attempt.Actions = append(attempt.Actions, HealingAction{
		Type:        "retry",
		Description: fmt.Sprintf("Marked for retry after API error: %s", errInfo.Message),
		Success:     true,
	})
	return nil
}

// fixToolError attempts to fix a tool execution error.
func (h *Healer) fixToolError(ctx context.Context, attempt *HealingAttempt, errInfo ErrorInfo) error {
	if delegated, err := h.fixWithAI(ctx, attempt, errInfo); delegated {
		return err
	}
	attempt.Actions = append(attempt.Actions, HealingAction{
		Type:        "analyze",
		Description: fmt.Sprintf("Analyzed tool error: %s", errInfo.Message),
		Success:     false,
	})
	return fmt.Errorf("tool error healing requires AI integration (no AI provider configured)")
}

// fixGenericError attempts to fix an unknown error.
func (h *Healer) fixGenericError(ctx context.Context, attempt *HealingAttempt, errInfo ErrorInfo) error {
	attempt.Actions = append(attempt.Actions, HealingAction{
		Type:        "analyze",
		Description: fmt.Sprintf("Analyzed error (unknown type): %s", errInfo.Message),
		Success:     false,
	})
	return fmt.Errorf("generic error healing not supported")
}

// rebuild attempts to rebuild the application.
func (h *Healer) rebuild(ctx context.Context, attempt *HealingAttempt) error {
	if h.config.BuildCommand == "" {
		return fmt.Errorf("no build command configured")
	}

	parts := strings.Fields(h.config.BuildCommand)
	if len(parts) == 0 {
		return fmt.Errorf("empty build command")
	}

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Dir = h.findProjectRoot()

	output, err := cmd.CombinedOutput()

	action := HealingAction{
		Type:        "rebuild",
		Description: fmt.Sprintf("Executed: %s", h.config.BuildCommand),
		Success:     err == nil,
	}
	attempt.Actions = append(attempt.Actions, action)

	if err != nil {
		return fmt.Errorf("build failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// Restart restarts the application.
// This should be called by the main function after successful healing.
func (h *Healer) Restart() error {
	h.state = HealerStateRestarting

	// Get the path to the current executable
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Ensure we have an absolute path
	exe, err = filepath.Abs(exe)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// On Unix systems, we can use execve to replace the current process
	// This preserves the PID and process group
	if runtime.GOOS != "windows" {
		return h.restartUnix(exe)
	}

	// On Windows, we spawn a new process and exit
	return h.restartWindows(exe)
}

func (h *Healer) restartUnix(exe string) error {
	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	// Get current environment
	env := os.Environ()

	// Add a marker to indicate this is a restart
	env = append(env, "YCODE_SELF_HEAL_RESTART=1")

	// Get current args
	args := os.Args

	// Use syscall.Exec on Unix to replace current process
	// This doesn't return on success
	return syscallExec(exe, args, env, cwd)
}

func (h *Healer) restartWindows(exe string) error {
	// On Windows, spawn a new process and exit
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = func() string {
		cwd, _ := os.Getwd()
		return cwd
	}()
	cmd.Env = append(os.Environ(), "YCODE_SELF_HEAL_RESTART=1")

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start new process: %w", err)
	}

	// Exit current process
	os.Exit(0)
	return nil // unreachable
}

// findProjectRoot attempts to find the project root directory.
func (h *Healer) findProjectRoot() string {
	// Try to find go.mod
	dir, _ := os.Getwd()
	for dir != "/" && dir != "." {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// Fall back to current directory
	cwd, _ := os.Getwd()
	return cwd
}

// GetAttempts returns the healing attempts for a failure type.
func (h *Healer) GetAttempts(failureType FailureType) []HealingAttempt {
	return h.attempts[failureType]
}

// GetHistory returns the error history.
func (h *Healer) GetHistory() []ErrorInfo {
	return h.history
}

// Reset clears all healing state.
func (h *Healer) Reset() {
	h.attempts = make(map[FailureType][]HealingAttempt)
	h.history = make([]ErrorInfo, 0)
	h.state = HealerStateIdle
}
