// Package agentmode implements the --agent posture for ycode shell:
// output augmentation that teaches foreign agents (Claude Code, OpenCode,
// Codex, …) about ycode's killer capabilities by appending hints to
// stderr — never stdout — when bash commands match patterns where a
// `yc <verb>` would have served better.
//
// The engine is regex/data-driven, not LLM-mediated. Cheap, deterministic,
// no provider dependency. A future --agent=smart mode can layer LLM
// suggestions on top.
package agentmode

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/qiangli/ycode/internal/shell"
)

// itoa avoids dragging fmt into the hot-path attribute formatter.
func itoa(n int) string { return strconv.Itoa(n) }

// Hint is the in-package shape; the public Hint type lives in
// internal/shell to keep cmd/ycode importable without pulling agentmode.
type Hint struct {
	ID       string
	Pattern  *regexp.Regexp
	Suggest  string
	Category string
}

// Catalog is the pattern set evaluated against every command in agent
// mode. Order matters: higher-value hints first; first match wins for
// dedup-within-process. Adding a hint is a one-line entry.
var Catalog = []Hint{
	{
		ID:       "grep-r-suggests-search-symbols",
		Pattern:  regexp.MustCompile(`\bgrep\b[^|]*\s+-\w*r`),
		Category: "code-search",
		Suggest:  "try `yc search-symbols '<pattern>' [path]` (AST-aware, language-agnostic)",
	},
	{
		// Single-file or explicit-list grep against a source file (no -r).
		// Common case: `grep -nE '^func' foo.go` to enumerate declarations —
		// exactly what `yc symbols` does without a regex. The body skips over
		// '...' and "..." spans so a `|` inside a quoted regex (e.g.
		// `'(func|type)'`) doesn't end the match prematurely. $1 captures
		// the last source-file argument so the suggestion is runnable verbatim.
		ID:       "grep-source-file-suggests-symbols",
		Pattern:  regexp.MustCompile(`\bgrep\b(?:[^|'"]|'[^']*'|"[^"]*")*\s(\S+\.(?:go|py|ts|js|rs|java|c|rb))\b`),
		Category: "code-search",
		Suggest:  "for declarations in `$1`: `yc symbols $1`; for substring search: `yc search-symbols '<pattern>' $1`",
	},
	{
		ID:       "rg-or-ack-suggests-search-symbols",
		Pattern:  regexp.MustCompile(`\b(rg|ack|ag)\b`),
		Category: "code-search",
		Suggest:  "try `yc search-symbols '<pattern>'` for AST-aware results, or `yc symbols <path>` to enumerate",
	},
	{
		ID:       "find-source-suggests-symbols",
		Pattern:  regexp.MustCompile(`\bfind\b[^|]*-name\b[^|]*\.(go|py|ts|js|rs|java|c|rb)\b`),
		Category: "file-walk",
		Suggest:  "try `yc symbols <path>` to enumerate symbols, or `yc repomap` for a token-budgeted overview",
	},
	{
		ID:       "tree-suggests-repomap-or-graph",
		Pattern:  regexp.MustCompile(`\btree\b(\s+-\w+)*`),
		Category: "structure",
		Suggest:  "try `yc repomap` (token-budgeted file→symbol overview) or `yc graph \"<DQL>\"` (code knowledge graph) — both beat raw `tree` for understanding code",
	},
	{
		ID:       "ls-recursive-suggests-repomap",
		Pattern:  regexp.MustCompile(`\bls\s+(-\w*R\w*|-\w+\s+-\w*R)`),
		Category: "structure",
		Suggest:  "try `yc repomap` for a token-budgeted file→symbol overview",
	},
	{
		ID:       "wc-source-suggests-symbols",
		Pattern:  regexp.MustCompile(`\bwc\b\s+-\w*l\b[^|]*?\s(\S+\.(?:go|py|ts|js|rs|java|c|rb))\b`),
		Category: "structure",
		Suggest:  "`yc symbols $1` enumerates functions/types/methods directly without line counting",
	},
	{
		ID:       "curl-http-suggests-browser",
		Pattern:  regexp.MustCompile(`\bcurl\b[^|]*?(https?://\S+)`),
		Category: "net",
		Suggest:  "for JS-rendered pages: `yc browser fetch $1`; for an interactive session: `yc browser open $1`",
	},
	{
		ID:       "git-log-status-diff-suggests-yc-git",
		Pattern:  regexp.MustCompile(`\bgit\s+(log|status|diff|branch|show|blame)\b`),
		Category: "git",
		Suggest:  "`yc git $1` uses native go-git (no fork), faster on large repos",
	},
	{
		ID:       "rm-rf-advisory",
		Pattern:  regexp.MustCompile(`\brm\b\s+(-rf|-fr|-r\s+-f|-f\s+-r)\b`),
		Category: "safety",
		Suggest:  "advisory: this is destructive; rerun with `--sandbox` for podman copy-on-write",
	},
	{
		ID:       "cat-pipe-head-suggests-repomap",
		Pattern:  regexp.MustCompile(`\bcat\b[^|]*\|\s*head\b`),
		Category: "structure",
		Suggest:  "for repo context, `yc repomap --budget=N` gives a token-budgeted symbol map",
	},
	{
		ID:       "ctags-suggests-symbols",
		Pattern:  regexp.MustCompile(`\b(ctags|etags)\b`),
		Category: "code-search",
		Suggest:  "`yc symbols <path>` extracts symbols natively (treesitter, no index file)",
	},
	{
		ID:       "wget-suggests-browser",
		Pattern:  regexp.MustCompile(`\bwget\b[^|]*?(https?://\S+)`),
		Category: "net",
		Suggest:  "for JS-rendered pages: `yc browser fetch $1` (also handles redirects + Content-Type)",
	},
	{
		ID:       "find-large-suggests-repomap",
		Pattern:  regexp.MustCompile(`\bfind\b\s+\.\s+(-type\s+f\b|-name\b)`),
		Category: "file-walk",
		Suggest:  "for a token-budgeted file overview: `yc repomap`",
	},
	{
		ID:       "echo-content-pipe-grep",
		Pattern:  regexp.MustCompile(`\becho\b[^|]*\|\s*grep\b`),
		Category: "code-search",
		Suggest:  "for richer matching consider `yc search-symbols` (over actual code) or in-process bash regex",
	},
}

