package tools

import "sort"

// Toolset defines a named group of tools, optionally composing other toolsets.
type Toolset struct {
	Name        string // e.g., "research"
	Description string
	Tools       []string // direct tool names
	Includes    []string // names of other toolsets to include
}

// ToolsetRegistry manages named toolsets and resolves composition.
type ToolsetRegistry struct {
	toolsets map[string]*Toolset
}

// NewToolsetRegistry creates a registry with built-in toolsets.
func NewToolsetRegistry() *ToolsetRegistry {
	r := &ToolsetRegistry{toolsets: make(map[string]*Toolset)}
	r.registerBuiltins()
	return r
}

func (r *ToolsetRegistry) registerBuiltins() {
	// Core file operations — always-available tools.
	r.Register(&Toolset{
		Name:        "core",
		Description: "Core file operations",
		Tools:       []string{"bash", "read_file", "write_file", "edit_file", "glob_search", "grep_search"},
	})
	r.Register(&Toolset{
		Name:        "web",
		Description: "Web access tools",
		Tools:       []string{"WebFetch", "WebSearch"},
	})
	r.Register(&Toolset{
		Name:        "git",
		Description: "Git operations",
		Tools:       []string{"git_diff", "git_log", "git_status", "git_blame"},
	})
	r.Register(&Toolset{
		Name:        "memory",
		Description: "Memory operations",
		Tools:       []string{"MemorySave", "MemoryRecall", "MemoryForget"},
	})
	r.Register(&Toolset{
		Name:        "file_extended",
		Description: "Extended file operations",
		Tools:       []string{"list_directory", "tree", "get_file_info", "read_multiple_files", "copy_file", "move_file", "delete_file", "create_directory"},
	})
	r.Register(&Toolset{
		Name:        "search",
		Description: "Search tools",
		Tools:       []string{"grep_search", "glob_search", "ToolSearch"},
	})
	// Composite toolsets
	r.Register(&Toolset{
		Name:        "research",
		Description: "Research toolset (web + search + read)",
		Tools:       []string{"read_file"},
		Includes:    []string{"web", "search"},
	})
	r.Register(&Toolset{
		Name:        "full_stack",
		Description: "Full development toolset",
		Includes:    []string{"core", "web", "git", "file_extended", "memory"},
	})
	r.Register(&Toolset{
		Name:        "read_only",
		Description: "Read-only tools for exploration",
		Tools:       []string{"bash", "read_file", "glob_search", "grep_search", "list_directory", "tree", "get_file_info", "read_multiple_files", "WebFetch", "WebSearch", "ToolSearch"},
	})
}

// Register adds a toolset.
func (r *ToolsetRegistry) Register(ts *Toolset) {
	r.toolsets[ts.Name] = ts
}

// Resolve returns all tool names for a toolset, recursively resolving includes.
// Uses a visited set to prevent infinite loops.
func (r *ToolsetRegistry) Resolve(name string) []string {
	seen := make(map[string]bool)
	visited := make(map[string]bool)
	r.resolveInto(name, seen, visited)

	result := make([]string, 0, len(seen))
	for tool := range seen {
		result = append(result, tool)
	}
	sort.Strings(result)
	return result
}

func (r *ToolsetRegistry) resolveInto(name string, tools map[string]bool, visited map[string]bool) {
	if visited[name] {
		return // cycle prevention
	}
	visited[name] = true

	ts, ok := r.toolsets[name]
	if !ok {
		return
	}
	for _, t := range ts.Tools {
		tools[t] = true
	}
	for _, inc := range ts.Includes {
		r.resolveInto(inc, tools, visited)
	}
}

// Get returns a toolset by name.
func (r *ToolsetRegistry) Get(name string) (*Toolset, bool) {
	ts, ok := r.toolsets[name]
	return ts, ok
}

// List returns all registered toolset names.
func (r *ToolsetRegistry) List() []string {
	var names []string
	for name := range r.toolsets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ResolveMultiple resolves multiple toolset names and merges results.
func (r *ToolsetRegistry) ResolveMultiple(names []string) []string {
	merged := make(map[string]bool)
	visited := make(map[string]bool)
	for _, name := range names {
		r.resolveInto(name, merged, visited)
	}
	result := make([]string, 0, len(merged))
	for tool := range merged {
		result = append(result, tool)
	}
	sort.Strings(result)
	return result
}
