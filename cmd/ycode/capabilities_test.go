package main

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/capabilities"
)

// TestCapabilityRegistry is the cross-cutting consistency gate. For
// every declared capability in internal/capabilities/registry.yaml the
// lint asserts:
//
//   - cli verbs resolve to real subcommands under rootCmd
//   - the legacy config registry is empty; runtime policy lives in agent.yaml
//
// When this test fails: the right fix is almost never "loosen the
// lint". Either restore the missing surface, or update the registry
// to reflect the new reality. If you've genuinely retired a capability,
// remove the entry. If you've moved it, update the path.
func TestCapabilityRegistry(t *testing.T) {
	reg, err := capabilities.Load()
	if err != nil {
		t.Fatalf("capabilities.Load: %v", err)
	}

	cobraVerbs := collectTopLevelVerbs(rootCmd)

	var violations []string

	for _, c := range reg.Capabilities {
		for _, verb := range c.CLI {
			// Accept the "verb/subverb" form for capabilities that own
			// a specific sub-subcommand (rare; see registry.yaml rule 3).
			top := verb
			if idx := strings.Index(verb, "/"); idx >= 0 {
				top = verb[:idx]
			}
			if !cobraVerbs[top] {
				violations = append(violations,
					fmt.Sprintf("[%s] cli: `ycode %s` is declared but no such top-level cobra verb exists",
						c.ID, top))
			}
		}

		for _, path := range c.Config {
			violations = append(violations, fmt.Sprintf("[%s] legacy config path %q is forbidden; declare it in agent.yaml", c.ID, path))
		}
	}

	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("capability registry drift detected:\n  %s",
			strings.Join(violations, "\n  "))
	}
}

// TestEveryTopLevelCobraVerbIsClaimed is the INVERSE lint: every cobra
// command MUST be owned by some capability. Catches the drift where
// someone adds a new `ycode foo` verb but forgets to declare it.
//
// Allowlist (verbs intentionally unclaimed) is intentionally tiny:
//
//   - help, completion: cobra built-ins, not capabilities.
//   - shell-trace: internal shim called by wrap/sitecustomize, not
//     user-facing — documented in cmd/ycode/main.go.
func TestEveryTopLevelCobraVerbIsClaimed(t *testing.T) {
	reg, err := capabilities.Load()
	if err != nil {
		t.Fatalf("capabilities.Load: %v", err)
	}
	claimed := map[string]bool{}
	for _, v := range reg.AllCLIVerbs() {
		top := v
		if idx := strings.Index(v, "/"); idx >= 0 {
			top = v[:idx]
		}
		claimed[top] = true
	}
	intentionallyUnclaimed := map[string]bool{
		"help":                 true,
		"completion":           true,
		"shell-trace":          true,
		"internal-shell-trace": true,
	}
	var orphans []string
	for _, c := range rootCmd.Commands() {
		name := c.Name()
		if intentionallyUnclaimed[name] || claimed[name] {
			continue
		}
		orphans = append(orphans, name)
	}
	if len(orphans) > 0 {
		sort.Strings(orphans)
		t.Fatalf("unclaimed cobra verbs (add to registry.yaml or to intentionallyUnclaimed):\n  %s",
			strings.Join(orphans, "\n  "))
	}
}

// collectTopLevelVerbs returns the set of immediate subcommand names
// under root. Only the first level — sub-subcommands ("model list")
// are NOT included; the registry's `cli:` field is top-level only by
// design (rule #3).
func collectTopLevelVerbs(root *cobra.Command) map[string]bool {
	out := map[string]bool{}
	for _, c := range root.Commands() {
		out[c.Name()] = true
	}
	return out
}
