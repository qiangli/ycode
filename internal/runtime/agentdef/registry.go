package agentdef

import (
	"fmt"
	"sort"
	"sync"
)

// Registry holds loaded agent definitions and provides lookup by name.
// It is safe for concurrent use.
type Registry struct {
	mu   sync.RWMutex
	defs map[string]*AgentDefinition
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		defs: make(map[string]*AgentDefinition),
	}
}

// NewRegistryFromDefs creates a registry pre-loaded with definitions.
func NewRegistryFromDefs(defs []*AgentDefinition) (*Registry, error) {
	r := NewRegistry()
	for _, d := range defs {
		if err := r.Register(d); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// Register adds an agent definition to the registry.
// Returns an error if a definition with the same name already exists.
func (r *Registry) Register(d *AgentDefinition) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.defs[d.Name]; exists {
		return fmt.Errorf("agent definition %q already registered", d.Name)
	}
	r.defs[d.Name] = d
	return nil
}

// RegisterOrReplace adds or replaces an agent definition.
func (r *Registry) RegisterOrReplace(d *AgentDefinition) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.defs[d.Name] = d
}

// Lookup returns the agent definition for the given name.
func (r *Registry) Lookup(name string) (*AgentDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.defs[name]
	return d, ok
}

// List returns all registered definitions sorted by name.
func (r *Registry) List() []*AgentDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*AgentDefinition, 0, len(r.defs))
	for _, d := range r.defs {
		result = append(result, d)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// Names returns all registered definition names sorted.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.defs))
	for name := range r.defs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Len returns the number of registered definitions.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.defs)
}

// ResolveEmbeds resolves embed references for all definitions in the registry.
// Each definition's Embed field lists parent definition names to inherit from.
// Fields are merged in embed order (first embed has highest priority for conflicts).
func (r *Registry) ResolveEmbeds() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for name, def := range r.defs {
		if err := r.resolveEmbed(def, make(map[string]bool)); err != nil {
			return fmt.Errorf("resolve embeds for %q: %w", name, err)
		}
	}
	return nil
}

// resolveEmbed recursively resolves embeds, detecting cycles.
func (r *Registry) resolveEmbed(def *AgentDefinition, visited map[string]bool) error {
	if len(def.Embed) == 0 {
		return nil
	}
	if visited[def.Name] {
		return fmt.Errorf("circular embed: %q", def.Name)
	}
	visited[def.Name] = true

	for _, parentName := range def.Embed {
		parent, ok := r.defs[parentName]
		if !ok {
			return fmt.Errorf("embedded agent %q not found", parentName)
		}
		// Resolve parent's embeds first.
		if err := r.resolveEmbed(parent, visited); err != nil {
			return err
		}
		def.MergeFrom(parent)
	}

	return nil
}

// FindTriggered returns definitions whose triggers match the given text.
func (r *Registry) FindTriggered(text string) []*AgentDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var matched []*AgentDefinition
	for _, d := range r.defs {
		if d.MatchesTrigger(text) {
			matched = append(matched, d)
		}
	}
	return matched
}

// Load is a convenience that loads definitions from directories and creates a registry.
// Directories are processed in order; later dirs override earlier by agent name.
// Embeds are resolved after loading.
func Load(dirs ...string) (*Registry, error) {
	defs, err := LoadPaths(dirs...)
	if err != nil {
		return nil, err
	}
	reg, err := NewRegistryFromDefs(defs)
	if err != nil {
		return nil, err
	}
	if err := reg.ResolveEmbeds(); err != nil {
		return nil, err
	}
	return reg, nil
}
