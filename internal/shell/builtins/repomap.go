package builtins

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/qiangli/ycode/internal/runtime/repomap"
)

func init() { Register(&repomapVerb{}) }

type repomapVerb struct{}

func (repomapVerb) Name() string { return "repomap" }
func (repomapVerb) Description() string {
	return "Generate a token-budgeted file→symbol overview of the workspace"
}
func (repomapVerb) Usage() string {
	return "yc repomap [path] [--budget=N] [--query=<text>] [--json]"
}

func (repomapVerb) Run(_ context.Context, args []string, stdio Stdio, cwd string) (int, error) {
	asJSON := false
	target := ""
	opts := repomap.DefaultOptions()

	for _, a := range args {
		switch {
		case a == "--json":
			asJSON = true
		case len(a) > len("--budget=") && a[:len("--budget=")] == "--budget=":
			n, err := strconv.Atoi(a[len("--budget="):])
			if err != nil || n <= 0 {
				fmt.Fprintf(stdio.Stderr, "yc repomap: invalid --budget value %q\n", a)
				return 2, nil
			}
			opts.MaxTokens = n
		case len(a) > len("--query=") && a[:len("--query=")] == "--query=":
			opts.RelevanceQuery = a[len("--query="):]
		default:
			if target == "" {
				target = a
			}
		}
	}
	if target == "" {
		target = cwd
		if target == "" {
			target = "."
		}
	} else {
		target = resolvePath(target, cwd)
	}

	rm, err := repomap.Generate(target, opts)
	if err != nil {
		return 1, err
	}

	if asJSON {
		enc := json.NewEncoder(stdio.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rm.Entries)
		return 0, nil
	}
	fmt.Fprint(stdio.Stdout, rm.Format())
	return 0, nil
}
