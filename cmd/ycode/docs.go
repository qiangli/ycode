package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/docs"
)

// `ycode docs` — the agent-facing capability index. See
// internal/docs/embed.go for the curation contract.
//
// SAFEGUARDS (mirror of internal/docs/embed.go safeguard #8):
//
//   - This is the ONLY cobra entry point for the agent-facing docs.
//     The MCP-tool surface (`mcp__ycode__docs`) and the AGENTS.md
//     one-liner are separate registrations that all delegate to
//     internal/docs functions — never duplicate the content here.
//   - Output is plain markdown to stdout. No spinners, no colors, no
//     pagers, no auto-confirmation prompts. Agents pipe and parse this;
//     anything extra breaks scripted callers.
//   - Exit code is ALWAYS 0 for documented invocations (including
//     "unknown topic" — we print a list of valid topics to stderr but
//     still exit 0 so agent loops that wrap `$(ycode docs $x)` don't
//     crash on a typo). The only non-zero exit is a genuine internal
//     error parsing the embedded files, which the CI gate prevents.
//   - Do not add side effects (no telemetry, no analytics, no cache
//     writes). `ycode docs` MUST be safe to run from any sandbox at any
//     time including before `ycode init` has ever been run.
func newDocsCmd() *cobra.Command {
	var (
		listFlag   bool
		allFlag    bool
		searchFlag string
	)

	cmd := &cobra.Command{
		Use:   "docs [topic]",
		Short: "Agent-facing capability prompts (embedded; works offline)",
		Long: `Print curated agent-facing prompts describing ycode capabilities.

With no arg, prints the topic index. With a <topic> arg, prints the
prompt for that topic. Output is markdown on stdout, intended to be
read by an LLM agent or piped into a system prompt.

For the operator-facing CLI surface (subcommands, flags, usage), use
'ycode help' or 'ycode <cmd> --help'. docs and help are complementary:
docs is curated prose for agent decision-making; help is auto-generated
structural metadata for humans.`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch {
			case listFlag:
				return runDocsList(cmd.OutOrStdout())
			case allFlag:
				return runDocsAll(cmd.OutOrStdout())
			case searchFlag != "":
				return runDocsSearch(cmd.OutOrStdout(), searchFlag)
			case len(args) == 0:
				return runDocsIndex(cmd.OutOrStdout())
			default:
				return runDocsTopic(cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0])
			}
		},
	}
	cmd.Flags().BoolVar(&listFlag, "list", false, "Print JSON list of topics (machine-readable)")
	cmd.Flags().BoolVar(&allFlag, "all", false, "Concatenate every topic (for system-prompt stuffing; use sparingly)")
	cmd.Flags().StringVar(&searchFlag, "search", "", "Substring match on topic, summary, and when fields")
	return cmd
}

func runDocsIndex(w io.Writer) error {
	body, err := docs.IndexBody()
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(w, body)
	return err
}

func runDocsTopic(stdout, stderr io.Writer, slug string) error {
	doc, err := docs.Get(slug)
	if err != nil {
		// Unknown-topic path: exit 0 (see safeguard), print available
		// topics to stderr so a wrapping shell sees the suggestion but
		// stdout stays empty for piping.
		topics, _ := docs.Topics()
		fmt.Fprintf(stderr, "ycode docs: unknown topic %q\navailable topics: %s\n",
			slug, strings.Join(topics, ", "))
		return nil
	}
	_, err = fmt.Fprint(stdout, doc.Raw)
	return err
}

func runDocsAll(w io.Writer) error {
	topics, err := docs.Topics()
	if err != nil {
		return err
	}
	// Lead with the index so the consumer reads the orientation block
	// before the dump. The separator between sections is a thematic
	// break + the topic slug as an H1 — easy for both humans and LLMs
	// to navigate.
	idx, err := docs.IndexBody()
	if err != nil {
		return err
	}
	if _, err := fmt.Fprint(w, idx); err != nil {
		return err
	}
	for _, slug := range topics {
		doc, err := docs.Get(slug)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "\n\n---\n\n# %s\n\n%s", slug, doc.Body); err != nil {
			return err
		}
	}
	return nil
}

func runDocsList(w io.Writer) error {
	topics, err := docs.Topics()
	if err != nil {
		return err
	}
	type row struct {
		Topic    string `json:"topic"`
		Summary  string `json:"summary"`
		When     string `json:"when"`
		MaxLines int    `json:"max_lines"`
	}
	out := make([]row, 0, len(topics))
	for _, slug := range topics {
		d, err := docs.Get(slug)
		if err != nil {
			return err
		}
		out = append(out, row{Topic: d.Topic, Summary: d.Summary, When: d.When, MaxLines: d.MaxLines})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func runDocsSearch(w io.Writer, query string) error {
	topics, err := docs.Topics()
	if err != nil {
		return err
	}
	q := strings.ToLower(query)
	type hit struct {
		Slug    string
		Summary string
		When    string
	}
	var hits []hit
	for _, slug := range topics {
		d, err := docs.Get(slug)
		if err != nil {
			return err
		}
		hay := strings.ToLower(d.Topic + " " + d.Summary + " " + d.When)
		if strings.Contains(hay, q) {
			hits = append(hits, hit{d.Topic, d.Summary, d.When})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Slug < hits[j].Slug })
	if len(hits) == 0 {
		fmt.Fprintf(w, "no topics match %q\n", query)
		return nil
	}
	for _, h := range hits {
		fmt.Fprintf(w, "- **%s** — %s\n  when: %s\n  drill: `ycode docs %s`\n",
			h.Slug, h.Summary, h.When, h.Slug)
	}
	return nil
}