// PostHints fire AFTER execution, based on the result.
type PostHint struct {
	ID       string
	Suggest  string
	Category string
	Match    func(exitCode int, stderr string) bool
}

var PostCatalog = []PostHint{
	{
		ID:       "exit-127-suggests-yc-help",
		Category: "discovery",
		Suggest:  "command not found — try `yc help` to list ycode-native built-ins",
		Match: func(exitCode int, _ string) bool {
			return exitCode == 127
		},
	},
	{
		ID:       "permission-denied-suggests-sandbox",
		Category: "safety",
		Suggest:  "permission denied — `--sandbox` grants podman-isolated execution with controlled mounts",
		Match: func(exitCode int, stderr string) bool {
			return exitCode != 0 && strings.Contains(strings.ToLower(stderr), "permission denied")
		},
	},
	{
		ID:       "no-such-file-suggests-symbols",
		Category: "discovery",
		Suggest:  "no such file — try `yc symbols <path>` to enumerate, or `yc repomap` for an overview",
		Match: func(exitCode int, stderr string) bool {
			low := strings.ToLower(stderr)
			return exitCode != 0 && (strings.Contains(low, "no such file") || strings.Contains(low, "not a directory"))
		},
	},
	{
		ID:       "git-not-a-repo-suggests-yc-git",
		Category: "git",
		Suggest:  "not a git repository — `yc git init` initializes one (native go-git)",
		Match: func(exitCode int, stderr string) bool {
			return exitCode != 0 && strings.Contains(strings.ToLower(stderr), "not a git repository")
		},
	},
}

