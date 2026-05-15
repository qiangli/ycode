package builtins

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/qiangli/ycode/pkg/memex/qacache"
)

func init() {
	Register(&qacacheVerb{})
}

// qacacheVerb implements `yc qacache <subcommand>`. The Q→A cache is a
// project-local store of prior question/answer pairs that ycode injects
// pre-LLM to short-circuit repeated work.
type qacacheVerb struct{}

func (qacacheVerb) Name() string { return "qacache" }
func (qacacheVerb) Description() string {
	return "Inspect the project Q→A cache (subcommands: stats, list, clear)"
}
func (qacacheVerb) Usage() string { return "yc qacache <stats|list|clear> [--json]" }

func (qacacheVerb) Run(_ context.Context, args []string, stdio Stdio, cwd string) (int, error) {
	sub := "stats"
	asJSON := false
	for _, a := range args {
		if a == "--json" {
			asJSON = true
			continue
		}
		if !strings.HasPrefix(a, "-") {
			sub = strings.ToLower(a)
		}
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	dir := filepath.Join(cwd, ".agents", "ycode", "qacache")
	cache, err := qacache.New(dir)
	if err != nil {
		fmt.Fprintf(stdio.Stderr, "yc qacache: %v\n", err)
		return 1, nil
	}

	switch sub {
	case "stats":
		stats := cache.Stats()
		if asJSON {
			enc := json.NewEncoder(stdio.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(stats)
			return 0, nil
		}
		fmt.Fprintf(stdio.Stdout, "entries=%d hits=%d misses=%d writes=%d invalidated=%d promoted=%d  dir=%s\n",
			stats.Entries, stats.Hits, stats.Misses, stats.Writes, stats.Invalidated, stats.Promoted, dir)
		return 0, nil
	case "list":
		// PromotionCandidates is the most useful surface to list — it's
		// also a way to peek at "what's about to become a memory."
		cands := cache.PromotionCandidates(time.Now())
		if asJSON {
			enc := json.NewEncoder(stdio.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(cands)
			return 0, nil
		}
		if len(cands) == 0 {
			fmt.Fprintln(stdio.Stdout, "(no promotion candidates)")
			return 0, nil
		}
		for _, e := range cands {
			fmt.Fprintf(stdio.Stdout, "asks=%d class=%s key=%s\n  Q: %s\n  A: %s\n",
				e.AskCount, e.Class, e.Key, firstLine(e.Question, 120), firstLine(e.Answer, 200))
		}
		return 0, nil
	case "clear":
		// Drop all entries from disk by removing the dir and recreating.
		// This is destructive — kept off by default; called only via explicit subcommand.
		if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintf(stdio.Stderr, "yc qacache clear: %v\n", err)
			return 1, nil
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			fmt.Fprintf(stdio.Stderr, "yc qacache clear: %v\n", err)
			return 1, nil
		}
		fmt.Fprintln(stdio.Stdout, "qacache cleared")
		return 0, nil
	default:
		fmt.Fprintf(stdio.Stderr, "yc qacache: unknown subcommand %q (allowed: stats, list, clear)\n", sub)
		return 2, nil
	}
}
