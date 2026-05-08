package builtins

import (
	"context"
	"fmt"
	"text/tabwriter"
)

func init() { Register(&helpVerb{}) }

type helpVerb struct{}

func (helpVerb) Name() string        { return "help" }
func (helpVerb) Description() string { return "List yc <verb> built-ins with their usage" }
func (helpVerb) Usage() string       { return "yc help" }

func (helpVerb) Run(_ context.Context, _ []string, stdio Stdio, _ string) (int, error) {
	fmt.Fprintln(stdio.Stdout, "yc — ycode shell built-ins")
	fmt.Fprintln(stdio.Stdout, "")

	w := tabwriter.NewWriter(stdio.Stdout, 0, 4, 2, ' ', 0)
	for _, v := range All() {
		fmt.Fprintf(w, "  %s\t%s\n", v.Usage(), v.Description())
	}
	_ = w.Flush()

	fmt.Fprintln(stdio.Stdout, "")
	fmt.Fprintln(stdio.Stdout, "JSON catalog: ycode shell --manifest")
	fmt.Fprintln(stdio.Stdout, "Agent integration: see docs/shell-agent.md")
	return 0, nil
}
