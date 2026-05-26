package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/runtime/mcp"
	"github.com/qiangli/ycode/internal/tools"
)

// `ycode tools` — operator surface for "what tools does this binary expose,
// to whom, with what permission?". The agent-facing complement is
// `ycode docs` (curated capability prompts) and `mcp__ycode__list_docs`.
//
// Three surfaces are enumerated:
//
//   - MCP: every tool from the always-on handler inventory (see
//     handlers_inventory.go). Mirrors what foreign agents see in
//     tools/list against `ycode mcp serve`.
//   - Internal: every tool registered into tools.Registry — the in-process
//     agent loop's surface. Foreign agents do NOT see these directly;
//     they're called by ycode's own runtime.
//   - CLI: every top-level cobra subcommand. The human-facing surface.
//
// The output is intentionally flat — no nested grouping, no truncation
// beyond a description ellipsis. Operators pipe through grep / awk;
// pretty output is the agent doc system's job.
func newToolsCmd() *cobra.Command {
	var (
		showMCP, showInternal, showCLI bool
		showAll                        bool
		jsonOut                        bool
		filter                         string
	)
	cmd := &cobra.Command{
		Use:   "tools",
		Short: "List the tool surfaces this binary exposes (MCP, internal, CLI)",
		Long: "Operator-side inventory of ycode's tool surfaces. Use --mcp / --internal / --cli " +
			"to scope, --json for machine-readable, --filter to substring-match names. " +
			"For the agent-facing equivalents see `ycode docs` and `mcp__ycode__list_docs`.",
	}
	cmd.AddCommand(&cobra.Command{
		Use:          "list",
		Short:        "List tools across MCP / internal / CLI surfaces",
		SilenceUsage: true,
		RunE: func(c *cobra.Command, _ []string) error {
			// Default to all surfaces when no scope flag set; --all is
			// retained as an explicit synonym for scripts that want to
			// document intent.
			if !showMCP && !showInternal && !showCLI {
				showAll = true
			}
			scopes := scopeSet{mcp: showMCP || showAll, internal: showInternal || showAll, cli: showCLI || showAll}
			return runToolsList(c.OutOrStdout(), scopes, jsonOut, filter)
		},
	})
	cmd.PersistentFlags().BoolVar(&showMCP, "mcp", false, "Include MCP tool surface (always-on handlers)")
	cmd.PersistentFlags().BoolVar(&showInternal, "internal", false, "Include in-process agent-loop tool registry")
	cmd.PersistentFlags().BoolVar(&showCLI, "cli", false, "Include top-level cobra subcommands")
	cmd.PersistentFlags().BoolVar(&showAll, "all", false, "Equivalent to --mcp --internal --cli (default when no scope flag)")
	cmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "Machine-readable JSON")
	cmd.PersistentFlags().StringVar(&filter, "filter", "", "Substring match on tool name")
	return cmd
}

type scopeSet struct {
	mcp, internal, cli bool
}

type toolRow struct {
	Surface     string `json:"surface"`     // "mcp" | "internal" | "cli"
	Name        string `json:"name"`        // tool name (mcp/internal) or verb (cli)
	Permission  string `json:"permission"`  // ReadOnly / WorkspaceWrite / DangerFullAccess / "" for cli
	Source      string `json:"source"`      // handler family (mcp), spec source (internal), or "" (cli)
	Description string `json:"description"` // one-liner; truncated in text mode
}

func runToolsList(w io.Writer, scopes scopeSet, jsonOut bool, filter string) error {
	var rows []toolRow
	if scopes.mcp {
		rows = append(rows, collectMCPRows()...)
	}
	if scopes.internal {
		rows = append(rows, collectInternalRows()...)
	}
	if scopes.cli {
		rows = append(rows, collectCLIRows()...)
	}
	if filter != "" {
		needle := strings.ToLower(filter)
		filtered := rows[:0]
		for _, r := range rows {
			if strings.Contains(strings.ToLower(r.Name), needle) {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Surface != rows[j].Surface {
			return rows[i].Surface < rows[j].Surface
		}
		return rows[i].Name < rows[j].Name
	})

	if jsonOut {
		out, err := json.MarshalIndent(rows, "", "  ")
		if err != nil {
			return err
		}
		_, err = w.Write(append(out, '\n'))
		return err
	}

	if len(rows) == 0 {
		fmt.Fprintln(w, "(no tools)")
		return nil
	}
	for _, r := range rows {
		desc := truncateForInventory(strings.ReplaceAll(r.Description, "\n", " "), 80)
		perm := r.Permission
		if perm == "" {
			perm = "-"
		}
		src := r.Source
		if src == "" {
			src = "-"
		}
		fmt.Fprintf(w, "%-9s  %-30s  %-18s  %-12s  %s\n",
			r.Surface, r.Name, perm, src, desc)
	}
	return nil
}

func collectMCPRows() []toolRow {
	var out []toolRow
	for _, h := range alwaysOnMCPHandlers() {
		family := handlerFamily(h)
		for _, t := range h.ListTools() {
			out = append(out, toolRow{
				Surface:     "mcp",
				Name:        t.Name,
				Permission:  permissionForTool(h, t.Name),
				Source:      family,
				Description: t.Description,
			})
		}
	}
	return out
}

// handlerFamily — see truncateForInventory comment for naming convention.

func collectInternalRows() []toolRow {
	r := tools.NewRegistry()
	tools.RegisterBuiltins(r)
	var out []toolRow
	for _, spec := range append(r.AlwaysAvailable(), r.Deferred()...) {
		out = append(out, toolRow{
			Surface:     "internal",
			Name:        spec.Name,
			Permission:  spec.RequiredMode.String(),
			Source:      string(spec.Source),
			Description: spec.Description,
		})
	}
	return out
}

func collectCLIRows() []toolRow {
	var out []toolRow
	for _, c := range rootCmd.Commands() {
		if c.Hidden {
			continue
		}
		out = append(out, toolRow{
			Surface:     "cli",
			Name:        c.Name(),
			Description: c.Short,
		})
	}
	return out
}

// handlerFamily returns the Go package name of the handler — the most
// useful single label for grouping (every package owns one handler in
// practice). Pure presentation; not a stable contract.
func handlerFamily(h any) string {
	// fmt's %T yields e.g. "*treesitter.MCPHandler" or "*docs.MCPHandler".
	// The package name is the token between the last "*" and the last ".".
	t := fmt.Sprintf("%T", h)
	t = strings.TrimPrefix(t, "*")
	if dot := strings.LastIndex(t, "."); dot >= 0 {
		t = t[:dot]
	}
	if t == "" {
		return "handler"
	}
	return t
}

// permissionForTool consults the handler's optional PermissionAware
// interface. Handlers that don't implement it are treated as ReadOnly
// (matching the gate's default in internal/runtime/mcp/permission.go).
func permissionForTool(h mcp.ServerHandler, name string) string {
	if pa, ok := h.(mcp.PermissionAware); ok {
		return string(pa.RequiredMode(name))
	}
	return string(mcp.ModeReadOnly)
}

func truncateForInventory(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n < 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}
