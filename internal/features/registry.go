package features

import (
	_ "embed"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

//go:embed registry.yaml
var embeddedRegistry []byte

// Load parses the registry embedded in the binary at build time. This is the
// canonical entry point for runtime code (`ycode features list`, etc.).
func Load() (*Registry, error) {
	return Parse(embeddedRegistry)
}

// LoadFromPath parses a registry file from disk. Used by the verify CI gate
// when checking the on-disk source rather than the embedded copy.
func LoadFromPath(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return Parse(data)
}

// Parse decodes raw YAML into a Registry and validates structural invariants.
func Parse(data []byte) (*Registry, error) {
	var r Registry
	if err := yaml.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse registry: %w", err)
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return &r, nil
}

// Validate enforces structural rules: every feature has a name, no duplicates,
// every tier is recognized.
func (r *Registry) Validate() error {
	seen := make(map[string]bool, len(r.Features))
	for i, f := range r.Features {
		if f.Name == "" {
			return fmt.Errorf("feature[%d]: missing name", i)
		}
		if seen[f.Name] {
			return fmt.Errorf("duplicate feature: %s", f.Name)
		}
		seen[f.Name] = true
		if !f.Tier.Valid() {
			return fmt.Errorf("feature %s: invalid tier %q (must be stable, experimental, or wip)", f.Name, f.Tier)
		}
	}
	return nil
}

// ByTier returns features at the given tier in registry order.
func (r *Registry) ByTier(t Tier) []Feature {
	var out []Feature
	for _, f := range r.Features {
		if f.Tier == t {
			out = append(out, f)
		}
	}
	return out
}

// Get returns the feature with the given name, or false if not found.
func (r *Registry) Get(name string) (Feature, bool) {
	for _, f := range r.Features {
		if f.Name == name {
			return f, true
		}
	}
	return Feature{}, false
}

// Verify checks the registry against the working tree at root: every declared
// file path must exist. Richer checks (integration test presence, dogfood
// recency, failure-mode doc entry) are added incrementally and run by CI.
func Verify(r *Registry, root string) []string {
	var issues []string
	for _, f := range r.Features {
		for _, fp := range f.Files {
			path := fp
			if root != "" {
				path = root + string(os.PathSeparator) + fp
			}
			if _, err := os.Stat(path); err != nil {
				issues = append(issues, fmt.Sprintf("%s: missing file %s", f.Name, fp))
			}
		}
	}
	return issues
}
