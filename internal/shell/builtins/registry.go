// Package builtins provides the `yc <verb>` family of in-process commands
// that ycode shell injects into the bash exec path. Foreign agents that
// shell out to ycode get these capabilities (treesitter AST search, repo
// map, code graph, sandbox, browser-use, semantic memory, native git)
// as plain shell commands with no MCP setup required.
//
// Built-ins live under `yc <verb>` to namespace cleanly and stay
// unshadowable by PATH binaries. The interp.ExecHandler middleware
// (Handler) intercepts argv[0] == "yc" before LookPathDir.
//
// To add a built-in: write a Verb implementation, register it in init().
package builtins

import (
	"context"
	"fmt"
	"io"
	"sort"
	"sync"

	"github.com/qiangli/ycode/internal/shell"
)

// Verb is a single yc <name> built-in.
type Verb interface {
	// Name is the subcommand name without the "yc" prefix (e.g. "symbols").
	Name() string

	// Description is the one-line summary shown in `yc help` and the manifest.
	Description() string

	// Usage shows arg syntax, e.g. "yc symbols <path|pattern>".
	Usage() string

	// Run executes the built-in. ctx, args (without "yc <name>"), stdin/out/err,
	// cwd. Returns the desired exit code (0 on success).
	Run(ctx context.Context, args []string, stdio Stdio, cwd string) (int, error)
}

// Stdio bundles per-call I/O streams. Mirrors the bash.Stdio shape so
// adapters compose cleanly.
type Stdio struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

var (
	regMu sync.RWMutex
	reg   = make(map[string]Verb)
)

// Register adds a verb to the global registry. Panics on duplicate name —
// matches mvdan/sh's expectation that built-in names are stable.
func Register(v Verb) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := reg[v.Name()]; dup {
		panic(fmt.Sprintf("ycode shell builtins: duplicate verb %q", v.Name()))
	}
	reg[v.Name()] = v
}

// Lookup returns a verb by name.
func Lookup(name string) (Verb, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	v, ok := reg[name]
	return v, ok
}

// All returns every registered verb sorted by name.
func All() []Verb {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]Verb, 0, len(reg))
	for _, v := range reg {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

func init() {
	// Wire the manifest catalog now that we own the registry.
	shell.SetBuiltinsForManifest(manifestCatalog)
}

func manifestCatalog() []shell.ManifestBuiltin {
	verbs := All()
	out := make([]shell.ManifestBuiltin, 0, len(verbs))
	for _, v := range verbs {
		out = append(out, shell.ManifestBuiltin{
			Name:        "yc " + v.Name(),
			Verb:        v.Name(),
			Description: v.Description(),
			Usage:       v.Usage(),
		})
	}
	return out
}
