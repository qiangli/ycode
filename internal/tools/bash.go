package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"syscall"

	"github.com/qiangli/ycode/internal/runtime/bash"
	"github.com/qiangli/ycode/internal/runtime/permission"
)

// bashInput extends ExecParams with job management fields.
type bashInput struct {
	bash.ExecParams
	JobID  string `json:"job_id,omitempty"`
	Signal string `json:"signal,omitempty"`
}

// RegisterBashHandler registers the bash tool handler.
// An optional executor can be provided to route commands through a container
// sandbox. When executor is nil, commands run directly on the host.
// The jobs registry enables background execution; the session tracks working
// directory across invocations. Both may be nil.
func RegisterBashHandler(r *Registry, workDir string, jobs *bash.JobRegistry, executor ...bash.Executor) {
	spec, ok := r.Get("bash")
	if !ok {
		return
	}

	var exec bash.Executor
	if len(executor) > 0 {
		exec = executor[0]
	}

	// Session tracks working directory across invocations.
	session := bash.NewShellSession(workDir)

	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params bashInput
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse bash input: %w", err)
		}

		// Job management path: retrieve output or send signal to background job.
		if params.JobID != "" {
			return handleJobRequest(jobs, params.JobID, params.Signal)
		}

		if params.Command == "" {
			return "", fmt.Errorf("either 'command' or 'job_id' is required")
		}

		if params.WorkDir == "" {
			params.WorkDir = workDir
		}
		// Inject TTY executor so interactive commands (ssh, sudo, etc.)
		// can delegate to the TUI instead of being rejected.
		params.TTYExec = r.TTYExecutor()
		// Inject job registry for background execution.
		params.Jobs = jobs

		// Safety analysis: validate the command against the current permission mode.
		// If the user explicitly approved this invocation via the permission
		// prompter, treat it as full-access (the registry already confirmed).
		mode := resolvePermissionMode(r)
		if IsPermissionApproved(ctx) {
			mode = permission.DangerFullAccess
		}
		if err := bash.ValidateForMode(params.Command, mode); err != nil {
			return "", fmt.Errorf("bash safety check: %w", err)
		}

		// Wrap command with session's working directory tracking
		// (preserves cwd across invocations). Skip for background and
		// interactive commands where output parsing would interfere.
		useSession := !params.Background && !bash.NeedsTTY(params.Command)
		if useSession {
			params.Command = session.WrapCommand(params.Command)
		}

		result, err := bash.ExecuteWith(ctx, exec, params.ExecParams)
		if err != nil {
			return "", err
		}

		output := result.Stdout
		if useSession {
			output = session.ParseOutput(output)
		}
		if result.Stderr != "" {
			if output != "" {
				output += "\n"
			}
			output += result.Stderr
		}
		if result.ExitCode != 0 {
			output += fmt.Sprintf("\n(exit code: %d)", result.ExitCode)
		}
		// When a command is not found (exit 127), suggest alternatives.
		if result.ExitCode == 127 {
			if hint := bash.SuggestAlternatives(result.Stderr); hint != "" {
				output += "\n" + hint
			}
		}
		if output == "" {
			output = "(no output)"
		}
		return output, nil
	}
}

// handleJobRequest processes job_id-based requests (output retrieval or signal).
func handleJobRequest(jobs *bash.JobRegistry, jobID string, signal string) (string, error) {
	if jobs == nil {
		return "", fmt.Errorf("background jobs not available")
	}

	job, ok := jobs.Get(jobID)
	if !ok {
		return "", fmt.Errorf("unknown job: %s", jobID)
	}

	if signal != "" {
		sig, err := parseSignal(signal)
		if err != nil {
			return "", err
		}
		if err := jobs.SignalJob(jobID, sig); err != nil {
			return "", err
		}
		return fmt.Sprintf("Signal %s sent to %s. Status: %s", signal, jobID, job.StatusSummary()), nil
	}

	// Return incremental output + status.
	output := job.Output()
	status := job.StatusSummary()
	if output == "" {
		return fmt.Sprintf("Status: %s\n(no new output)", status), nil
	}
	return fmt.Sprintf("Status: %s\n\n%s", status, output), nil
}

// parseSignal converts a signal name string to a syscall.Signal.
func parseSignal(name string) (syscall.Signal, error) {
	switch name {
	case "SIGINT":
		return syscall.SIGINT, nil
	case "SIGTERM":
		return syscall.SIGTERM, nil
	case "SIGKILL":
		return syscall.SIGKILL, nil
	default:
		return 0, fmt.Errorf("unsupported signal: %s (use SIGINT, SIGTERM, or SIGKILL)", name)
	}
}

// resolvePermissionMode returns the current permission mode from the registry,
// defaulting to WorkspaceWrite if no resolver is configured.
func resolvePermissionMode(r *Registry) permission.Mode {
	r.mu.RLock()
	resolver := r.permResolver
	r.mu.RUnlock()
	if resolver != nil {
		return resolver()
	}
	return permission.WorkspaceWrite
}
