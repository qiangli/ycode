package ycodecli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/qiangli/ycode/internal/docs"
	harnesscli "github.com/qiangli/ycode/internal/harness/cli"
)

// `ycode docs` — the agent-facing capability index. See
// internal/docs/embed.go for the curation contract.
//
// SAFEGUARDS (mirror of internal/docs/embed.go safeguard #8):
//
//   - This is the ONLY execution entry point for the agent-facing docs.
//     The AGENTS.md one-liner is a separate registration that
//     delegates to internal/docs functions — never duplicate the
//     content here.
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
func runDocsInvocation(inv harnesscli.Invocation, streams harnesscli.IO) error {
	switch inv.Dispatch.Action {
	case "":
		listFlag, _ := inv.Flags["list"].(bool)
		allFlag, _ := inv.Flags["all"].(bool)
		searchFlag, _ := inv.Flags["search"].(string)
		switch {
		case listFlag:
			return runDocsList(streams.Out)
		case allFlag:
			return runDocsAll(streams.Out)
		case searchFlag != "":
			return runDocsSearch(streams.Out, searchFlag)
		case len(inv.Arguments) == 0:
			return runDocsIndex(streams.Out)
		default:
			return runDocsTopic(streams.Out, streams.Err, inv.Arguments[0])
		}
	case "catalog":
		cat, err := docs.LoadCatalog()
		if err != nil {
			return err
		}
		taskFlag, _ := inv.Flags["task"].(string)
		jsonFlag, _ := inv.Flags["json"].(bool)
		if taskFlag != "" {
			cat = cat.FilterByTask(taskFlag)
		}
		if jsonFlag {
			return cat.RenderJSON(streams.Out)
		}
		return cat.RenderText(streams.Out)
	default:
		return fmt.Errorf("unsupported docs action %q", inv.Dispatch.Action)
	}
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
