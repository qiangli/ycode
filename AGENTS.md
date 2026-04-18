# AGENTS.md

ycode — pure Go CLI agent harness for autonomous software development.

**Read [INSTRUCTIONS.md](./INSTRUCTIONS.md) first** — it contains the shared project conventions, build commands, testing guidelines, and skill dispatch rules that apply to all AI agents working on this repo.

**Read [CLAUDE.md](./CLAUDE.md)** for Claude Code-specific guidance including build commands, architecture overview, and internal conventions.

**Read [USAGE.md](./USAGE.md)** for CLI modes, configuration, tools, and workflows.

## Project-Specific Notes

- **Go 1.26+ required** — uses Go 1.26.1 in go.mod
- **Build entry**: `cmd/ycode/main.go` → compiles to `bin/ycode`
- **Key patterns**: `RuntimeContext` holds all registries (no global state), per-tool middleware wrappers, section-based prompt assembly with dynamic boundary marker
- **Three-tier config merge**: user (`~/.config/ycode/`) > project (`.agents/ycode/`) > local (`.agents/ycode/local/`)
- **Skills are internal** — no `skills/<name>/skill.md` on disk; `/init` is a two-phase command with agentic enhancement

## For More Detail

Read these on demand, not upfront:
- [docs/architecture.md](./docs/architecture.md) — full architecture, design decisions, component details
- [skills/README.md](./skills/README.md) — skill system convention
