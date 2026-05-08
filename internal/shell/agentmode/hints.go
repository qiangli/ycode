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
	"regexp"
	"strings"
	"sync"

	"github.com/qiangli/ycode/internal/shell"
)

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
		Pattern:  regexp.MustCompile(`\bwc\b\s+-\w*l\b[^|]*\.(go|py|ts|js|rs|java|c|rb)\b`),
		Category: "structure",
		Suggest:  "`yc symbols <path>` enumerates functions/types/methods directly without line counting",
	},
	{
		ID:       "curl-http-suggests-browser",
		Pattern:  regexp.MustCompile(`\bcurl\b[^|]*https?://`),
		Category: "net",
		Suggest:  "for JS-rendered pages: `yc browser fetch <url>`; for an interactive session: `yc browser open <url>`",
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
}

// Suggest runs the hint engine on a raw input command. Exposed so
// `--suggest <cmd>` can call it without needing exec context.
//
// `rt` is currently unused but kept in the signature so future hints
// can consult cwd, available skills, etc.
func Suggest(_ *shell.ShellRuntime, command string) []shell.Hint {
	var hints []shell.Hint
	seen := suggestSeen()
	defer suggestRelease(seen)

	for _, h := range Catalog {
		if h.Pattern.MatchString(command) {
			if _, dup := seen[h.ID]; dup {
				continue
			}
			seen[h.ID] = struct{}{}
			hints = append(hints, shell.Hint{ID: h.ID, Category: h.Category, Message: h.Suggest})
		}
	}
	return hints
}

// SuggestPost runs the post-execution hint matchers, e.g. exit==127 →
// "try yc help". Called by the dispatcher after every command in agent
// mode.
func SuggestPost(_ *shell.ShellRuntime, exitCode int, stderr string) []shell.Hint {
	var hints []shell.Hint
	seen := suggestSeen()
	defer suggestRelease(seen)

	for _, h := range PostCatalog {
		if h.Match(exitCode, stderr) {
			if _, dup := seen[h.ID]; dup {
				continue
			}
			seen[h.ID] = struct{}{}
			hints = append(hints, shell.Hint{ID: h.ID, Category: h.Category, Message: h.Suggest})
		}
	}
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
}
