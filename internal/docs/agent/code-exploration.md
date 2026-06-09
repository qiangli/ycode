---
topic: code-exploration
summary: AST-aware code search and repo orientation
when: you need to find a declaration, a caller, or orient in unfamiliar code, and grep/find would surface false positives or take many round-trips
audience: agent
max_lines: 120
---

Four shell built-ins compose into a complete code-exploration loop
that beats `grep -rn` / `find -name '*.go'` / `ctags -R` on every
axis that matters for an agent: AST awareness (no comment/string
matches), structured scope (declared identifiers, not lexical hits),
zero index maintenance, and one call instead of a probe loop.

Reach for these BEFORE `grep` whenever the source file is Go, Python,
JavaScript/TypeScript, Rust, Java, C, or Ruby. For config files, logs,
or plain text — grep is still the right answer.

## When to use this

Picking by question type:

| You want to ... | Use |
|---|---|
| Enumerate the declarations in a file or directory | `yc symbols <path>` |
| Orient in an unfamiliar repo (one call, token-budgeted) | `yc repomap [--budget=N]` |
| Find a name across the workspace (substring or regex) | `yc search-symbols <pattern> [path]` |
| Find every caller / reference of a symbol | `yc refs <symbol>` |

Picking by previous-instinct:

| If you were about to type ... | Use this instead |
|---|---|
| `ctags -R`, `grep -E '^(func\|type)' file.go` | `yc symbols file.go` |
| `find . -name '*.go' \| xargs head`, `tree` | `yc repomap` |
| `grep -rn 'FuncName' .`, `rg 'FuncName'` | `yc search-symbols FuncName` |
| `grep -rn 'FuncName(' .` (find callers) | `yc refs FuncName` |

## Why AST-aware wins

- **No comment/string hits.** `grep` matches a function name inside a
  docstring or a JSON example; `yc search-symbols` doesn't.
- **No vendored-copy noise.** Go aliases and TypeScript re-exports
  resolve to the declared identifier; `yc search-symbols` doesn't fan
  out into `vendor/` or `node_modules/` copies.
- **One call per intent.** No `grep ... | head`, no `find ... -exec`,
  no "run again with `-w`" because the first regex over-matched.

## Tool surface

- `yc symbols <path>` — top-level declarations (func, type, class,
  method) at `<path>` (file or directory). Treesitter-native.
- `yc repomap [path] [--budget=N] [--query=<text>] [--json]` —
  token-budgeted file→symbol overview. `--query` ranks files by
  relevance to a text query so the top of the list is the most likely
  starting point.
- `yc search-symbols <pattern> [path] [--json]` — name-substring or
  regex search across declared identifiers. Pattern is matched against
  symbol names, not source lines.
- `yc refs <symbol> [--json]` — find references and callers of
  `<symbol>` across the workspace. Resolves through imports/aliases.

All four also have MCP equivalents (`mcp__ycode__list_symbols`,
`mcp__ycode__build_repomap`, `mcp__ycode__search_symbols_by_pattern`,
`mcp__ycode__find_symbol_references`) when you're inside an agent that
prefers tool calls to shell commands.

## Failure modes

| Symptom | Fix |
|---|---|
| Empty output for a file you can see | Language not in the supported set, or the file has parse errors. Open it directly. |
| `yc search-symbols` misses a hit `grep` finds | The hit was in a comment, string literal, or a renamed import — that's the design. Use `grep` as a fallback for that file. |
| `yc repomap` budget too small | Default is conservative; pass `--budget=8000` (or higher) for a wider sweep. |
| `yc refs` returns zero | The symbol may be declared in a sibling module not yet in the workspace, or the name is too common. Pair with `--json` and a regex post-filter. |

## Workflow recipes

Orient in a repo you've never seen:

```sh
yc repomap --budget=4000        # ranked top-level overview
yc symbols internal/some/dir    # zoom into one directory
```

Trace a function from declaration to all callers:

```sh
yc search-symbols MyFunc        # where is it declared?
yc refs MyFunc                  # who calls it?
```

Find anything matching a fuzzy intent:

```sh
yc search-symbols 'auth.*middleware'   # regex; matches AuthMiddleware, authedMiddleware
```

## Exact calls

- File-level declarations: `yc symbols ./internal/runtime/conversation/runtime.go`
- Directory enumeration: `yc symbols ./internal/runtime/`
- Repo orientation: `yc repomap --budget=4000`
- Query-ranked orientation: `yc repomap --query="auth middleware"`
- Substring search: `yc search-symbols Dispatch`
- Regex search, scoped: `yc search-symbols '^run' ./cmd/`
- Find callers: `yc refs DispatchEnvelope`
- Machine-readable: append `--json` to any of the above
