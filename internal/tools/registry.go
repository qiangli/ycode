package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/qiangli/ycode/internal/runtime/permission"
)

// PermissionResolver returns the current permission mode.
// This is called on every tool invocation to get the live mode.
type PermissionResolver func() permission.Mode

// PermissionPrompter asks the user whether to allow a tool invocation
// that requires elevated permissions. Returns true if the user approves.
type PermissionPrompter func(ctx context.Context, toolName string, requiredMode permission.Mode) (bool, error)

// contextKey is an unexported type for context keys in this package.
type contextKey string

// permApprovedKey is set in the context when the user has explicitly approved
// a tool invocation via the permission prompter.
const permApprovedKey contextKey = "permissionApproved"

// IsPermissionApproved returns true if the context indicates the user has
// explicitly approved this tool invocation via the permission prompter.
func IsPermissionApproved(ctx context.Context) bool {
	v, _ := ctx.Value(permApprovedKey).(bool)
	return v
}

// Registry holds all registered tools and dispatches invocations.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]*ToolSpec

	// Permission enforcement (optional — if nil, all tools are allowed).
	permResolver PermissionResolver
	permPrompter PermissionPrompter

	// Optional Bleve search index for semantic tool discovery.
	searchIndex *ToolSearchIndex
}

// NewRegistry creates a new empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]*ToolSpec),
	}
}

// SetPermissionResolver sets the function that resolves the current permission mode.
func (r *Registry) SetPermissionResolver(resolver PermissionResolver) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.permResolver = resolver
}

// SetPermissionPrompter sets the function that prompts the user for permission.
func (r *Registry) SetPermissionPrompter(prompter PermissionPrompter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.permPrompter = prompter
}

// SetSearchIndex attaches a Bleve search index for semantic tool discovery.
func (r *Registry) SetSearchIndex(idx *ToolSearchIndex) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.searchIndex = idx
}

// SearchIndex returns the attached Bleve search index, if any.
func (r *Registry) SearchIndex() *ToolSearchIndex {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.searchIndex
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
// It checks permission before execution: if the current mode doesn't allow
// the tool's RequiredMode, the invocation is denied (or the user is prompted).
func (r *Registry) Invoke(ctx context.Context, name string, input json.RawMessage) (string, error) {
	spec, ok := r.Get(name)
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	if spec.Handler == nil {
		return "", fmt.Errorf("tool %s has no handler", name)
	}

	// Check permission if a resolver is configured.
	r.mu.RLock()
	resolver := r.permResolver
	prompter := r.permPrompter
	r.mu.RUnlock()

	if resolver != nil {
		currentMode := resolver()
		if !currentMode.Allows(spec.RequiredMode) {
			// Current mode doesn't allow this tool. Try prompting the user.
			if prompter != nil {
				allowed, err := prompter(ctx, name, spec.RequiredMode)
				if err != nil {
					return "", fmt.Errorf("permission prompt for %q: %w", name, err)
				}
				if !allowed {
					return "", fmt.Errorf("permission denied: tool %q requires %s but current mode is %s",
						name, spec.RequiredMode, currentMode)
				}
				// User approved — mark context so handlers can skip redundant checks.
				ctx = context.WithValue(ctx, permApprovedKey, true)
			} else {
				return "", fmt.Errorf("permission denied: tool %q requires %s but current mode is %s (plan mode is active — exit plan mode to use write tools)",
					name, spec.RequiredMode, currentMode)
			}
		}
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

// AlwaysAvailableForMode returns always-available tools filtered by permission mode.
// In plan mode (ReadOnly), tools requiring WorkspaceWrite or higher are excluded.
func (r *Registry) AlwaysAvailableForMode(mode permission.Mode) []*ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var specs []*ToolSpec
	for _, spec := range r.tools {
		if spec.AlwaysAvailable && mode.Allows(spec.RequiredMode) {
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
