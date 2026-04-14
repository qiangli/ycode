package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/qiangli/ycode/internal/runtime/permission"
)

// FilteredRegistry wraps a Registry and restricts tool access to a subset.
// Tools not in the allowlist are invisible (Get returns false, Invoke returns error).
type FilteredRegistry struct {
	inner   *Registry
	allowed map[string]bool
	mu      sync.RWMutex
}

// NewFilteredRegistry creates a registry view restricted to the named tools.
// If allowedTools is nil or empty, all tools in the inner registry are visible.
func NewFilteredRegistry(inner *Registry, allowedTools []string) *FilteredRegistry {
	fr := &FilteredRegistry{inner: inner}
	if len(allowedTools) > 0 {
		fr.allowed = make(map[string]bool, len(allowedTools))
		for _, name := range allowedTools {
			fr.allowed[name] = true
		}
	}
	return fr
}

// isAllowed returns true if the tool passes the allowlist filter.
func (fr *FilteredRegistry) isAllowed(name string) bool {
	fr.mu.RLock()
	defer fr.mu.RUnlock()
	if fr.allowed == nil {
		return true // no filter — all tools allowed
	}
	return fr.allowed[name]
}

// Get returns a tool spec if it exists and is allowed.
func (fr *FilteredRegistry) Get(name string) (*ToolSpec, bool) {
	if !fr.isAllowed(name) {
		return nil, false
	}
	return fr.inner.Get(name)
}

// Invoke executes a tool if it is allowed.
func (fr *FilteredRegistry) Invoke(ctx context.Context, name string, input json.RawMessage) (string, error) {
	if !fr.isAllowed(name) {
		return "", fmt.Errorf("tool %q not available in this agent context", name)
	}
	return fr.inner.Invoke(ctx, name, input)
}

// AlwaysAvailable returns always-available tools filtered by the allowlist.
func (fr *FilteredRegistry) AlwaysAvailable() []*ToolSpec {
	all := fr.inner.AlwaysAvailable()
	return fr.filterSpecs(all)
}

// AlwaysAvailableForMode returns tools filtered by both mode and allowlist.
func (fr *FilteredRegistry) AlwaysAvailableForMode(mode permission.Mode) []*ToolSpec {
	all := fr.inner.AlwaysAvailableForMode(mode)
	return fr.filterSpecs(all)
}

// Deferred returns deferred tools filtered by the allowlist.
func (fr *FilteredRegistry) Deferred() []*ToolSpec {
	all := fr.inner.Deferred()
	return fr.filterSpecs(all)
}

// All returns all tools filtered by the allowlist.
func (fr *FilteredRegistry) All() []*ToolSpec {
	all := fr.inner.All()
	return fr.filterSpecs(all)
}

// Names returns allowed tool names.
func (fr *FilteredRegistry) Names() []string {
	all := fr.inner.Names()
	if fr.allowed == nil {
		return all
	}
	var filtered []string
	for _, name := range all {
		if fr.isAllowed(name) {
			filtered = append(filtered, name)
		}
	}
	sort.Strings(filtered)
	return filtered
}

func (fr *FilteredRegistry) filterSpecs(specs []*ToolSpec) []*ToolSpec {
	if fr.allowed == nil {
		return specs
	}
	var filtered []*ToolSpec
	for _, s := range specs {
		if fr.allowed[s.Name] {
			filtered = append(filtered, s)
		}
	}
	return filtered
}
