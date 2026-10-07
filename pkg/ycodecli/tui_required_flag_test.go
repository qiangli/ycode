package ycodecli

import (
	"testing"

	harnessspec "github.com/qiangli/ycode/internal/harness/spec"
)

// A slash carries positional arguments only, so a required authored flag on
// the command (or inherited from an ancestor) refuses the shortcut.
func TestRequiredFlagRefusesSlashShortcut(t *testing.T) {
	root := harnessspec.CLICommand{
		Flags: []harnessspec.CLIFlag{{Name: "tenant", Required: true, Scope: "local"}},
		Commands: []harnessspec.CLICommand{
			{Name: "plan"},
			{Name: "model", Flags: []harnessspec.CLIFlag{{Name: "org", Required: true, Scope: "inherited"}},
				Commands: []harnessspec.CLICommand{{Name: "use"}}},
			{Name: "continue", Flags: []harnessspec.CLIFlag{{Name: "why", Required: true, Scope: "local"}}},
		},
	}
	for path, want := range map[string]string{"plan": "", "model use": "org", "continue": "why"} {
		got, ok := requiredFlag(root, splitPath(path))
		if got != want || ok != (want != "") {
			t.Errorf("requiredFlag(%q) = %q, %v; want %q", path, got, ok, want)
		}
	}
}

func splitPath(s string) []string {
	if s == "model use" {
		return []string{"model", "use"}
	}
	return []string{s}
}
