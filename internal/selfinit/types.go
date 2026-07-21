package selfinit

import "context"

// Tool is a foreign agentic coding tool ycode can register itself with.
// Implementations live in per-tool files (claude.go, opencode.go, …).
type Tool interface {
	// Name returns the short identifier used in flags and logs
	// (e.g. "claude", "opencode").
	Name() string

	// Detect reports whether this tool appears installed on the host.
	// Cheap; intended to gate optional writers.
	Detect() bool

	// WriteInstructions writes the L2 memory-file delimited block
	// for this tool (user scope). Returns whether the file changed.
	//
	// The block describes only surfaces ycode actually serves — the
	// `yc <verb>` shell built-ins. It takes no capability list: ycode
	// no longer registers MCP servers into foreign tools, because it
	// no longer runs any (docs/plan-remove-mcp.md).
	WriteInstructions(ctx context.Context) (changed bool, err error)
}

// Result summarises one SelfInit run for callers (logs, --doctor).
type Result struct {
	RepoRoot        string              // empty if not in a git repo
	ProjectFiles    []string            // files SelfInit wrote / patched
	UserFilesByTool map[string][]string // tool name -> files written
	UserGlobalFiles []string            // user-scope files (~/.config/ycode/...) ensured
	Skipped         bool                // true if marker matched and we no-op'd
	OptedOut        bool                // true if a no-init marker was found
}
