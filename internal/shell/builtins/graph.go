package builtins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/qiangli/ycode/internal/runtime/codegraph"
	memexgraph "github.com/qiangli/ycode/pkg/memex/graph"
)

func init() { Register(&graphVerb{}) }

type graphVerb struct{}

func (graphVerb) Name() string { return "graph" }
func (graphVerb) Description() string {
	return "Query the code knowledge graph with DQL (read-only). Falls back to an ephemeral mirror of .agents/ycode/graph.json when no persistent DB exists."
}
func (graphVerb) Usage() string { return `yc graph "<DQL>"` }

func (graphVerb) Run(ctx context.Context, args []string, stdio Stdio, cwd string) (int, error) {
	if len(args) == 0 {
		fmt.Fprintln(stdio.Stderr, "yc graph: missing DQL query")
		return 2, nil
	}
	dql := args[0]

	dir, err := graphDir()
	if err != nil {
		fmt.Fprintf(stdio.Stderr, "yc graph: %v\n", err)
		return 1, nil
	}

	if _, err := os.Stat(dir); err == nil {
		return runDQL(ctx, dir, true, dql, stdio)
	}

	// Persistent DB missing — try the JSON cache produced by codegraph.Build.
	cachePath := codegraph.CachePath(cwd)
	if _, err := os.Stat(cachePath); err != nil {
		fmt.Fprintf(stdio.Stderr, "yc graph: no persistent graph at %s\n", dir)
		fmt.Fprintln(stdio.Stderr, "  and no JSON cache at .agents/ycode/graph.json")
		fmt.Fprintln(stdio.Stderr, "  run `ycode serve` to populate the persistent graph, or `ycode /init` to build the cache")
		return 1, nil
	}

	ephemeralDir, err := ensureEphemeralMirror(ctx, cwd, cachePath, stdio)
	if err != nil {
		fmt.Fprintf(stdio.Stderr, "yc graph: ephemeral mirror: %v\n", err)
		return 1, nil
	}
	return runDQL(ctx, ephemeralDir, false, dql, stdio)
}

func runDQL(ctx context.Context, dir string, readOnly bool, dql string, stdio Stdio) (int, error) {
	g, err := memexgraph.Open(memexgraph.Options{Dir: dir, ReadOnly: readOnly})
	if err != nil {
		fmt.Fprintf(stdio.Stderr, "yc graph: open: %v\n", err)
		return 1, nil
	}
	defer g.Close()

	out, err := g.Query(ctx, dql, nil)
	if err != nil {
		fmt.Fprintf(stdio.Stderr, "yc graph: query: %v\n", err)
		return 1, nil
	}
	if _, err := stdio.Stdout.Write(out); err != nil {
		return 1, err
	}
	if len(out) > 0 && out[len(out)-1] != '\n' {
		fmt.Fprintln(stdio.Stdout)
	}
	return 0, nil
}

// ensureEphemeralMirror builds (or reuses) a bonsai mirror of
// .agents/ycode/graph.json under $XDG_RUNTIME_DIR/ycode/graph-ephemeral-<hash>/.
// The mirror is rebuilt when the JSON cache is newer than the existing
// mirror's marker file; otherwise it's reused so successive `yc graph`
// calls in the same session don't pay the mirror cost.
func ensureEphemeralMirror(ctx context.Context, cwd, cachePath string, stdio Stdio) (string, error) {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = os.TempDir()
	}
	sum := sha256.Sum256([]byte(cwd))
	dir := filepath.Join(base, "ycode", "graph-ephemeral-"+hex.EncodeToString(sum[:8]))
	marker := filepath.Join(dir, ".source-mtime")

	cacheInfo, err := os.Stat(cachePath)
	if err != nil {
		return "", fmt.Errorf("stat cache: %w", err)
	}

	if data, err := os.ReadFile(marker); err == nil {
		if string(data) == cacheInfo.ModTime().UTC().Format("20060102T150405.000000000Z") {
			return dir, nil
		}
	}

	// Stale or absent mirror — rebuild from JSON cache.
	if err := os.RemoveAll(dir); err != nil {
		return "", fmt.Errorf("clean stale mirror: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create mirror dir: %w", err)
	}

	fmt.Fprintln(stdio.Stderr, "yc graph: building ephemeral mirror from .agents/ycode/graph.json (one-time per change)…")
	gc, err := codegraph.Load(cachePath)
	if err != nil {
		return "", fmt.Errorf("load cache: %w", err)
	}
	if gc == nil {
		return "", fmt.Errorf("graph.json cache empty")
	}

	g, err := memexgraph.Open(memexgraph.Options{Dir: dir})
	if err != nil {
		return "", fmt.Errorf("open mirror: %w", err)
	}
	if err := gc.MirrorTo(ctx, g); err != nil {
		_ = g.Close()
		return "", fmt.Errorf("mirror: %w", err)
	}
	if err := g.Close(); err != nil {
		return "", fmt.Errorf("close mirror: %w", err)
	}

	if err := os.WriteFile(marker, []byte(cacheInfo.ModTime().UTC().Format("20060102T150405.000000000Z")), 0o600); err != nil {
		return "", fmt.Errorf("write marker: %w", err)
	}
	return dir, nil
}

func graphDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home: %w", err)
	}
	return filepath.Join(home, ".agents", "ycode", "projects", "data", "graph"), nil
}