// Suggest runs the hint engine on a raw input command. Exposed so
// `--suggest <cmd>` can call it without needing exec context.
//
// `rt` is currently unused but kept in the signature so future hints
// can consult cwd, available skills, etc.
//
// The Suggest field of each rule is run through Pattern.ExpandString
// against the matched command, so `$1`, `${name}`, etc. interpolate
// from capture groups — agents see runnable invocations instead of
// generic templates. Rules without capture groups behave as before.
// To emit a literal `$` in a suggestion, escape it as `$$`.
//
// Every call records a JSONL row to the mining sink (path resolves to
// $YCODE_SHELL_HISTORY_FILE or ~/.agents/ycode/shell-history.jsonl;
// disable with YCODE_SHELL_MINE_DISABLE=1) so the catalog-improvement
// loop can later see which commands missed.
func Suggest(_ *shell.ShellRuntime, command string) []shell.Hint {
	_, end := shell.StartSpan(context.Background(), "ycode.shell.suggest")
	var hints []shell.Hint
	seen := suggestSeen()
	defer suggestRelease(seen)

	var firedIDs []string
	for _, h := range Catalog {
		loc := h.Pattern.FindStringSubmatchIndex(command)
		if loc == nil {
			continue
		}
		if _, dup := seen[h.ID]; dup {
			continue
		}
		seen[h.ID] = struct{}{}
		msg := string(h.Pattern.ExpandString(nil, h.Suggest, command, loc))
		hints = append(hints, shell.Hint{ID: h.ID, Category: h.Category, Message: msg})
		firedIDs = append(firedIDs, h.ID)
		shell.ObserveHint(h.ID, h.Category, "pre")
	}
	RecordPre(command, firedIDs)
	end(nil, "fired_count", itoa(len(firedIDs)))
	return hints
}

// SuggestPost runs the post-execution hint matchers, e.g. exit==127 →
// "try yc help". Called by the dispatcher after every command in agent
// mode.
func SuggestPost(_ *shell.ShellRuntime, exitCode int, stderr string) []shell.Hint {
	_, end := shell.StartSpan(context.Background(), "ycode.shell.suggest_post")
	var hints []shell.Hint
	seen := suggestSeen()
	defer suggestRelease(seen)

	var firedIDs []string
	for _, h := range PostCatalog {
		if h.Match(exitCode, stderr) {
			if _, dup := seen[h.ID]; dup {
				continue
			}
			seen[h.ID] = struct{}{}
			hints = append(hints, shell.Hint{ID: h.ID, Category: h.Category, Message: h.Suggest})
			firedIDs = append(firedIDs, h.ID)
			shell.ObserveHint(h.ID, h.Category, "post")
		}
	}
	RecordPost(exitCode, firedIDs)
	end(nil, "fired_count", itoa(len(firedIDs)), "exit_code", itoa(exitCode))
	return hints
}

// process-wide dedup of hint IDs. -c invocations get a fresh map per
// process; long-running interactive sessions accumulate within the
// process. Cross-process dedup is Phase C work.
var (
	seenMu sync.Mutex
	seen   = map[string]struct{}{}
)

func suggestSeen() map[string]struct{} {
	seenMu.Lock()
	return seen
}

func suggestRelease(_ map[string]struct{}) {
	seenMu.Unlock()
}

// ResetSeen clears the dedup table. Used by tests.
func ResetSeen() {
	seenMu.Lock()
	seen = map[string]struct{}{}
	seenMu.Unlock()
}

// ManifestEntries returns the hint catalog as manifest rows for
// `ycode shell --manifest`.
func ManifestEntries() []shell.ManifestHint {
	out := make([]shell.ManifestHint, 0, len(Catalog)+len(PostCatalog))
	for _, h := range Catalog {
		out = append(out, shell.ManifestHint{
			ID: h.ID, Pattern: h.Pattern.String(), Suggest: h.Suggest, Category: h.Category,
		})
	}
	for _, h := range PostCatalog {
		out = append(out, shell.ManifestHint{
			ID: h.ID, Pattern: "(post-exec)", Suggest: h.Suggest, Category: h.Category,
		})
	}
	return out
}

func init() {
	shell.SetSuggestFunc(Suggest)
	shell.SetHintCatalogForManifest(ManifestEntries())
	shell.SetPostHintsFunc(SuggestPost)
}
