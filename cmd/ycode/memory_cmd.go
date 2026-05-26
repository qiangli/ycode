package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/pkg/memex/memory"
)

// `ycode memory` — operator-facing memex inspection.
//
// Memex (internal/runtime/memexmcp + pkg/memex/memory) is the persistent
// agent-memory layer; today it's reachable from:
//
//   - the in-process agent loop (memory_save / memory_recall / ...
//     tools in internal/tools)
//   - foreign agents via MCP (memex_recall, memex_save, ...)
//   - the embedded /memos/ HTTP UI
//
// There was NO CLI surface for an operator to bulk-list, export, or
// purge memory. This command closes that gap. Subcommands:
//
//   list    — print all memories (project + global). --json for machine-readable.
//   show    — print one memory by name.
//   forget  — delete a memory by name.
//   export  — write all memories as a single JSON document to stdout or a file.
//
// Import / restore is intentionally NOT exposed yet — it requires
// careful conflict handling and provenance preservation that the
// memex Save() path doesn't gate for. Manual `cat backup.json |
// jq ... | xargs ycode memory save` is the documented workaround.
//
// The command uses the same NewManagerWithGlobal call as newApp() so
// the operator view matches what the agent sees.
func newMemoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Inspect and manage memex memory (list / show / forget / export)",
		Long: "Operator surface for the persistent memex memory layer. " +
			"For agent-callable equivalents see the memex_* MCP tools and " +
			"the in-session memory_save / memory_recall tools.",
	}
	cmd.AddCommand(
		newMemoryListCmd(),
		newMemoryShowCmd(),
		newMemoryForgetCmd(),
		newMemoryExportCmd(),
	)
	return cmd
}

// openMemoryManagerForCLI builds the same memex.Manager newApp() uses,
// rooted at ~/.agents/ycode/memory (global) + <cwd>/.agents/ycode/memory
// (project). Heavy components (Bleve searcher, vector index, dreamer)
// are NOT wired — the CLI operates on the raw store and doesn't need
// semantic search. Mirrors openMemexForMCP for the stdio MCP path.
func openMemoryManagerForCLI() (*memory.Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("locate home: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getwd: %w", err)
	}
	globalDir := filepath.Join(home, ".agents", "ycode", "memory")
	projectDir := filepath.Join(cwd, ".agents", "ycode", "memory")
	return memory.NewManagerWithGlobal(globalDir, projectDir)
}

func newMemoryListCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:          "list",
		Short:        "List every memory (project + global)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mgr, err := openMemoryManagerForCLI()
			if err != nil {
				return err
			}
			mems, err := mgr.All()
			if err != nil {
				return err
			}
			sort.Slice(mems, func(i, j int) bool { return mems[i].Name < mems[j].Name })
			return writeMemoryList(cmd.OutOrStdout(), mems, jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Machine-readable JSON array")
	return cmd
}

func newMemoryShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "show <name>",
		Short:        "Print one memory's full body",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := openMemoryManagerForCLI()
			if err != nil {
				return err
			}
			mems, err := mgr.All()
			if err != nil {
				return err
			}
			for _, m := range mems {
				if m.Name == args[0] {
					return writeMemoryDetail(cmd.OutOrStdout(), m)
				}
			}
			return fmt.Errorf("no memory named %q", args[0])
		},
	}
	return cmd
}

func newMemoryForgetCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:          "forget <name>",
		Short:        "Delete a memory by name",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !force {
				// Refuse without --force to match the destructive-action
				// safety rule. The agent-callable memex_forget tool gates
				// on permission tier instead.
				return fmt.Errorf("`ycode memory forget %q` requires --force (this deletes the memory; there is no undo)", args[0])
			}
			mgr, err := openMemoryManagerForCLI()
			if err != nil {
				return err
			}
			if err := mgr.Forget(args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "forgot %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Confirm the deletion is intended")
	return cmd
}

func newMemoryExportCmd() *cobra.Command {
	var outPath string
	cmd := &cobra.Command{
		Use:          "export",
		Short:        "Write every memory as a JSON array",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mgr, err := openMemoryManagerForCLI()
			if err != nil {
				return err
			}
			mems, err := mgr.All()
			if err != nil {
				return err
			}
			sort.Slice(mems, func(i, j int) bool { return mems[i].Name < mems[j].Name })
			data, err := json.MarshalIndent(mems, "", "  ")
			if err != nil {
				return err
			}
			if outPath == "" || outPath == "-" {
				_, err = cmd.OutOrStdout().Write(append(data, '\n'))
				return err
			}
			return os.WriteFile(outPath, append(data, '\n'), 0o644)
		},
	}
	cmd.Flags().StringVarP(&outPath, "output", "o", "", "Write to file (default: stdout)")
	return cmd
}

// writeMemoryList formats `ycode memory list` output. Tabular mode is
// designed for skim-ability — name + type + scope + short description
// truncated to fit a terminal. JSON mode is identical to `--json`
// from other introspection commands and is the contract scripts rely
// on; do not reshape it without bumping a versioned schema.
func writeMemoryList(w io.Writer, mems []*memory.Memory, jsonOut bool) error {
	if jsonOut {
		data, err := json.MarshalIndent(mems, "", "  ")
		if err != nil {
			return err
		}
		_, err = w.Write(append(data, '\n'))
		return err
	}
	if len(mems) == 0 {
		fmt.Fprintln(w, "(no memories)")
		return nil
	}
	for _, m := range mems {
		desc := strings.TrimSpace(m.Description)
		if len(desc) > 80 {
			desc = desc[:77] + "..."
		}
		scope := string(m.Scope)
		if scope == "" {
			scope = "project"
		}
		fmt.Fprintf(w, "%-40s  %-10s  %-8s  %s\n", m.Name, m.Type, scope, desc)
	}
	return nil
}

// writeMemoryDetail formats `ycode memory show` output: header lines
// for the structured fields followed by the full body. Keep the
// "---\n" separator stable; some scripts pipe through awk on it.
func writeMemoryDetail(w io.Writer, m *memory.Memory) error {
	fmt.Fprintf(w, "name:        %s\n", m.Name)
	fmt.Fprintf(w, "type:        %s\n", m.Type)
	fmt.Fprintf(w, "scope:       %s\n", m.Scope)
	if m.Description != "" {
		fmt.Fprintf(w, "description: %s\n", m.Description)
	}
	if len(m.Tags) > 0 {
		fmt.Fprintf(w, "tags:        %s\n", strings.Join(m.Tags, ", "))
	}
	if m.Importance > 0 {
		fmt.Fprintf(w, "importance:  %.2f\n", m.Importance)
	}
	fmt.Fprintln(w, "---")
	fmt.Fprintln(w, m.Content)
	return nil
}
