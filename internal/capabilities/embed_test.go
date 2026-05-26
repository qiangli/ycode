package capabilities

import (
	"testing"
)

// TestRegistryParses asserts the YAML is well-formed and the schema
// version is set. Cobra / config / MCP cross-references are validated
// by cmd/ycode/capabilities_test.go (they need rootCmd + the Config
// struct, both of which live outside this package).
func TestRegistryParses(t *testing.T) {
	r, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if r.SchemaVersion != 1 {
		t.Fatalf("schemaVersion = %d, want 1", r.SchemaVersion)
	}
	if len(r.Capabilities) == 0 {
		t.Fatal("no capabilities declared")
	}

	// Every capability must have an id and summary; id must be unique.
	seen := map[string]bool{}
	for i, c := range r.Capabilities {
		if c.ID == "" {
			t.Errorf("capability #%d: empty id", i)
		}
		if seen[c.ID] {
			t.Errorf("capability %q: duplicate id", c.ID)
		}
		seen[c.ID] = true
		if c.Summary == "" {
			t.Errorf("capability %q: empty summary", c.ID)
		}
		if len(c.Audience) == 0 {
			t.Errorf("capability %q: empty audience", c.ID)
		}
		for _, aud := range c.Audience {
			switch aud {
			case "agent", "human", "ci", "runtime":
			default:
				t.Errorf("capability %q: unknown audience %q (allowed: agent, human, ci, runtime)",
					c.ID, aud)
			}
		}
	}
}
