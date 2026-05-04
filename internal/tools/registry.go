package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/qiangli/ycode/internal/runtime/bash"
	"github.com/qiangli/ycode/internal/runtime/permission"
	"github.com/qiangli/ycode/internal/runtime/policy"
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

// FileAccessHook is called when a tool accesses a file path, enabling
// JIT instruction discovery from subdirectories.
type FileAccessHook func(path string)

// Registry holds all registered tools and dispatches invocations.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]*ToolSpec

	// Global middleware chain — applied to all tool invocations in order.
	// Inspired by LangGraph's wrap_tool_call pattern for composable
	// interception (retry, caching, logging, metrics).
	globalMiddleware []Middleware

	// Permission enforcement (optional — if nil, all tools are allowed).
	permResolver PermissionResolver
	permPrompter PermissionPrompter

	// Optional Bleve search index for semantic tool discovery.
	searchIndex *ToolSearchIndex

	// Optional hook called when tools access file paths.
	fileAccessHook FileAccessHook

	// Optional hook called when tools modify a file (write/edit).
	fileWriteHook FileAccessHook

	// Optional quality monitor for tracking tool call success/failure rates.
	qualityMonitor *QualityMonitor

	// Optional policy engine for rule-based permission decisions.
	policyEngine *policy.Engine

	// Optional TTY executor for running interactive commands (ssh, sudo, etc.)
	// with full terminal access. Only available in TUI mode.
	ttyExecutor bash.TTYExecutor
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

// SetPolicyEngine attaches a policy engine for rule-based permission decisions.
// When set, policy rules are checked before the standard permission mode.
func (r *Registry) SetPolicyEngine(e *policy.Engine) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.policyEngine = e
}

// SetTTYExecutor sets the executor for interactive commands that need terminal access.
func (r *Registry) SetTTYExecutor(exec bash.TTYExecutor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ttyExecutor = exec
}

// TTYExecutor returns the configured TTY executor, or nil.
func (r *Registry) TTYExecutor() bash.TTYExecutor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.ttyExecutor
}

// SetFileAccessHook sets a callback invoked when tools access file paths.
func (r *Registry) SetFileAccessHook(hook FileAccessHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fileAccessHook = hook
}

// SetFileWriteHook sets a callback invoked when tools modify a file.
// Used to trigger incremental re-indexing of changed files.
func (r *Registry) SetFileWriteHook(hook FileAccessHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fileWriteHook = hook
}

// AddFileWriteHook chains an additional callback onto the file write hook.
// Unlike SetFileWriteHook which replaces, this preserves existing hooks.
func (r *Registry) AddFileWriteHook(hook FileAccessHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	prev := r.fileWriteHook
	if prev == nil {
		r.fileWriteHook = hook
	} else {
		r.fileWriteHook = func(path string) {
			prev(path)
			hook(path)
		}
	}
}

// NotifyFileWrite calls the file write hook if set.
// Tools should call this after successfully writing or editing a file.
func (r *Registry) NotifyFileWrite(path string) {
	r.mu.RLock()
	hook := r.fileWriteHook
	r.mu.RUnlock()
	if hook != nil {
		go hook(path) // async — don't block the tool response
	}
}

// NotifyFileAccess calls the file access hook if set.
// Tools should call this when they access a file path.
func (r *Registry) NotifyFileAccess(path string) {
	r.mu.RLock()
	hook := r.fileAccessHook
	r.mu.RUnlock()
	if hook != nil {
		hook(path)
	}
}

// SetQualityMonitor attaches a quality monitor for tracking tool call metrics.
func (r *Registry) SetQualityMonitor(qm *QualityMonitor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.qualityMonitor = qm
}

// QualityMonitor returns the attached quality monitor, if any.
func (r *Registry) QualityMonitor() *QualityMonitor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.qualityMonitor
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

	// Check policy rules first (highest priority).
	r.mu.RLock()
	pe := r.policyEngine
	r.mu.RUnlock()
	if pe != nil {
		decision, reason := pe.Evaluate(name, "")
		switch decision {
		case policy.DecisionDeny:
			return "", fmt.Errorf("policy denied tool %q: %s", name, reason)
		case policy.DecisionAllow:
			// Skip normal permission checks — policy explicitly allows.
			ctx = context.WithValue(ctx, permApprovedKey, true)
		case policy.DecisionAsk:
			// Fall through to normal permission flow.
		}
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

	// Build the handler chain: global middleware wraps the tool's handler.
	handler := spec.Handler
	r.mu.RLock()
	middleware := r.globalMiddleware
	r.mu.RUnlock()
	// Apply in reverse so the first registered middleware is outermost.
	for i := len(middleware) - 1; i >= 0; i-- {
		handler = middleware[i](handler)
	}

	start := time.Now()
	result, err := handler(ctx, input)

	// Record call metrics in quality monitor.
	if r.qualityMonitor != nil {
		success := err == nil
		duration := time.Since(start).Milliseconds()
		r.qualityMonitor.RecordCall(name, success, float64(duration))
	}

	return result, err
}

// AlwaysAvailable returns tool specs that should be sent in every API request.
func (r *Registry) AlwaysAvailable() []*ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var specs []*ToolSpec
	for _, spec := range r.tools {
		if spec.AlwaysAvailable && !spec.Disabled {
			specs = append(specs, spec)
		}
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return specs
}

// AlwaysAvailableForMode returns always-available tools filtered by permission mode.
// In plan mode (ReadOnly), tools requiring WorkspaceWrite or higher are excluded.
// Disabled tools are excluded.
func (r *Registry) AlwaysAvailableForMode(mode permission.Mode) []*ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var specs []*ToolSpec
	for _, spec := range r.tools {
		if spec.AlwaysAvailable && !spec.Disabled && mode.Allows(spec.RequiredMode) {
			specs = append(specs, spec)
		}
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return specs
}

// Deferred returns tool specs that are loaded on demand.
// Disabled tools are excluded.
func (r *Registry) Deferred() []*ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var specs []*ToolSpec
	for _, spec := range r.tools {
		if !spec.AlwaysAvailable && !spec.Disabled {
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

// UseMiddleware appends a global middleware that wraps every tool invocation.
// Global middleware is applied in the order registered, outermost first.
// This is the composable interception pattern inspired by LangGraph's
// wrap_tool_call — useful for retry, caching, logging, and metrics.
func (r *Registry) UseMiddleware(mw Middleware) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.globalMiddleware = append(r.globalMiddleware, mw)
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
