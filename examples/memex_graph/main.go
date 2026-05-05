// Command memex_graph is a smoke test that exercises the public surface
// of pkg/memex/graph from outside the ycode tree. It must build and run
// without importing any internal/ packages.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/qiangli/ycode/pkg/memex/graph"
)

func main() {
	dir, err := os.MkdirTemp("", "memex-graph-")
	if err != nil {
		fail(err)
	}
	defer os.RemoveAll(dir)

	g, err := graph.Open(graph.Options{Dir: dir})
	if err != nil {
		fail(err)
	}
	defer g.Close()

	ctx := context.Background()

	// Insert two memory entries with a relation between them.
	if _, err := g.Mutate(ctx, []byte(`
		_:alpha <memory.name> "alpha-decision" .
		_:alpha <memory.type> "feedback" .
		_:alpha <dgraph.type> "Memory" .
		_:beta  <memory.name> "beta-context" .
		_:beta  <memory.type> "reference" .
		_:beta  <dgraph.type> "Memory" .
		_:alpha <memory.related_to> _:beta .
	`)); err != nil {
		fail(err)
	}

	// Query for alpha's outgoing edges.
	body, err := g.Query(ctx, `{
		q(func: eq(memory.name, "alpha-decision")) {
			memory.name
			memory.related_to {
				memory.name
				memory.type
			}
		}
	}`, nil)
	if err != nil {
		fail(err)
	}

	fmt.Println("pkg/memex/graph: DQL round-trip OK")
	fmt.Println(string(body))
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "memex-graph smoke:", err)
	os.Exit(1)
}
