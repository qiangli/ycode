// Package capabilities is the embedded capability registry — the single
// declared mapping of capability families to their CLI / MCP / HTTP /
// config surfaces. Source of truth lives in registry.yaml in this
// directory; the lint gate (cmd/ycode/capabilities_test.go) asserts
// every declared surface resolves at build time.
//
// See registry.yaml for the curation rules. This package only loads
// and exposes the data — it has no opinion on what belongs where.
package capabilities

import (
	_ "embed"
	"fmt"
	"sync"

	"gopkg.in/yaml.v3"
)

//go:embed registry.yaml
var registryYAML []byte

// Registry is the top-level parsed registry document.
type Registry struct {
	SchemaVersion int          `yaml:"schemaVersion"`
	Capabilities  []Capability `yaml:"capabilities"`
}

// Capability describes one capability family across every surface.
// Field names mirror the YAML keys exactly. Empty slices are valid
// and meaningful — they mean "intentionally no surface of this type".
type Capability struct {
	ID       string   `yaml:"id"`
	Summary  string   `yaml:"summary"`
	Audience []string `yaml:"audience"`
	CLI      []string `yaml:"cli"`
	MCP      []string `yaml:"mcp"`
	HTTP     []string `yaml:"http"`
	Config   []string `yaml:"config"`
	// Gaps lists intentional asymmetries (e.g. "stdio MCP lacks this
	// family — requires ycode serve"). The lint suppresses warnings
	// that match a declared gap.
	Gaps []string `yaml:"gaps,omitempty"`
}

var (
	loadOnce sync.Once
	loaded   *Registry
	loadErr  error
)

// Load parses the embedded registry. The result is cached for the
// lifetime of the process; any parse error is sticky and returned on
// every subsequent call (the lint will have caught the malformed YAML
// in CI, so the runtime path only fails when running against a broken
// embed — at which point all bets are off anyway).
func Load() (*Registry, error) {
	loadOnce.Do(func() {
		loaded = &Registry{}
		loadErr = yaml.Unmarshal(registryYAML, loaded)
		if loadErr == nil && loaded.SchemaVersion == 0 {
			loadErr = fmt.Errorf("registry.yaml missing schemaVersion")
		}
	})
	return loaded, loadErr
}

// MustLoad is the panic-on-error variant used by callers that have
// already passed the lint (i.e. the binary built — so the YAML is
// valid). Test code prefers Load.
func MustLoad() *Registry {
	r, err := Load()
	if err != nil {
		panic("capabilities.MustLoad: " + err.Error())
	}
	return r
}

// ByID returns the named capability or nil. Lookup is linear (the
// registry is small enough that an index isn't worth maintaining).
func (r *Registry) ByID(id string) *Capability {
	for i := range r.Capabilities {
		if r.Capabilities[i].ID == id {
			return &r.Capabilities[i]
		}
	}
	return nil
}

// AllCLIVerbs returns every CLI verb declared across every capability,
// flat. Useful for the inverse lint check ("every cobra command should
// belong to some capability").
func (r *Registry) AllCLIVerbs() []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range r.Capabilities {
		for _, v := range c.CLI {
			if !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	return out
}

// AllMCPTools returns every MCP tool name declared across every
// capability, flat.
func (r *Registry) AllMCPTools() []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range r.Capabilities {
		for _, t := range c.MCP {
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}

// AllConfigPaths returns every config struct path declared, flat.
func (r *Registry) AllConfigPaths() []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range r.Capabilities {
		for _, p := range c.Config {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}
