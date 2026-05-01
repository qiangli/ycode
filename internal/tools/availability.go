package tools

import (
	"log/slog"

	"github.com/qiangli/ycode/internal/runtime/sysinfo"
)

// FilterBySystemContext disables tools that cannot work in the current
// system environment. This is called once at startup after tool registration.
//
// Disabled tools are excluded from AlwaysAvailable(), Deferred(), and
// ToolSearch results, preventing the LLM from seeing or calling tools
// that would fail due to missing system capabilities.
func FilterBySystemContext(r *Registry, sys *sysinfo.SystemContext) {
	if sys == nil {
		return
	}

	disabled := 0

	// No internet → disable web tools.
	if !sys.HasInternet {
		disabled += disableTools(r, "air-gapped",
			"WebFetch", "WebSearch")
	}

	// Cannot run containers → disable browser automation.
	// Note: ast_search has an in-process tree-sitter fallback, so it stays enabled.
	// Only browser_* tools require a podman container unconditionally.
	if !sys.CanRunContainers {
		disabled += disableTools(r, "no container engine",
			"browser_navigate", "browser_click", "browser_type",
			"browser_scroll", "browser_screenshot", "browser_extract",
			"browser_back", "browser_tabs")
	}

	// No git → disable git tools.
	if !sys.HasGit {
		disabled += disableTools(r, "git not in PATH",
			"git_status", "git_log", "git_commit", "git_branch",
			"git_stash", "git_add", "git_reset", "git_show", "git_grep",
			"view_diff",
			"GitServerRepoList", "GitServerRepoCreate",
			"GitServerWorktreeCreate", "GitServerWorktreeMerge",
			"GitServerWorktreeCleanup")
	}

	// CI environment → disable interactive tools (no human to respond).
	if sys.IsCI {
		disabled += disableTools(r, "CI environment",
			"AskUserQuestion", "SendUserMessage")
	}

	if disabled > 0 {
		slog.Info("tools disabled by system context",
			"count", disabled,
			"summary", sys.Summary())
	}
}

// disableTools marks the named tools as disabled in the registry.
// Returns the count of tools actually disabled.
func disableTools(r *Registry, reason string, names ...string) int {
	count := 0
	for _, name := range names {
		spec, ok := r.Get(name)
		if !ok {
			continue
		}
		if spec.Disabled {
			continue // already disabled
		}
		spec.Disabled = true
		count++
		slog.Debug("tool disabled", "tool", name, "reason", reason)
	}
	return count
}
