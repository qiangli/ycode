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
