# AI Agent Instructions

Shared instructions for any AI coding assistant working on this repository.
This file is agent-agnostic — it applies to Claude Code, Cursor, Copilot, Windsurf, Cline, or any other AI tool.

Tool-specific instruction files (CLAUDE.md, .cursorrules, etc.) should reference this file for shared conventions.

## Skills

This repository uses a **skill system** — reusable, agent-agnostic slash commands defined as markdown files.

**Dispatch rule**: When the user's message starts with `/<name>` (e.g. `/build`, `/deploy`, `/learn`), read `skills/<name>/skill.md` and follow its instructions exactly. Everything after `/<name> ` (the rest of the message) is `ARGS` — pass it to the skill wherever the skill references `{{ARGS}}`. If the skill does not use `{{ARGS}}` and `ARGS` is non-empty, ignore it. If no matching skill exists, tell the user. To list available skills, run: `ls skills/*/skill.md`.

See [skills/README.md](./skills/README.md) for the full convention and available commands.

### Internal Skills (embedded in the ycode binary)

Some skills are **internal** — compiled into the ycode binary with no `skills/<name>/skill.md` file on disk. They are dispatched via the `Skill` tool or as registered commands with `AgentTurn: true`.

#### `/init [focus]` — Guided AGENTS.md Setup

The `/init` command is a **two-phase command** — it runs a deterministic scaffold, then chains into an agentic conversation turn where the LLM enhances the generated files.

**Phase 1 — Scaffold (deterministic, no LLM):**
The command handler creates the workspace structure immediately, before the LLM is involved:
- `.agents/ycode/` directory (config, sessions, cache, logs)
- `.agents/ycode.json` with auto-detected project metadata (languages, frameworks, build commands)
- `.gitignore` entries for ycode artifacts
- Template `AGENTS.md` (if it doesn't already exist)

This phase is idempotent — running `/init` again skips existing files. The scaffold report is displayed to the user.

**Phase 2 — Enhancement (LLM-driven):**
After the scaffold completes, an agentic turn starts automatically. The LLM receives the original `/init [focus]` input, calls the `Skill("init")` tool, and gets back instructions to:
1. Scan the project (README, manifests, CI config, existing instruction files)
2. Enhance `AGENTS.md` with project-specific content — build commands, architecture notes, testing quirks, conventions that differ from defaults
3. Keep it compact: every line must answer "would an agent miss this without help?"

The enhancement instructions are embedded in the binary (`init_skill.md`). The LLM uses only read-only tools (`read_file`, `glob_search`, `grep_search`) during the scan, plus `edit_file`/`write_file` to update AGENTS.md.

**Design rationale:** Phase 1 guarantees the workspace exists even if the LLM call fails or is cancelled. Phase 2 produces high-quality, project-specific agent instructions that would otherwise require manual authoring. This matches the pattern used by modern agentic tools (e.g. opencode's `/init` guided setup).

#### `/commit [message]`

The `/commit` skill is also embedded in the binary. It generates a commit message via a single LLM call and runs git commands directly, bypassing the full conversation runtime.

## Project Conventions

### Layered Build System

The build system follows a strict three-layer separation:

1. **Makefile** — dependency graph only. Targets declare what depends on what and delegate to scripts. No multi-line logic, no embedded bash blocks.
2. **scripts/** — bash orchestration only. Controls sequencing, env setup, conditionals, process management. No complex computation or test logic.
3. **Go** — all logic. Tests (unit and integration), utilities, and any non-trivial computation must be written in Go.

### Quick Reference

```bash
make build       # full quality gate: tidy → fmt → vet → compile → test → verify
make deploy      # deploy to localhost:58080 (HOST=<remote> PORT=<port>)
make validate    # integration tests against running instance
make compile     # quick compile only
make test        # unit tests only (-short -race)
```

Single test: `go test -short -race -run TestName ./internal/path/to/package/`

Integration tests: `go test -tags integration -v -count=1 ./internal/integration/...`

### Testing

- **Unit tests**: Written in Go. Run with `go test -short -race`. Use `testing.Short()` to skip slow paths. Located alongside source in `*_test.go` files.
- **Integration tests**: Written in Go in `internal/integration/`. Use `//go:build integration` build tag. Test against real running services (ycode server, OTEL collector, Prometheus). Use `t.Skip()` for graceful degradation when services are unavailable.
- **No test logic in bash**. Bash scripts may invoke `go test` but must not contain assertions, HTTP calls for validation, or result parsing.

### Adding New Tests

- Unit tests: add `*_test.go` next to the source. Use `testing.Short()` for anything slow.
- Integration tests: add to `internal/integration/` with `//go:build integration` tag. Use the helpers in `helpers_test.go` for HTTP calls and connectivity checks.
- New Makefile targets: keep them as single commands or delegate to a script in `scripts/`.

### Committing Changes

When asked to commit changes in this project, follow the `/commit` skill (embedded in the ycode binary). Key points:

- **Gather context in parallel.** Run `git status`, `git diff`, and `git log --oneline -5` concurrently before drafting a commit message.
- **Stage files by name.** Never use `git add -A` or `git add .`.
- **Only stage your own changes.** If the working tree was already dirty at session start, do not stage pre-existing modifications — only stage files you changed.
- **Match the repo's commit style.** Use the prefix convention from `git log` (e.g. `fix:`, `feat:`, `docs:`).

## For More Detail

Read these on demand, not upfront:
- [USAGE.md](./USAGE.md) — CLI modes, config, tools, workflows
- [docs/architecture.md](./docs/architecture.md) — full architecture, design decisions, component details
