package selfinit

import (
	"context"
	"fmt"
	"log/slog"
	"os"
)

// Options controls SelfInit's behavior. The zero value is suitable for
// the auto-startup hook (Force=false, Logger=slog.Default).
type Options struct {
	// Cwd is the working directory to walk up from when finding the
	// git repo root. Defaults to os.Getwd() if empty.
	Cwd string

	// Home is the user home directory used to locate the manifest and
	// per-tool config files. Defaults to os.UserHomeDir() if empty.
	Home string

	// DefaultPort is the fallback proxy port used when no manifest is
	// readable. Defaults to selfinit.DefaultPort (58080).
	DefaultPort int

	// YcodeVersion is the running ycode binary's version, mixed into
	// the state hash so a binary upgrade triggers a refresh.
	YcodeVersion string

	// Force re-runs all writes even if the marker matches. Used by
	// the explicit `ycode init --refresh` and the /init skill.
	Force bool

	// RegisterForeignTools gates auto-discovery of foreign-agent
	// writers (Claude Code, OpenCode, …) from the package-level
	// registry. Off by default — touching another tool's user-scope
	// config is opt-in. When true, registered Tools whose Detect()
	// returns true get their WriteMCP / WriteInstructions called.
	//
	// Has no effect when Tools is non-nil; callers passing an
	// explicit list have already opted in by enumerating writers.
	// The env var YCODE_SELFINIT_FOREIGN=1 also flips this to true
	// (so autostart paths can be enabled without code changes).
	RegisterForeignTools bool

	// Tools, if non-nil, is the explicit list of tool writers to
	// consult. Defaults to the package-level registry. Tests override
	// this to inject stubs.
	Tools []Tool

	// Logger is required by callers but defaulted to slog.Default()
	// when nil.
	Logger *slog.Logger
}

// Run is the single source of truth for ycode's first-class-citizen
// behavior in any git repo. Idempotent — re-running with the same
// state is a no-op (modulo Force).
func Run(ctx context.Context, opts Options) (Result, error) {
	cwd := opts.Cwd
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return Result{}, fmt.Errorf("selfinit: getwd: %w", err)
		}
	}
	home := opts.Home
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return Result{}, fmt.Errorf("selfinit: home dir: %w", err)
		}
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	repoRoot := FindGitRoot(cwd)
	if repoRoot == "" {
		// Not in a git repo — user-scope writes still apply (the user
		// is on a host that has agentic tools; ycode wants to register
		// regardless of whether they're inside a project right now).
		// Project-scope writes are skipped.
		logger.Debug("selfinit: not in a git repo, skipping project-scope writes")
	} else if IsOptedOut(repoRoot) {
		logger.Debug("selfinit: per-repo opt-out marker present, skipping",
			"repo", repoRoot)
		return Result{RepoRoot: repoRoot, OptedOut: true}, nil
	}

	// User-global Foreman skill — universal protocol, same body for
	// every repo. Run unconditionally (before the marker-skip below)
	// so a deleted user-global skill is restored on next init in any
	// repo. Idempotent via writeFileIfChanged.
	var userGlobalFiles []string
	if userSkillPath, err := WriteForemanUserSkill(home); err != nil {
		logger.Warn("selfinit: write user-global foreman skill", "err", err)
	} else {
		userGlobalFiles = append(userGlobalFiles, userSkillPath)
	}

	caps := LoadCapabilities(home, opts.DefaultPort)

	// Foreign-tool writers are opt-in. An explicit Tools list means
	// the caller has already chosen to enumerate writers, so honor it.
	// Otherwise consult the registry only if RegisterForeignTools or
	// YCODE_SELFINIT_FOREIGN=1.
	tools := opts.Tools
	if tools == nil {
		if opts.RegisterForeignTools || os.Getenv("YCODE_SELFINIT_FOREIGN") == "1" {
			tools = registeredTools()
		}
	}
	var detected []Tool
	var detectedNames []string
	for _, t := range tools {
		if t.Detect() {
			detected = append(detected, t)
			detectedNames = append(detectedNames, t.Name())
		}
	}

	want := stateHash(opts.YcodeVersion, caps, detectedNames)
	if !opts.Force && repoRoot != "" && MarkerMatches(repoRoot, want) {
		logger.Debug("selfinit: marker matches, skipping", "repo", repoRoot)
		return Result{
			RepoRoot:        repoRoot,
			Capabilities:    capNames(caps),
			UserGlobalFiles: userGlobalFiles,
			Skipped:         true,
		}, nil
	}

	res := Result{
		RepoRoot:        repoRoot,
		Capabilities:    capNames(caps),
		UserFilesByTool: map[string][]string{},
		UserGlobalFiles: userGlobalFiles,
	}

	// Project-scope writes (only inside a git repo).
	if repoRoot != "" {
		written, warnings, err := WriteProjectFiles(repoRoot, caps)
		if err != nil {
			return res, err
		}
		for _, w := range warnings {
			logger.Warn("selfinit: project write warning", "msg", w)
		}
		res.ProjectFiles = written
	}

	// User-scope writes (per detected foreign tool).
	for _, t := range detected {
		var files []string
		if changed, err := t.WriteMCP(ctx, caps); err != nil {
			logger.Warn("selfinit: write MCP config",
				"tool", t.Name(), "err", err)
		} else if changed {
			files = append(files, "mcp")
		}
		if changed, err := t.WriteInstructions(ctx, caps); err != nil {
			logger.Warn("selfinit: write instructions",
				"tool", t.Name(), "err", err)
		} else if changed {
			files = append(files, "instructions")
		}
		if len(files) > 0 {
			res.UserFilesByTool[t.Name()] = files
		}
	}

	// Marker only meaningful inside a git repo.
	if repoRoot != "" {
		if err := WriteMarker(repoRoot, want); err != nil {
			logger.Warn("selfinit: write marker", "err", err)
		}
	}

	return res, nil
}

// capNames extracts the Name field from each spec for Result reporting.
func capNames(caps []CapabilitySpec) []string {
	out := make([]string, len(caps))
	for i, c := range caps {
		out[i] = c.Name
	}
	return out
}

// Per-process registry of Tool implementations. Per-tool files
// (claude.go, opencode.go, …) call RegisterTool from init().
var toolRegistry []Tool

// RegisterTool adds a Tool implementation to the package-level
// registry. Idempotent against re-registration of the same Name.
func RegisterTool(t Tool) {
	for _, existing := range toolRegistry {
		if existing.Name() == t.Name() {
			return
		}
	}
	toolRegistry = append(toolRegistry, t)
}

func registeredTools() []Tool {
	out := make([]Tool, len(toolRegistry))
	copy(out, toolRegistry)
	return out
}
