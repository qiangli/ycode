package selfinit

import "context"

// CapabilitySpec describes one MCP server ycode advertises to a foreign
// agentic tool. Built from the live manifest or the baseline fallback.
type CapabilitySpec struct {
	// Name is the foreign-tool config entry name. Always prefixed
	// "ycode-" so removal logic (future ycode init --uninstall) can
	// scope cleanly.
	Name string

	// Transport is "stdio" or "http".
	Transport string

	// Command and Args are set when Transport == "stdio".
	Command string
	Args    []string

	// URL is set when Transport == "http".
	URL string

	// Family is the short identifier from the manifest mcp.http map
	// (e.g. "loom", "pulse", "gitea") or "stdio" for the stdio entry.
	// Used to look up human descriptions for L2 instructions blocks.
	Family string
}

// Tool is a foreign agentic coding tool ycode can register itself with.
// Implementations live in per-tool files (claude.go, opencode.go, …).
type Tool interface {
	// Name returns the short identifier used in flags and logs
	// (e.g. "claude", "opencode").
	Name() string

	// Detect reports whether this tool appears installed on the host.
	// Cheap; intended to gate optional writers.
	Detect() bool

	// WriteMCP writes the L1 MCP server config for this tool (user
	// scope) given the capability list. Returns whether the file
	// changed (false when content was already up to date).
	WriteMCP(ctx context.Context, caps []CapabilitySpec) (changed bool, err error)

	// WriteInstructions writes the L2 memory-file delimited block
	// for this tool (user scope). Returns whether the file changed.
	WriteInstructions(ctx context.Context, caps []CapabilitySpec) (changed bool, err error)
}

// Result summarises one SelfInit run for callers (logs, --doctor).
type Result struct {
	RepoRoot        string              // empty if not in a git repo
	Capabilities    []string            // names of caps registered (Name field)
	ProjectFiles    []string            // files SelfInit wrote / patched
	UserFilesByTool map[string][]string // tool name -> files written
	UserGlobalFiles []string            // user-scope files (~/.config/ycode/...) ensured
	Skipped         bool                // true if marker matched and we no-op'd
	OptedOut        bool                // true if a no-init marker was found
}
