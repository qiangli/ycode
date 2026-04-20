# AGENTS.md

ycode — pure Go CLI agent harness for autonomous software development.

**Read [INSTRUCTIONS.md](./INSTRUCTIONS.md) first** — it contains the shared project conventions, build commands, testing guidelines, and skill dispatch rules that apply to all AI agents working on this repo.

**Read [CLAUDE.md](./CLAUDE.md)** for Claude Code-specific guidance including build commands, architecture overview, and internal conventions.

**Read [USAGE.md](./USAGE.md)** for CLI modes, configuration, tools, and workflows.

## Quick Reference

```bash
make build        # full quality gate: tidy → fmt → vet → compile → test → verify
make compile      # quick compile only (bin/ycode)
make test         # unit tests only (-short -race)
make deploy       # deploy to localhost:58080 (HOST=<remote> for remote)
make validate     # integration tests against running instance
```

Single test: `go test -short -race -run TestName ./internal/path/to/package/`

Integration tests: `go test -tags integration -v -count=1 ./internal/integration/...`

## Project-Specific Notes

- **Go 1.26+ required** — uses Go 1.26.1 in go.mod
- **Build entry**: `cmd/ycode/main.go` → compiles to `bin/ycode`
- **Key patterns**: `RuntimeContext` holds all registries (no global state), per-tool middleware wrappers, section-based prompt assembly with dynamic boundary marker
- **Three-tier config merge**: user (`~/.config/ycode/`) > project (`.agents/ycode/`) > local (`.agents/ycode/local/`)
- **Skills are internal** — no `skills/<name>/skill.md` on disk; `/init` is a two-phase command with agentic enhancement
- **Build system**: Three-layer separation — Makefile (deps only) → scripts/ (bash orchestration) → Go (all logic)
- **Go commands exclude `priorart/`** — the `PACKAGES` variable filters out prior art submodules

## Development Workflow

Standard cycle (also available as skills `/build`, `/deploy`, `/validate`):

1. **Build** (`make build`) — must pass before deploy
2. **Deploy** (`make deploy`) — starts `ycode serve` on localhost:58080
3. **Validate** (`make validate`) — runs smoke, integration, acceptance, performance tests

Remote deploy requires passwordless SSH: `ssh -o BatchMode=yes <host> "echo ok"`

## For More Detail

Read these on demand, not upfront:
- [docs/architecture.md](./docs/architecture.md) — full architecture, design decisions, component details
- [skills/README.md](./skills/README.md) — skill system convention
