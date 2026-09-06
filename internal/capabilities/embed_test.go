package capabilities

import (
	"strings"
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

func TestYAMLHarnessCapabilityIsCanonical(t *testing.T) {
	registry, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	harness := registry.ByID("yaml_harness")
	if harness == nil || !strings.Contains(harness.Summary, "agent.yaml") {
		t.Fatalf("yaml_harness capability does not describe the canonical lifecycle: %#v", harness)
	}
	if len(harness.Config) != 0 {
		t.Fatalf("yaml_harness must not expose legacy host config: %v", harness.Config)
	}
	for _, id := range []string{"lifecycle", "bashy", "resources", "operability"} {
		capability := registry.ByID(id)
		if capability == nil {
			t.Errorf("missing YAML harness capability family %q", id)
			continue
		}
		if strings.Contains(strings.ToLower(capability.Summary), "autopilot") || strings.Contains(strings.ToLower(capability.Summary), "native tool") {
			t.Errorf("capability %q still advertises legacy harness policy: %q", id, capability.Summary)
		}
	}
}
