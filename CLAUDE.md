# CLAUDE.md

ycode — pure Go CLI agent harness for autonomous software development.

## Quick Reference

```bash
make build       # full quality gate: tidy → fmt → vet → test → compile → verify
make deploy      # deploy to localhost:58080 (HOST=<remote> PORT=<port>)
make validate    # smoke + integration + acceptance + performance tests
make compile     # quick compile only
make test        # unit tests only
```

Single test: `go test -race -run TestName ./internal/path/to/package/`

## Skills

**TRIGGER**: On each user query, check `./skills/` for a matching skill. If the user's request matches a skill name or description, read `skills/<name>/skill.md` and follow its instructions.

Available skills:
- **build** — `skills/build/skill.md` — build, fix errors, commit on success
- **deploy** — `skills/deploy/skill.md` — deploy to host, ensure build first
- **validate** — `skills/validate/skill.md` — run test suites, ensure build+deploy first

## For More Detail

Read these on demand, not upfront:
- [USAGE.md](./USAGE.md) — CLI modes, config, tools, workflows
- [docs/architecture.md](./docs/architecture.md) — full architecture, design decisions, component details
