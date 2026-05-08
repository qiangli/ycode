package shell

import (
	"encoding/json"
	"io"
)

// Manifest is the JSON capability catalog emitted by `ycode shell --manifest`.
// Foreign agents (Claude Code, OpenCode, Codex) read this once at startup
// to learn the surface — equivalent to MCP's tools/list, but without an
// RPC server.
type Manifest struct {
	Version         string              `json:"version"`
	Sentinels       []string            `json:"sentinels"`
	PermissionModes []string            `json:"permission_modes"`
	Modes           []string            `json:"modes"`
	Builtins        []ManifestBuiltin   `json:"builtins"`
	Skills          []ManifestSkill     `json:"skills"`
	SlashCommands   []ManifestSlashSpec `json:"slash_commands"`
	Hints           []ManifestHint      `json:"hints,omitempty"`
}

// ManifestBuiltin describes a single yc <verb> built-in. Schema is a
// loose param descriptor — agents that want full JSON-Schema can synthesize.
type ManifestBuiltin struct {
	Name        string `json:"name"`
	Verb        string `json:"verb"`
	Description string `json:"description"`
	Usage       string `json:"usage"`
}

// ManifestSkill describes a discoverable skill (@<id> or @<path>).
type ManifestSkill struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ManifestSlashSpec describes a slash command available in shell mode.
type ManifestSlashSpec struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	ShellSafe   bool   `json:"shell_safe"`
}

// ManifestHint mirrors the hint-engine catalog so agents that prefer
// to do their own routing can read it once and skip the round-trip.
// Populated when the agentmode hint engine is wired.
type ManifestHint struct {
	ID       string `json:"id"`
	Pattern  string `json:"pattern"`
	Suggest  string `json:"suggest"`
	Category string `json:"category"`
}

// BuildManifest assembles a Manifest from the runtime's registries.
// Hint catalog is filled in by the agentmode package via SetHintCatalog
// to avoid an import cycle.
func BuildManifest(rt *ShellRuntime) Manifest {
	m := Manifest{
		Version:         "0.1.0",
		Sentinels:       []string{"/", "@", "!", "?"},
		PermissionModes: []string{"read-only", "workspace-write", "danger-full-access"},
		Modes:           []string{"interactive", "non-interactive", "sandbox"},
	}
	if rt != nil {
		if reg := rt.Registry(); reg != nil {
			for _, spec := range reg.List() {
				m.SlashCommands = append(m.SlashCommands, ManifestSlashSpec{
					Name:        spec.Name,
					Description: spec.Description,
					ShellSafe:   spec.ShellSafe,
				})
			}
		}
		if skills := rt.Skills(); skills != nil {
			for _, name := range skills.List() {
				m.Skills = append(m.Skills, ManifestSkill{Name: name})
			}
		}
	}
	m.Builtins = append(m.Builtins, listBuiltinsForManifest()...)
	m.Hints = manifestHints
	return m
}

// listBuiltinsForManifest returns the manifest entries for registered
// yc <verb> built-ins. The actual registry lives in internal/shell/builtins;
// the import is wired via a package-level setter to avoid a cycle.
func listBuiltinsForManifest() []ManifestBuiltin {
	if builtinsForManifest == nil {
		return nil
	}
	return builtinsForManifest()
}

var (
	builtinsForManifest func() []ManifestBuiltin
	manifestHints       []ManifestHint
)

// SetBuiltinsForManifest is called by internal/shell/builtins/init() to
// wire the built-in catalog into the manifest. Avoids the cycle that
// would arise if internal/shell imported internal/shell/builtins.
func SetBuiltinsForManifest(fn func() []ManifestBuiltin) {
	builtinsForManifest = fn
}

// SetHintCatalogForManifest is called by internal/shell/agentmode/init().
func SetHintCatalogForManifest(hints []ManifestHint) {
	manifestHints = hints
}

// WriteManifest writes the JSON manifest to w with newline-terminated
// pretty-printed output. Used by `ycode shell --manifest`.
func WriteManifest(rt *ShellRuntime, w io.Writer) error {
	m := BuildManifest(rt)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(m)
}
