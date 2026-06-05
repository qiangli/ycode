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
	"os"
	"path/filepath"
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
//
// Why is a one-line rationale rendered alongside Suggest so the agent
// reading the hint has a reason to switch rather than fall back on
// muscle memory. Terse-sentence style: "AST-aware; skips comments and
// strings; resolves Go aliasing."
type Hint struct {
	ID       string
	Pattern  *regexp.Regexp
	Suggest  string
	Category string
	Why      string

	// SkipOnSuccess marks the hint as a "you might prefer yc <verb>"
	// nudge rather than a correctness warning. The caller suppresses
	// the print when the command exited 0 — idiomatic invocations
	// shouldn't carry repeated context-bloat hints across a session.
	SkipOnSuccess bool
}

// Catalog is the pattern set evaluated against every command in agent
// mode. Order matters: higher-value hints first; first match wins for
// dedup-within-process. Adding a hint is a one-line entry.
var Catalog = []Hint{
	{
		ID:       "grep-r-suggests-search-symbols",
		Pattern:  regexp.MustCompile(`\bgrep\b[^|]*\s+(-\w*r\w*)`),
		Category: "code-search",
		Suggest:  "noticing `$1` on grep — try `yc search-symbols '<pattern>' [path]` (AST-aware, language-agnostic)",
		Why:      "AST-aware: skips comments and string literals; resolves Go/TS aliases; no false positives in vendored copies.",
		// Idiomatic recursive grep that returned hits is still a working
		// answer — only re-pitch yc when it didn't (or hasn't yet).
		SkipOnSuccess: true,
	},
	{
		// Single-file or explicit-list grep against a source file (no -r).
		// Common case: `grep -nE '^func' foo.go` to enumerate declarations —
		// exactly what `yc symbols` does without a regex. The body skips over
		// '...' and "..." spans so a `|` inside a quoted regex (e.g.
		// `'(func|type)'`) doesn't end the match prematurely. $1 captures
		// the last source-file argument so the suggestion is runnable verbatim.
		ID:            "grep-source-file-suggests-symbols",
		Pattern:       regexp.MustCompile(`\bgrep\b(?:[^|'"]|'[^']*'|"[^"]*")*\s(\S+\.(?:go|py|ts|js|rs|java|c|rb))\b`),
		Category:      "code-search",
		Suggest:       "for declarations in `$1`: `yc symbols $1`; for substring search: `yc search-symbols '<pattern>' $1`",
		Why:           "`yc symbols` returns top-level decls in one call — no regex to guess, no comments matched.",
		SkipOnSuccess: true,
	},
	{
		ID:            "rg-or-ack-suggests-search-symbols",
		Pattern:       regexp.MustCompile(`\b(rg|ack|ag)\b`),
		Category:      "code-search",
		Suggest:       "try `yc search-symbols '<pattern>'` for AST-aware results, or `yc symbols <path>` to enumerate",
		Why:           "Treesitter-backed: scopes to declared identifiers; ignores string/comment hits that grep-family tools surface.",
		SkipOnSuccess: true,
	},
	{
		ID:            "find-source-suggests-symbols",
		Pattern:       regexp.MustCompile(`\bfind\b[^|]*-name\b[^|]*\.(go|py|ts|js|rs|java|c|rb)\b`),
		Category:      "file-walk",
		Suggest:       "try `yc symbols <path>` to enumerate symbols, or `yc repomap` for a token-budgeted overview",
		Why:           "`yc repomap` returns files ranked by symbol density + top-level decls — one call replaces find + head loops.",
		SkipOnSuccess: true,
	},
	{
		ID:            "tree-suggests-repomap-or-graph",
		Pattern:       regexp.MustCompile(`\btree\b(\s+-\w+)*`),
		Category:      "structure",
		Suggest:       "try `yc repomap` (token-budgeted file→symbol overview) or `yc graph \"<DQL>\"` (code knowledge graph) — both beat raw `tree` for understanding code",
		Why:           "Directory shape rarely answers \"what's the structure here\" — symbol density does.",
		SkipOnSuccess: true,
	},
	{
		ID:            "ls-recursive-suggests-repomap",
		Pattern:       regexp.MustCompile(`\bls\s+(-\w*R\w*|-\w+\s+-\w*R)`),
		Category:      "structure",
		Suggest:       "try `yc repomap` for a token-budgeted file→symbol overview",
		Why:           "`yc repomap` returns the same hierarchy plus top-level symbols, capped to a token budget.",
		SkipOnSuccess: true,
	},
	{
		ID:            "wc-source-suggests-symbols",
		Pattern:       regexp.MustCompile(`\bwc\b\s+-\w*l\b[^|]*?\s(\S+\.(?:go|py|ts|js|rs|java|c|rb))\b`),
		Category:      "structure",
		Suggest:       "`yc symbols $1` enumerates functions/types/methods directly without line counting",
		Why:           "Line count is rarely the question; symbol count + names is what callers actually want.",
		SkipOnSuccess: true,
	},
	{
		ID:            "curl-http-suggests-browser",
		Pattern:       regexp.MustCompile(`\bcurl\b[^|]*?(https?://\S+)`),
		Category:      "net",
		Suggest:       "for JS-rendered pages: `yc browser fetch $1`; for an interactive session: `yc browser open $1`",
		Why:           "`yc browser fetch` handles redirects, content-type sniffing, and JS execution; curl returns the unrendered source.",
		SkipOnSuccess: true,
	},
	{
		ID:            "git-log-status-diff-suggests-yc-git",
		Pattern:       regexp.MustCompile(`\bgit\s+(log|status|diff|branch|show|blame)\b`),
		Category:      "git",
		Suggest:       "`yc git $1` uses native go-git (no fork), faster on large repos",
		Why:           "go-git avoids the fork+exec cost; on large repos that's 50-200ms saved per call.",
		SkipOnSuccess: true,
	},
	{
		// `cd X && a && b & c` parses as `(cd X && a && b) & (c)` because
		// `&` binds looser than `&&`. The cd happens in the backgrounded
		// subshell; the commands after `&` run in the parent shell's cwd
		// — usually NOT the cd'd directory — and write to surprising
		// places ("bin/.pid: No such file or directory"). Real cases that
		// hit this: `cd /repo && build && nohup ./srv >log & echo $! > pid
		// && tail log` (the echo + tail run in the parent cwd, not /repo).
		//
		// Match shape: `cd <path> &&` … then a bare `&` that's whitespace-
		// flanked, followed by another command word. The whitespace
		// flanking is what distinguishes the backgrounding `&` from
		// shell redirects (`2>&1`, `>&3`), `&&`, and the closing `&)`
		// pattern where parens already scope the cd.
		ID:       "cd-with-background-warns-precedence",
		Pattern:  regexp.MustCompile(`\bcd\s+\S+\s+&&.*?\s&\s+\S`),
		Category: "shell-syntax",
		Suggest:  "bash operator precedence: `&` is looser than `&&`, so commands AFTER the `&` run in the parent shell's cwd, not the cd'd directory. Wrap the backgrounded segment in parentheses: `(cd … && cmd &) && next`, or split into two commands. Common failure mode: `nohup foo & echo $! > bin/.pid` writes the pid file in the wrong directory.",
		Why:      "Silent failures: the second half runs successfully but against the wrong filesystem path; debugging is painful because each piece looks fine in isolation.",
	},
	{
		ID:       "rm-rf-advisory",
		Pattern:  regexp.MustCompile(`\brm\b\s+(-rf|-fr|-r\s+-f|-f\s+-r)\b`),
		Category: "safety",
		Suggest:  "advisory: this is destructive; rerun with `--sandbox` for podman copy-on-write",
		Why:      "Sandbox runs against a copy-on-write overlay; mistakes are confined to the container's mount.",
	},
	{
		ID:            "cat-pipe-head-suggests-repomap",
		Pattern:       regexp.MustCompile(`\bcat\b[^|]*\|\s*head\b`),
		Category:      "structure",
		Suggest:       "for repo context, `yc repomap --budget=N` gives a token-budgeted symbol map",
		Why:           "`cat | head` peeks at one file; repomap gives the whole repo within a budget you control.",
		SkipOnSuccess: true,
	},
	{
		ID:            "ctags-suggests-symbols",
		Pattern:       regexp.MustCompile(`\b(ctags|etags)\b`),
		Category:      "code-search",
		Suggest:       "`yc symbols <path>` extracts symbols natively (treesitter, no index file)",
		Why:           "No index file to maintain or stale; treesitter parses on demand.",
		SkipOnSuccess: true,
	},
	{
		ID:            "wget-suggests-browser",
		Pattern:       regexp.MustCompile(`\bwget\b[^|]*?(https?://\S+)`),
		Category:      "net",
		Suggest:       "for JS-rendered pages: `yc browser fetch $1` (also handles redirects + Content-Type)",
		Why:           "wget returns the raw response; `yc browser fetch` resolves redirects and reports the final URL.",
		SkipOnSuccess: true,
	},
	{
		ID:            "find-large-suggests-repomap",
		Pattern:       regexp.MustCompile(`\bfind\b\s+\.\s+(-type\s+f\b|-name\b)`),
		Category:      "file-walk",
		Suggest:       "for a token-budgeted file overview: `yc repomap`",
		Why:           "`find` enumerates paths; repomap ranks them by symbol density so the most informative files surface first.",
		SkipOnSuccess: true,
	},
	{
		ID:            "echo-content-pipe-grep",
		Pattern:       regexp.MustCompile(`\becho\b[^|]*\|\s*grep\b`),
		Category:      "code-search",
		Suggest:       "for richer matching consider `yc search-symbols` (over actual code) or in-process bash regex",
		Why:           "Echo+grep tests a literal string; bash `[[ \"$x\" =~ regex ]]` does the same without forking.",
		SkipOnSuccess: true,
	},
}

