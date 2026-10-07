package api

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Exercise actual catalog parsing and all selectors with each supported tool
// spelling. Different generic/provider IDs catch accidental CLI-ID selection.
func TestFleetBackendSelectors(t *testing.T) {
	for _, tool := range []string{"genie", "ycode"} {
		t.Run(tool, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("BASHY_FLEET_DIR", root)
			t.Setenv("BASHY_FLEET_SEEDS", "off")
			for _, noun := range []string{"TOOLS", "MODELS", "AGENTS"} {
				t.Setenv("BASHY_"+noun+"_PATH", "")
				t.Setenv("BASHY_"+noun+"_DIR", filepath.Join(root, noun))
				if err := os.MkdirAll(filepath.Join(root, noun), 0700); err != nil {
					t.Fatal(err)
				}
			}
			write := func(noun, name, text string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(root, noun, name+".yaml"), []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
			}
			write("MODELS", "small", "name: small\nband: 2\nmodel: generic-small\nids:\n  ycode: provider-small\n  genie: cli-small\n")
			write("MODELS", "large", "name: large\nband: 4\nmodel: generic-large\nids:\n  ycode: provider-large\n  genie: cli-large\n")
			write("AGENTS", "fixture", fmt.Sprintf(`agents:
  - name: backend-small
    tool: %s
    model: small
  - name: backend-large
    tool: %s
    model: large
  - name: backend-cascade
    tool: %s
    model: small
    band: 4
    band_source: cascade
    base: backend-small
    escalation: [backend-large]
  - name: external-large
    tool: codex
    model: large
`, tool, tool, tool))

			for selector, want := range map[string]string{
				"backend-small": "provider-small",
				"small":         "provider-small",
				"L2":            "provider-large",
				"literal-model": "literal-model",
			} {
				if got, _ := ResolveFleetModel(selector); got != want {
					t.Errorf("ResolveFleetModel(%q) = %q, want %q", selector, got, want)
				}
			}
			ladder, ok := ResolveCascadeLadder("backend-cascade")
			if !ok || !reflect.DeepEqual(ladder, []string{"provider-small", "provider-large"}) {
				t.Errorf("cascade = %v, %t; want both provider IDs", ladder, ok)
			}
			if a, ok := YcodeAgentForModel("provider-large"); !ok || a.Name != "backend-large" || a.Tool != tool {
				t.Errorf("reverse lookup = %+v, %t; want backend-large using %s", a, ok, tool)
			}
			if a, err := ResolveFleetAgent("L4"); err != nil || a.Name != "external-large" {
				t.Errorf("agent switch = %+v, %v; want external-large", a, err)
			}
		})
	}
}
