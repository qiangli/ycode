# selfinit — ycode as the agentic-tool kernel

ycode is the OS / infrastructure for agentic coding tools. Every Claude Code, OpenCode, Codex, or Gemini CLI session runs *on top of* a shared local substrate — memex (memory), ollama (inference), pulse (observability), treesitter (AST). `selfinit` is the mechanism that makes ycode a **first-class citizen in every local git repo on every entry-point invocation**.

## What it does

On any ycode entry-point (`ycode`, `ycode prompt`, `ycode serve`), `selfinit.Run` does this once:

1. Walks up from cwd to find the git repo root. Outside a git repo, project-scope writes are skipped — but user-scope writes (foreign-tool configs) still happen.
2. Checks `<repo>/.ycode/.init-done` against a state hash of the manifest, ycode version, and detected tool list. Match ⇒ no-op.
3. Detects installed agentic tools (claude on PATH or `~/.claude/`; opencode on PATH or `~/.config/opencode/`; codex / gemini queued).
4. Writes `<repo>/.ycode/AGENTS.md` — the long-form, manifest-derived ycode awareness doc.
5. Patches `<repo>/AGENTS.md` and/or `<repo>/CLAUDE.md` with a small delimited reference block (or, in greenfield repos where neither exists, creates `AGENTS.md` as a fully ycode-owned file marked on line 1).
6. For each detected foreign tool, refreshes its user-scope memory file with a delimited block describing the `yc <verb>` shell built-ins:
   - **Claude Code**: `~/.claude/CLAUDE.md`.
   - **OpenCode**: `~/.config/opencode/AGENTS.md`.

   No tool-config file is touched. ycode does not run an MCP server, so
   nothing is written to `~/.claude.json`'s `mcpServers` map or to
   `~/.config/opencode/opencode.json`'s `mcp` map.
7. Drops a fresh marker.

## Greenfield vs brownfield

| State of `<repo>/AGENTS.md` and `<repo>/CLAUDE.md` | Result |
|---|---|
| Both exist | Both patched with delimited block |
| Only `AGENTS.md` exists | Only `AGENTS.md` patched |
| Only `CLAUDE.md` exists | Only `CLAUDE.md` patched (no AGENTS.md created) |
| Neither exists | `AGENTS.md` created, fully ycode-owned. Line 1 carries the `<!-- ycode-owned: ... -->` marker; remove that line to reclaim the file (next refresh switches to delimiter mode automatically) |

## Idempotency

Every write is content-compared against the current file before touching disk; identical content is a no-op (mtime preserved, no spurious diffs). Splice spacing is normalised on every call, so brownfield re-runs converge to a fixed point.

## How to invoke

| Path | Trigger |
|---|---|
| Auto on startup | Any ycode subcommand (cobra root `PersistentPreRun`) |
| Explicit refresh | `ycode init --refresh` |
| Doctor (preview) | `ycode init --doctor` |
| Per-repo opt-out | `ycode init --opt-out` (writes `<repo>/.ycode/.no-init`) |
| Slash command (in TUI) | `/init` — runs the existing AGENTS.md-via-LLM generator, then SelfInit on top |

## Opting out

| Granularity | How |
|---|---|
| Per repo | `<repo>/.ycode/.no-init` (created by `ycode init --opt-out`) |
| Per process | `--no-self-init` flag, `YCODE_NO_SELF_INIT=1` env |

## Implementation pointers

- `internal/selfinit/selfinit.go` — `Run(ctx, opts)` orchestrator.
- `internal/selfinit/{manifest,injection,marker,detect}.go` — capability source, markdown splice, state hash, git-root walk + tool detection.
- `internal/selfinit/{claude,opencode}.go` — per-tool L1 + L2 writers, registered via `init()` into the package-level registry.
- `internal/selfinit/project.go` — greenfield/brownfield project-scope writes.
- `cmd/ycode/selfinit_hook.go` — cobra root hook that fires SelfInit on every invocation.
- `cmd/ycode/init_cmd.go` — explicit `ycode init` subcommand with `--refresh|--doctor|--opt-out|--json`.
- `internal/commands/handlers.go` — `/init` slash command runs SelfInit after the LLM-driven AGENTS.md regeneration.

## Adding a new foreign tool

Per-tool writer is one Go file in `internal/selfinit/`. Implement the `selfinit.Tool` interface:

```go
type Tool interface {
    Name() string
    Detect() bool
    WriteInstructions(ctx context.Context) (changed bool, err error)
}
```

`WriteInstructions` takes no capability list: the block it writes
describes only surfaces ycode actually serves — the `yc <verb>` shell
built-ins — so there is nothing to enumerate per host.

…then `RegisterTool(&yourTool{})` from an `init()`. SelfInit picks it up automatically. Reference impls: `claude.go`, `opencode.go`.