// PostHints fire AFTER execution, based on the result.
//
// SuggestFunc, when non-nil, takes precedence over the static Suggest
// field and is called only after Match returns true. Use it for hints
// whose copy depends on a substring extracted from stderr (e.g. the
// specific `/bin/<tool>` path that ENOENT'd).
type PostHint struct {
	ID          string
	Suggest     string
	Category    string
	Why         string
	Match       func(exitCode int, stderr string) bool
	SuggestFunc func(stderr string) string
}

// binPathTools maps Linux-canonical /bin/<tool> paths to their ycode-
// native counterparts. macOS only ships /usr/bin/<tool> for these, so
// scripts hard-coding /bin/X exit 127 with a confusing ENOENT; the
// post-hint below names both the path fix AND the relevant `yc <verb>`
// follow-up so the agent gets a teachable moment out of the failure.
var binPathTools = map[string]string{
	"grep": "yc search-symbols (AST-aware) / yc symbols <path> (declarations)",
	"sed":  "yc git (native go-git for repo edits) / in-process bash regex",
	"awk":  "in-process bash arithmetic + parameter expansion",
}

var binPathRE = regexp.MustCompile(`/bin/(grep|sed|awk)\b`)

var PostCatalog = []PostHint{
	{
		ID:          "bin-path-suggests-usr-bin-and-yc",
		Category:    "platform",
		Why:         "macOS ships these only under /usr/bin/; scripts hard-coding /bin/X 404 here. Same failure is also ycode's cue to suggest a richer native verb.",
		Match:       matchBinPath,
		SuggestFunc: suggestBinPath,
	},
	{
		ID:       "exit-127-suggests-yc-help",
		Category: "discovery",
		Suggest:  "command not found — try `yc help` to list ycode-native built-ins",
		Why:      "Exit 127 usually means PATH miss; `yc <verb>` is in-process and unshadowable.",
		Match: func(exitCode int, _ string) bool {
			return exitCode == 127
		},
	},
	{
		ID:       "permission-denied-suggests-sandbox",
		Category: "safety",
		Suggest:  "permission denied — `--sandbox` grants podman-isolated execution with controlled mounts",
		Why:      "Sandboxed runs get a clean filesystem view with cwd mounted; sidesteps host-permission issues.",
		Match: func(exitCode int, stderr string) bool {
			return exitCode != 0 && strings.Contains(strings.ToLower(stderr), "permission denied")
		},
	},
	{
		ID:       "no-such-file-suggests-symbols",
		Category: "discovery",
		Suggest:  "no such file — try `yc symbols <path>` to enumerate, or `yc repomap` for an overview",
		Why:      "Path was guessed wrong; repomap/symbols show what actually exists.",
		Match: func(exitCode int, stderr string) bool {
			low := strings.ToLower(stderr)
			return exitCode != 0 && (strings.Contains(low, "no such file") || strings.Contains(low, "not a directory"))
		},
	},
	{
		ID:       "git-not-a-repo-suggests-yc-git",
		Category: "git",
		Suggest:  "not a git repository — `yc git init` initializes one (native go-git)",
		Why:      "`yc git init` uses go-git; no system git required, no fork overhead.",
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

	if isRemoteExec(command) {
		RecordPre(command, nil)
		end(nil, "fired_count", "0", "skip", "remote_exec")
		return nil
	}

	var firedIDs []string
	for _, h := range Catalog {
		loc := h.Pattern.FindStringSubmatchIndex(command)
		if loc == nil {
			continue
		}
		if _, dup := seen[h.ID]; dup {
			continue
		}
		markSeen(h.ID)
		msg := string(h.Pattern.ExpandString(nil, h.Suggest, command, loc))
		hints = append(hints, shell.Hint{
			ID: h.ID, Category: h.Category, Message: msg, Why: h.Why,
			SkipOnSuccess: h.SkipOnSuccess,
		})
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
		if !h.Match(exitCode, stderr) {
			continue
		}
		if _, dup := seen[h.ID]; dup {
			continue
		}
		markSeen(h.ID)
		msg := h.Suggest
		if h.SuggestFunc != nil {
			msg = h.SuggestFunc(stderr)
		}
		hints = append(hints, shell.Hint{ID: h.ID, Category: h.Category, Message: msg, Why: h.Why})
		firedIDs = append(firedIDs, h.ID)
		shell.ObserveHint(h.ID, h.Category, "post")
	}
	RecordPost(exitCode, firedIDs)
	end(nil, "fired_count", itoa(len(firedIDs)), "exit_code", itoa(exitCode))
	return hints
}

// matchBinPath fires when stderr names a /bin/<tool> we know diverges
// between Linux and macOS. The shell-exec layer surfaces ENOENT for
// these as exit 127 with "no such file or directory" in stderr; the
// match is lenient on the exit code (some runners report 1 instead)
// and keys off the path text.
func matchBinPath(_ int, stderr string) bool {
	if stderr == "" {
		return false
	}
	low := strings.ToLower(stderr)
	if !strings.Contains(low, "no such file") && !strings.Contains(low, "not found") {
		return false
	}
	return binPathRE.MatchString(stderr)
}

// suggestBinPath builds the per-match hint copy: name the /bin/<tool>
// the script hit, the /usr/bin/<tool> macOS equivalent, AND a ycode
// native verb the agent could reach for instead. The yc suggestion is
// the teachable-moment payload — the path fix unblocks the agent, the
// yc nudge upgrades them.
func suggestBinPath(stderr string) string {
	m := binPathRE.FindStringSubmatch(stderr)
	if m == nil {
		return ""
	}
	tool := m[1]
	ycSuggest := binPathTools[tool]
	if ycSuggest == "" {
		ycSuggest = "see `yc help` for ycode-native alternatives"
	}
	return "`/bin/" + tool + "` doesn't exist on macOS — try `/usr/bin/" + tool +
		"`. While you're here: " + ycSuggest + " is the ycode-native path."
}

// isEnvKey reports whether s is a syntactically valid shell env-var
// name: leading letter or underscore, then letters/digits/underscores.
func isEnvKey(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_':
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

// isRemoteExec reports whether the command's effective first token is
// `ssh`, after skipping NAME=VALUE assignments and common wrappers
// (`env`, `/usr/bin/env`, `nohup`, `time`). When ssh runs a body on a
// remote host the local hint engine has no useful suggestions — the
// remote filesystem isn't reachable by `yc symbols`, `yc repomap`, etc.
func isRemoteExec(command string) bool {
	s := strings.TrimSpace(command)
	for s != "" {
		first, rest, _ := strings.Cut(s, " ")
		first = strings.TrimSpace(first)
		if first == "" {
			s = strings.TrimSpace(rest)
			continue
		}
		// NAME=VALUE env assignment prefix — only the key side is constrained
		// (must be a shell-identifier); the value may contain slashes etc.
		if eq := strings.IndexByte(first, '='); eq > 0 && isEnvKey(first[:eq]) {
			s = strings.TrimSpace(rest)
			continue
		}
		switch first {
		case "env", "/usr/bin/env", "/bin/env", "nohup", "time", "exec":
			s = strings.TrimSpace(rest)
			continue
		}
		return first == "ssh"
	}
	return false
}

// Dedup of hint IDs. Always process-local; additionally file-backed
// when $YCODE_SESSION_ID is set so successive `ycode shell -c "..."`
// invocations in one agent session don't re-fire the same hint. The
// file lives at $XDG_RUNTIME_DIR/ycode/hints-seen-$SESSION_ID.txt
// (fallback $TMPDIR/ycode/hints-seen-$SESSION_ID.txt). Append-only,
// best-effort: I/O errors degrade silently to in-memory-only.
var (
	seenMu       sync.Mutex
	seen         = map[string]struct{}{}
	seenLoadOnce sync.Once
)

func suggestSeen() map[string]struct{} {
	seenMu.Lock()
	seenLoadOnce.Do(loadSeenFromSessionFile)
	return seen
}

func suggestRelease(_ map[string]struct{}) {
	seenMu.Unlock()
}

// ResetSeen clears the dedup table. Used by tests.
func ResetSeen() {
	seenMu.Lock()
	seen = map[string]struct{}{}
	// Re-arm the lazy loader so tests setting $YCODE_SESSION_ID after
	// ResetSeen still pick up the file on the next Suggest call.
	seenLoadOnce = sync.Once{}
	seenMu.Unlock()
}

// MarkSeen records id in the in-memory set and, when a session file is
// configured, appends it to disk. Caller must hold seenMu.
func markSeen(id string) {
	if _, dup := seen[id]; dup {
		return
	}
	seen[id] = struct{}{}
	if path := sessionDedupPath(); path != "" {
		// Best-effort append; ignore errors (file may be unwriteable in
		// CI sandboxes, that's fine — we still dedup in-process).
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		_, _ = f.WriteString(id + "\n")
		_ = f.Close()
	}
}

// loadSeenFromSessionFile populates `seen` from the session dedup file
// when $YCODE_SESSION_ID is set. Called once per process. Errors are
// silent — a missing file is the normal first-call case.
func loadSeenFromSessionFile() {
	path := sessionDedupPath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		id := strings.TrimSpace(line)
		if id != "" {
			seen[id] = struct{}{}
		}
	}
}

// sessionDedupPath returns the path to the per-session dedup file, or
// "" when $YCODE_SESSION_ID is unset or the runtime dir can't be
// determined. Creates the parent directory on first call.
func sessionDedupPath() string {
	sid := strings.TrimSpace(os.Getenv("YCODE_SESSION_ID"))
	if sid == "" {
		return ""
	}
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "ycode")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	return filepath.Join(dir, "hints-seen-"+sid+".txt")
}

// ManifestEntries returns the hint catalog as manifest rows for
// `ycode shell --manifest`.
func ManifestEntries() []shell.ManifestHint {
	out := make([]shell.ManifestHint, 0, len(Catalog)+len(PostCatalog))
	for _, h := range Catalog {
		out = append(out, shell.ManifestHint{
			ID: h.ID, Pattern: h.Pattern.String(), Suggest: h.Suggest, Category: h.Category, Why: h.Why,
		})
	}
	for _, h := range PostCatalog {
		out = append(out, shell.ManifestHint{
			ID: h.ID, Pattern: "(post-exec)", Suggest: h.Suggest, Category: h.Category, Why: h.Why,
		})
	}
	return out
}

func init() {
	shell.SetSuggestFunc(Suggest)
	shell.SetHintCatalogForManifest(ManifestEntries())
	shell.SetPostHintsFunc(SuggestPost)
	shell.SetAutoSandboxFunc(MaybeAutoSandbox)
}
