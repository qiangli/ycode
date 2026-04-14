# CLAUDE.md

ycode — pure Go CLI agent harness for autonomous software development.

**Read [INSTRUCTIONS.md](./INSTRUCTIONS.md) first** — it contains the shared project conventions, build commands, testing guidelines, and skill dispatch rules that apply to all AI agents working on this repo.

## Claude Code-Specific Notes

### Committing Changes

When committing, also apply this Claude Code-specific behavior on top of the `/commit` skill:

- **Use the initial git status snapshot.** The system prompt includes the git status and diff captured at session start. Compare current `git status` against that snapshot to distinguish pre-existing dirty files from changes made during this session — do not stage pre-existing changes.

## For More Detail

Read these on demand, not upfront:
- [USAGE.md](./USAGE.md) — CLI modes, config, tools, workflows
- [docs/architecture.md](./docs/architecture.md) — full architecture, design decisions, component details
