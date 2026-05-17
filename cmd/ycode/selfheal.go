package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/runtime/selfheal/outcome"
	"github.com/qiangli/ycode/internal/runtime/selfheal/workspace"
)

// newSelfHealCmd assembles `ycode selfheal …` — operator front-end
// for the Phase 5 worker outcomes plus the STOP-file kill switch
// the Phase 3+4 daemon honors.
func newSelfHealCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "selfheal",
		Short: "Inspect and manage selfheal fix candidates",
		Long: `selfheal observes ycode tool-call failures, attempts a fix in a
per-signature workspace under ~/.agents/ycode/selfheal/, and either
opens a PR against your ycode fork (when GitHub creds are present) or
keeps the fix locally for you to export by hand. Sub-commands manage
the kill switch and inspect outcomes.`,
	}
	cmd.AddCommand(newSelfHealOnCmd())
	cmd.AddCommand(newSelfHealOffCmd())
	cmd.AddCommand(newSelfHealStatusCmd())
	cmd.AddCommand(newSelfHealListCmd())
	cmd.AddCommand(newSelfHealExportCmd())
	cmd.AddCommand(newSelfHealDiscardCmd())
	return cmd
}

// selfHealBaseDir resolves ~/.agents/ycode/selfheal, creating the dir
// if needed.
func selfHealBaseDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".agents", "ycode", "selfheal")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func newSelfHealOnCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "on",
		Short: "Re-enable the selfheal daemon (removes the STOP sentinel)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			base, err := selfHealBaseDir()
			if err != nil {
				return err
			}
			stop := filepath.Join(base, "STOP")
			if err := os.Remove(stop); err != nil && !os.IsNotExist(err) {
				return err
			}
			fmt.Fprintf(os.Stderr, "selfheal: STOP cleared at %s; daemon will resume on next poll\n", stop)
			return nil
		},
	}
}

func newSelfHealOffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "off",
		Short: "Stop dispatch of new selfheal workers (creates the STOP sentinel)",
		Long: "In-flight workers keep running until they finish — `off` only stops\n" +
			"dispatch of new ones. Use Ctrl-C on ycode serve to terminate the\n" +
			"process if you need to abort in-flight workers immediately.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			base, err := selfHealBaseDir()
			if err != nil {
				return err
			}
			stop := filepath.Join(base, "STOP")
			if err := os.WriteFile(stop, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "selfheal: STOP written to %s; daemon will skip dispatch until `selfheal on`\n", stop)
			return nil
		},
	}
}

func newSelfHealStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon STOP state and a count of pending / completed fixes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			base, err := selfHealBaseDir()
			if err != nil {
				return err
			}
			killSwitch := "off"
			if _, err := os.Stat(filepath.Join(base, "STOP")); err == nil {
				killSwitch = "ON (dispatch paused)"
			}
			entries, _ := listEntries(base)
			byMode := map[string]int{}
			for _, e := range entries {
				byMode[e.summary()]++
			}
			fmt.Fprintf(os.Stdout, "selfheal status\n")
			fmt.Fprintf(os.Stdout, "  base:        %s\n", base)
			fmt.Fprintf(os.Stdout, "  kill switch: %s\n", killSwitch)
			fmt.Fprintf(os.Stdout, "  fixes:       %d total\n", len(entries))
			keys := make([]string, 0, len(byMode))
			for k := range byMode {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(os.Stdout, "    %-16s %d\n", k+":", byMode[k])
			}
			return nil
		},
	}
}

func newSelfHealListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List per-signature fix candidates",
		RunE: func(cmd *cobra.Command, _ []string) error {
			base, err := selfHealBaseDir()
			if err != nil {
				return err
			}
			localOnly, _ := cmd.Flags().GetBool("local-only")
			entries, err := listEntries(base)
			if err != nil {
				return err
			}
			n := 0
			for _, e := range entries {
				if localOnly && e.PublishMode != string(outcome.ModeLocalOnly) {
					continue
				}
				printEntry(os.Stdout, e)
				n++
			}
			if n == 0 {
				fmt.Fprintln(os.Stdout, "(no fixes)")
			}
			return nil
		},
	}
	cmd.Flags().Bool("local-only", false, "Show only fixes that weren't pushed to a remote")
	return cmd
}

func newSelfHealExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export <signature>",
		Short: "Print the local-only fix's patch to stdout (or --out <file>)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			base, err := selfHealBaseDir()
			if err != nil {
				return err
			}
			out, _ := cmd.Flags().GetString("out")
			layout := workspace.PathsFor(base, args[0])
			res, err := readResult(layout.Outcome)
			if err != nil {
				return err
			}
			if res.PatchPath == "" {
				return fmt.Errorf("no patch path on signature %s (PublishMode=%s)", args[0], res.PublishMode)
			}
			data, err := os.ReadFile(res.PatchPath)
			if err != nil {
				return err
			}
			if out != "" {
				if err := os.WriteFile(out, data, 0o600); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "selfheal export: wrote %d bytes to %s\n", len(data), out)
				return nil
			}
			_, err = io.Copy(os.Stdout, strings.NewReader(string(data)))
			return err
		},
	}
	cmd.Flags().String("out", "", "Write patch to this path instead of stdout")
	return cmd
}

func newSelfHealDiscardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "discard <signature>",
		Short: "Remove a signature's workspace (clone + worktree + outcome)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			base, err := selfHealBaseDir()
			if err != nil {
				return err
			}
			layout := workspace.PathsFor(base, args[0])
			if _, err := os.Stat(layout.Root); err != nil {
				return fmt.Errorf("no workspace for signature %s", args[0])
			}
			if err := os.RemoveAll(layout.Root); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "selfheal discard: removed %s\n", layout.Root)
			return nil
		},
	}
}

// listEntry is the trimmed projection used by list/status. Loaded
// from each <sig>/outcome.json.
type listEntry struct {
	Signature   string `json:"signature"`
	Mode        string `json:"mode"`
	PublishMode string `json:"publish_mode"`
	PRURL       string `json:"pr_url"`
	PushedTo    string `json:"pushed_to"`
	PatchPath   string `json:"patch_path"`
	BranchName  string `json:"branch_name"`
	DiffLines   int    `json:"diff_lines"`
	Path        string `json:"-"`
}

func (e listEntry) summary() string {
	if e.PublishMode != "" {
		return string(e.PublishMode)
	}
	if e.Mode != "" {
		return e.Mode
	}
	return "unknown"
}

func listEntries(base string) ([]listEntry, error) {
	dirs, err := os.ReadDir(base)
	if err != nil {
		return nil, err
	}
	var out []listEntry
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		path := filepath.Join(base, d.Name(), "outcome.json")
		e, err := readResult(path)
		if err != nil {
			continue
		}
		e.Path = path
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Signature < out[j].Signature })
	return out, nil
}

func readResult(path string) (listEntry, error) {
	var e listEntry
	data, err := os.ReadFile(path)
	if err != nil {
		return e, err
	}
	if err := json.Unmarshal(data, &e); err != nil {
		return e, err
	}
	return e, nil
}

func printEntry(w io.Writer, e listEntry) {
	tag := "local "
	link := e.PatchPath
	if e.PublishMode == string(outcome.ModePR) {
		tag = "PR    "
		link = e.PRURL
	}
	fmt.Fprintf(w, "%s  %s  branch=%-50s  diff=%d  %s\n",
		tag, e.Signature, truncMax(e.BranchName, 50), e.DiffLines, link)
}

func truncMax(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// localOnlyCount is exposed for the `ycode serve` startup notice.
func localOnlyCount(ctx context.Context) (int, string) {
	base, err := selfHealBaseDir()
	if err != nil {
		return 0, ""
	}
	entries, err := listEntries(base)
	if err != nil {
		return 0, ""
	}
	n := 0
	for _, e := range entries {
		if e.PublishMode == string(outcome.ModeLocalOnly) {
			n++
		}
	}
	if n == 0 {
		return 0, ""
	}
	return n, fmt.Sprintf("selfheal: %d local fix%s pending (no GitHub creds detected); run `ycode selfheal list`",
		n, plural(n))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "es"
}
