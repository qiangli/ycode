# Skills

ycode's skills come from two clearly-separated lanes:

| Lane | Where it lives | Visible as | Examples |
|---|---|---|---|
| **External** | [`github.com/dhnt/dhnt/catalog`](https://github.com/dhnt/dhnt) — community catalog, embedded in the binary | `[external]` in `/skills` | `commit`, `code-review`, `security-review`, `init`, `refactor`, `fix-bug`, `write-test`, `fix-test`, … |
| **Internal** | `.agents/ycode/skills/<ycode-…>/skill.md` — ycode-specific tooling | `[internal]` in `/skills` | `ycode-build`, `ycode-deploy`, `ycode-eval`, `ycode-validate`, `ycode-setup`, `ycode-oci`, `ycode-claude`, `ycode-audit`, `ycode-bench-instructions`, `ycode-learn`, `ycode-autopilot`, `ycode-analyze` |

## Loader precedence

When a skill is invoked by name, the loader at
`internal/tools/skill.go:resolveSkill` walks lookups in this order:

1. **Local overlay** — `.agents/ycode/skills/`, `~/.agents/ycode/skills/`,
   `$YCODE_SKILLS_DIR`. A file in any of these with the same name as a
   catalog skill **shadows** the catalog entry. Use this to override an
   external skill in a specific repo without forking the dhnt module.
2. **External catalog** — `github.com/dhnt/dhnt/catalog`, consulted via
   the embedded Go API. Each catalog entry's frontmatter declares an
   `executor:` that controls dispatch:
   - `markdown` (default) — return the body to the LLM as instructions.
   - `builtin` — dispatch to a Go function registered under the same
     name via `internal/runtime/builtin.RegisterSkillExecutor`. Today
     used for `commit` and `init`, both of which are deterministic
     single-shot LLM calls implemented in `internal/runtime/builtin/`.
   - `cnl` — reserved for the future programmatic-skill layer
     (typed-AST machinery in `github.com/dhnt/dhnt/skills`); the loader
     currently returns a clear error.
3. **Builtin executor without a catalog entry** — legacy fallback for
   any executor registered in `internal/runtime/builtin/` whose name
   isn't (yet) in the upstream catalog.

## SDLC organisation (external catalog)

The community catalog is organised by software-development-lifecycle
phase:

```
discover → plan → build → test → review → commit → integrate → release
        → deploy → operate → maintain → document → onboard
```

`v0.2.0-alpha.1` ships the **daily-use tier** — 12 skills covering
build / test / review / commit / document. Subsequent alphas fill in
the rest. The full catalog is enumerable via
`catalog.All()` / `catalog.ByPhase(phase)`.

## Adding a new skill

### To the external (community) catalog

The catalog is a separate repository: clone
[`github.com/dhnt/dhnt`](https://github.com/dhnt/dhnt), add a directory
under `catalog/md/<phase>/<name>/skill.md` with YAML frontmatter +
markdown body, and open a PR. The structural-invariant tests in
`catalog/catalog_test.go` enforce naming conventions before the change
can land. Once the next dhnt tag cuts, ycode's `go.mod` bumps to that
version and the new skill is automatically available.

### To the internal (ycode-only) lane

Create `.agents/ycode/skills/<name>/skill.md` directly in this repo.
Follow the same frontmatter schema as the catalog. ycode-specific
skills should carry the `ycode-` prefix to distinguish them from
shadowed catalog entries.

### Frontmatter schema

```yaml
---
name: <slug>                          # required, must match directory name
description: <one-line>               # required
phase: discover|plan|build|test|review|commit|integrate|release|deploy|operate|maintain|document|onboard
executor: markdown|builtin|cnl        # default: markdown
user_invocable: true                  # whether /<name> is exposed
aliases: [<alt>, …]                   # optional synonyms
origin: priorart/<tool>|new           # provenance
---
```

## Slash commands

Five slash commands resolve to catalog skills via the unified loader:

| Slash command | Resolves to | Executor |
|---|---|---|
| `/init` | `document/init` | builtin |
| `/commit` | `commit/commit` | builtin |
| `/review` | `review/code-review` (alias `review`) | markdown |
| `/security-review` | `review/security-review` | markdown |
| `/plan` | (planned, not yet in catalog) | — |

The remaining ~23 slash commands are session/admin affordances
(`/help`, `/status`, `/clear`, `/compact`, `/model`, `/search`,
`/doctor`, …) that don't follow the skill shape. They stay as direct
handlers.

## Listing skills at runtime

```sh
bin/ycode
> /skills
```

Output looks like:

```
Available skills (24):
  [external]   code-review
  [external]   commit
  [external]   explain-changes
  …
  [internal]   ycode-analyze
  [internal]   ycode-build
  …
```

## See also

- [`skill-cnl.md`](./skill-cnl.md) — the typed-AST programmatic-skill
  layer (separate concern; consumed via `executor: cnl` in catalog
  frontmatter once dispatch lands).
- [`skill-cnl-rationale.md`](./skill-cnl-rationale.md) — design
  rationale for the dhnt-CNL layer.
- [`skills-summary.md`](./skills-summary.md) — the priorart competitive
  survey that informed the catalog seeding.
- [github.com/dhnt/dhnt](https://github.com/dhnt/dhnt) — the upstream
  catalog repository.
