package builtins

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	memexgraph "github.com/qiangli/ycode/pkg/memex/graph"
)

func init() { Register(&graphVerb{}) }

type graphVerb struct{}

func (graphVerb) Name() string { return "graph" }
func (graphVerb) Description() string {
	return "Query the code knowledge graph with DQL (read-only)"
}
func (graphVerb) Usage() string { return `yc graph "<DQL>"` }

func (graphVerb) Run(ctx context.Context, args []string, stdio Stdio, _ string) (int, error) {
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
	if _, err := os.Stat(dir); err != nil {
		fmt.Fprintf(stdio.Stderr, "yc graph: graph database not found at %s\n", dir)
		fmt.Fprintln(stdio.Stderr, "  run `ycode serve` first to populate the code knowledge graph")
		return 1, nil
	}

	g, err := memexgraph.Open(memexgraph.Options{Dir: dir, ReadOnly: true})
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

func graphDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home: %w", err)
	}
	return filepath.Join(home, ".agents", "ycode", "projects", "data", "graph"), nil
}
