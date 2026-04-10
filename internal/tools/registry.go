package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// Registry holds all registered tools and dispatches invocations.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]*ToolSpec
}

// NewRegistry creates a new empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]*ToolSpec),
	}
}

// Register adds a tool to the registry.
func (r *Registry) Register(spec *ToolSpec) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[spec.Name]; exists {
		return fmt.Errorf("tool %q already registered", spec.Name)
	}
	r.tools[spec.Name] = spec
	return nil
}

// Get returns a tool spec by name.
func (r *Registry) Get(name string) (*ToolSpec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	spec, ok := r.tools[name]
	return spec, ok
}

// Invoke executes a tool by name with the given input.
func (r *Registry) Invoke(ctx context.Context, name string, input json.RawMessage) (string, error) {
	spec, ok := r.Get(name)
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	if spec.Handler == nil {
		return "", fmt.Errorf("tool %s has no handler", name)
	}
	return spec.Handler(ctx, input)
}

// AlwaysAvailable returns tool specs that should be sent in every API request.
func (r *Registry) AlwaysAvailable() []*ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var specs []*ToolSpec
	for _, spec := range r.tools {
		if spec.AlwaysAvailable {
			specs = append(specs, spec)
		}
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return specs
}

// Deferred returns tool specs that are loaded on demand.
func (r *Registry) Deferred() []*ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var specs []*ToolSpec
	for _, spec := range r.tools {
		if !spec.AlwaysAvailable {
			specs = append(specs, spec)
		}
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return specs
}

// All returns all registered tool specs.
func (r *Registry) All() []*ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	specs := make([]*ToolSpec, 0, len(r.tools))
	for _, spec := range r.tools {
		specs = append(specs, spec)
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return specs
}

// Names returns all registered tool names.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ApplyMiddleware wraps a tool's handler with middleware.
func (r *Registry) ApplyMiddleware(toolName string, mw Middleware) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	spec, ok := r.tools[toolName]
	if !ok {
		return fmt.Errorf("tool %q not found", toolName)
	}
	spec.Handler = mw(spec.Handler)
	return nil
}
