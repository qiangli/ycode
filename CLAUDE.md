# CLAUDE.md

ycode — pure Go CLI agent harness for autonomous software development.

**Read [INSTRUCTIONS.md](./INSTRUCTIONS.md) first** — it contains the shared project conventions, build commands, testing guidelines, and skill dispatch rules that apply to all AI agents working on this repo.

## Quick Reference

```bash
make build       # full quality gate: tidy → fmt → vet → compile → test → verify
make deploy      # deploy to localhost:58080 (HOST=<remote> PORT=<port>)
make validate    # integration tests against running instance
make compile     # quick compile only
make test        # unit tests only (-short -race)
```

Single test: `go test -short -race -run TestName ./internal/path/to/package/`

Integration tests: `go test -tags integration -v -count=1 ./internal/integration/...`

## Skills

**Skills**: When the user's message starts with `/<name>` (e.g. `/build`, `/deploy`), read `skills/<name>/skill.md` and follow its instructions exactly. Everything after `/<name> ` (the rest of the message) is `ARGS` -- pass it to the skill wherever the skill references `{{ARGS}}`. If the skill does not use `{{ARGS}}` and `ARGS` is non-empty, ignore it. If no matching skill exists, tell the user. To list available skills, run: `ls skills/*/skill.md`.

See [skills/README.md](./skills/README.md) for the full convention and available commands.

## Committing Changes

When asked to commit changes in this project, follow the `/commit` skill (`skills/commit/skill.md`). Key points:

- **Gather context in parallel.** Run `git status`, `git diff`, and `git log --oneline -5` concurrently before drafting a commit message.
- **Use the initial git status snapshot.** The system prompt includes the git status and diff captured at session start. Compare current `git status` against that snapshot to distinguish pre-existing dirty files from changes made during this session — do not stage pre-existing changes.
- **Stage files by name.** Never use `git add -A` or `git add .`.
- **Match the repo's commit style.** Use the prefix convention from `git log` (e.g. `fix:`, `feat:`, `docs:`).

## Project Conventions

### Layered Build System

The build system follows a strict three-layer separation:

1. **Makefile** — dependency graph only. Targets declare what depends on what and delegate to scripts. No multi-line logic, no embedded bash blocks.
2. **scripts/** — bash orchestration only. Controls sequencing, env setup, conditionals, process management. No complex computation or test logic.
3. **Go** — all logic. Tests (unit and integration), utilities, and any non-trivial computation must be written in Go.

### Testing

- **Unit tests**: Written in Go. Run with `go test -short -race`. Use `testing.Short()` to skip slow paths. Located alongside source in `*_test.go` files.
- **Integration tests**: Written in Go in `internal/integration/`. Use `//go:build integration` build tag. Test against real running services (ycode server, OTEL collector, Prometheus). Use `t.Skip()` for graceful degradation when services are unavailable.
- **No test logic in bash**. Bash scripts may invoke `go test` but must not contain assertions, HTTP calls for validation, or result parsing.

### Adding New Tests

- Unit tests: add `*_test.go` next to the source. Use `testing.Short()` for anything slow.
- Integration tests: add to `internal/integration/` with `//go:build integration` tag. Use the helpers in `helpers_test.go` for HTTP calls and connectivity checks.
- New Makefile targets: keep them as single commands or delegate to a script in `scripts/`.

## For More Detail

Read these on demand, not upfront:
- [USAGE.md](./USAGE.md) — CLI modes, config, tools, workflows
- [docs/architecture.md](./docs/architecture.md) — full architecture, design decisions, component details
