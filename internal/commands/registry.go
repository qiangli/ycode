package commands

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// HandlerFunc is the function signature for a slash command handler.
type HandlerFunc func(ctx context.Context, args string) (string, error)

// Spec describes a slash command.
type Spec struct {
	Name        string
	Description string
	Usage       string
	Category    string
	Handler     HandlerFunc
	// AgentPrompt, when non-nil, chains into an agentic conversation turn
	// after the handler completes. The handler runs first (e.g. scaffold),
	// its output is displayed, then AgentPrompt is called to produce the
	// prompt sent to the LLM for the agentic turn.
	AgentPrompt func(args string) string
}

// Registry holds all registered slash commands.
type Registry struct {
	mu       sync.RWMutex
	commands map[string]*Spec
}

// NewRegistry creates a new command registry.
func NewRegistry() *Registry {
	return &Registry{
		commands: make(map[string]*Spec),
	}
}

// Register adds a command to the registry.
func (r *Registry) Register(spec *Spec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands[spec.Name] = spec
}

// Get returns a command by name.
func (r *Registry) Get(name string) (*Spec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	spec, ok := r.commands[name]
	return spec, ok
}

// Execute runs a command by name.
func (r *Registry) Execute(ctx context.Context, name string, args string) (string, error) {
	spec, ok := r.Get(name)
	if !ok {
		return "", fmt.Errorf("unknown command: /%s", name)
	}
	return spec.Handler(ctx, args)
}

// List returns all commands sorted by name.
func (r *Registry) List() []*Spec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	specs := make([]*Spec, 0, len(r.commands))
	for _, spec := range r.commands {
		specs = append(specs, spec)
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return specs
}

// ListByCategory groups commands by category.
func (r *Registry) ListByCategory() map[string][]*Spec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	categories := make(map[string][]*Spec)
	for _, spec := range r.commands {
		cat := spec.Category
		if cat == "" {
			cat = "general"
		}
		categories[cat] = append(categories[cat], spec)
	}
	return categories
}
