---
topic: repomap
summary: token-budgeted file→symbol overview of a repo
when: you've just landed in a repo and need orientation without burning context
audience: agent
max_lines: 80
---

Repomap walks a workspace, parses every supported source file with
treesitter, and emits a token-budgeted summary: per-file the top-level
symbols (functions, types, methods, exported names). It replaces the
"find files then head them" dance with one call that fits your context
budget by construction.

## When to use this

- First contact with an unfamiliar repo. One repomap call, then read
  only the files that look relevant.
- Before delegating to a sub-agent — give the sub-agent the repomap
  output instead of an empty `cwd` so it doesn't re-walk.
- You're answering "where is X?" — repomap will name the file that
  declares X without you having to grep.

Supported languages: Go, Python, JavaScript/TypeScript, Rust, Java, C,
Ruby. Files in unsupported languages are listed by path only, no
symbols.

## Tool surface

- **`yc repomap [path] [--budget=N] [--query=<text>] [--json]`** —
  shell built-in. `--budget` is in tokens; default fits in a comfortable
  context slice. `--query` ranks files by relevance to a text query.
- **MCP `build_repomap`** — walk a directory; same budget semantics.
- **MCP `repomap_for_files`** — parse an explicit list of files
  instead of walking. Use when you already know the files you care
  about and want their symbol overview specifically.

## Failure modes

| Symptom | Fix |
|---|---|
| Empty output for `--path=.` | Path may be outside a recognized workspace; pass an explicit project root. |
| Symbols missing for a real file | Language not supported by treesitter, or file has parse errors. Open the file directly. |
| Budget too small to be useful | Raise `--budget` — default is conservative; doubling it is usually fine for a single look. |

## Exact calls

- Repo orientation, shell: `yc repomap --budget=4000`
- Filtered by query: `yc repomap --query="auth middleware"`
- One-file detail (faster than treesitter from scratch each time):
  `yc symbols path/to/file.go`
- MCP form: `build_repomap` with `{path, budget?, query?}`.
- MCP for an explicit set: `repomap_for_files` with `{files: [...]}`.
