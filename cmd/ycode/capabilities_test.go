package main

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/capabilities"
	"github.com/qiangli/ycode/internal/runtime/config"
)

// TestCapabilityRegistry is the cross-cutting consistency gate. For
// every declared capability in internal/capabilities/registry.yaml the
// lint asserts:
//
//   - cli verbs resolve to real subcommands under rootCmd
//   - config paths reflect-resolve on *config.Config
//
// HTTP routes are NOT validated — they're wired programmatically in
// serve.go and parsing the AST is brittle. See registry.yaml rule #6.
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
	cfgPaths := collectConfigPaths(reflect.TypeOf(config.Config{}), "")

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
			if !configPathExists(reflect.TypeOf(config.Config{}), path) {
				violations = append(violations,
					fmt.Sprintf("[%s] config: path %q does not resolve on config.Config (known paths: %d)",
						c.ID, path, len(cfgPaths)))
			}
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

// configPathExists walks a dotted struct path on the Config root and
// returns true if every segment resolves to a field. Pointer indirection
// is automatic. The check matches Go field names (case-sensitive), NOT
// json tags — see registry.yaml rule 5.
func configPathExists(root reflect.Type, path string) bool {
	cur := root
	for _, seg := range strings.Split(path, ".") {
		for cur.Kind() == reflect.Ptr {
			cur = cur.Elem()
		}
		if cur.Kind() != reflect.Struct {
			return false
		}
		f, ok := cur.FieldByName(seg)
		if !ok {
			return false
		}
		cur = f.Type
	}
	return true
}

// collectConfigPaths flattens the Config struct to a set of dotted
// paths for the diagnostic message in TestCapabilityRegistry. Capped at
// one level of nesting to avoid explosion; the lint resolves arbitrary
// depth via configPathExists.
func collectConfigPaths(t reflect.Type, prefix string) []string {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	var out []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		path := f.Name
		if prefix != "" {
			path = prefix + "." + f.Name
		}
		out = append(out, path)
		// One level deep only.
		if prefix == "" {
			out = append(out, collectConfigPaths(f.Type, path)...)
		}
	}
	return out
}
