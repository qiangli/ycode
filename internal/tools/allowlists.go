package tools

// DefaultAgentAllowlists defines the default tool allowlists per agent type.
// An empty/nil slice means all tools are available.
var DefaultAgentAllowlists = map[string][]string{
	"Explore": {
		"bash", "read_file", "glob_search", "grep_search",
		"list_directory", "tree", "get_file_info", "read_multiple_files",
		"WebFetch", "WebSearch", "ToolSearch",
	},
	"Plan": {
		"bash", "read_file", "glob_search", "grep_search",
		"list_directory", "tree", "get_file_info", "read_multiple_files",
		"WebFetch", "WebSearch", "ToolSearch",
	},
	"Verification": {
		"bash", "read_file", "glob_search", "grep_search",
		"list_directory", "tree", "get_file_info",
	},
	// "general-purpose" — no restriction (nil), gets all tools.
}

// DefaultSubagentBlocklist defines tools that subagents should never have access to.
// These prevent recursive delegation, unauthorized user interaction, memory corruption,
// and cross-platform side effects.
var DefaultSubagentBlocklist = []string{
	"Agent",           // no recursive delegation
	"Handoff",         // no handoff from subagents
	"AskUserQuestion", // no direct user interaction
	"MemorySave",      // no writes to shared memory
	"MemoryForget",    // no memory deletion
	"CronCreate",      // no scheduling from subagents
	"CronDelete",      // no schedule deletion
	"RemoteTrigger",   // no remote triggers
}

// ApplyBlocklist removes blocked tools from an allowlist.
// If allowlist is nil (unrestricted), returns nil — callers should use
// FilteredRegistry.Hide() instead.
func ApplyBlocklist(allowlist []string, blocklist []string) []string {
	if len(blocklist) == 0 {
		return allowlist
	}
	blocked := make(map[string]bool, len(blocklist))
	for _, name := range blocklist {
		blocked[name] = true
	}
	if allowlist == nil {
		// Can't filter nil — callers should use FilteredRegistry.Hide() instead.
		return nil
	}
	var filtered []string
	for _, name := range allowlist {
		if !blocked[name] {
			filtered = append(filtered, name)
		}
	}
	return filtered
}
