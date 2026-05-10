# Dogfooding the skill catalog

A one-week structured run to validate the
[external skill catalog](./skills.md) (`github.com/dhnt/dhnt/catalog`)
against real ycode work, capture friction, and ship round-1 fixes
upstream.

ycode is the first consumer of the catalog. The hypothesis is that
the catalog covers the full software-development lifecycle well
enough to drive day-to-day work; the dogfood week is how we find
out where it doesn't.

## Setup (one-time, ~15 min)

The infrastructure is wired into ycode already; you just need to
know where to look.

### Telemetry

Every `/<skill>` invocation appends one JSON line to
**`~/.agents/ycode/skill-usage.jsonl`**:

```json
{"ts":"2026-05-09T12:34:56Z","name":"commit","source":"external_builtin","args_len":0,"ok":true,"latency_ms":35}
```

Sources:

| Source | Meaning |
|---|---|
| `internal` | Resolved via local overlay (`.agents/ycode/skills/`) |
| `external` | Resolved via dhnt catalog, `executor: markdown` |
| `external_builtin` | Catalog entry routed to a registered builtin Go executor |
| `external_cnl` | Catalog entry with `executor: cnl` (not yet supported) |
| `builtin` | Legacy builtin executor without a catalog entry |
| `not_found` | No skill matched the name |

The log is append-only, kilobytes per week, never rotated. Failure-
quiet: if the log can't be written, dispatch still works.

### Friction log

Create the file once:

```sh
mkdir -p ~/.agents/ycode
cat > ~/.agents/ycode/dogfood.md <<'EOF'
# skill-CNL dogfood log

> Two-line entries when a skill stings. Don't fix mid-week — collect.
> End of week: synthesise into action items per `docs/skills-dogfood.md`.

## YYYY-MM-DD
- /<skill>: <what was wrong, in one sentence>

EOF
```

Append entries as you go. Each entry is two lines:

```
- /<skill>: <what was wrong>
  <one-sentence what you'd want instead>
```

## The week

### Discipline

When you're about to do something that maps to a catalog skill,
**type `/<skill>` instead of describing the task in prose**. The
explicit invocation is what surfaces whether the skill body actually
steers the LLM the way you want.

### Suggested ordering

| Day | Focus | Reason |
|---|---|---|
| 1–2 | Daily-use tier (`commit`, `code-review`, `write-test`, `run-tests`, `explain-changes`, `fix-bug`, `refactor`) | Highest invocation count → fastest quality signal |
| 3–4 | Onboarding tier (`explore-codebase`, `find-entry-point`, `summarize-architecture`) on a repo you don't know well | Tests the discover/plan phases against a real unfamiliar codebase |
| 5 | Integration / release (`open-pr`, `address-pr-comments`, `bump-version`, `draft-release-notes`, `tag-release`) | Less-frequent but high-stakes |
| 6–7 | Opportunistic — operate / maintain / onboard skills as real work demands them | These tend to be sporadic; force usage rather than wait |

### Daily ritual (~5 min)

Once a day, glance at yesterday's log:

```sh
# Yesterday's invocations
jq -r '.ts + " " + .source + " /" + .name + (if .ok then "" else " FAIL " + .err_kind else "" end)' \
  ~/.agents/ycode/skill-usage.jsonl | grep "^$(date -v-1d +%Y-%m-%d)"
```

Note any patterns in the friction log. Don't fix anything; collect.

## End-of-week analysis

### Volume + distribution

```sh
# Invocations per skill, sorted by frequency
jq -r .name ~/.agents/ycode/skill-usage.jsonl | sort | uniq -c | sort -rn

# Invocations per source
jq -r .source ~/.agents/ycode/skill-usage.jsonl | sort | uniq -c | sort -rn

# Failure rate
jq -r 'select(.ok == false) | .name + " " + .err_kind' \
  ~/.agents/ycode/skill-usage.jsonl | sort | uniq -c | sort -rn
```

### Coverage

```sh
# Catalog skills you DIDN'T use (potential dead weight)
comm -23 \
  <(grep -lR "^name:" peers/dhnt/catalog/md | xargs -I{} grep -h "^name:" {} | awk '{print $2}' | sort) \
  <(jq -r .name ~/.agents/ycode/skill-usage.jsonl | sort -u)
```

### Latency

```sh
# Median + p95 latency per source
jq -r .source ~/.agents/ycode/skill-usage.jsonl | sort -u | while read src; do
  echo -n "$src "
  jq -r --arg s "$src" 'select(.source == $s) | .latency_ms' \
    ~/.agents/ycode/skill-usage.jsonl | \
    awk '{a[NR]=$1} END {n=NR; asort(a); print "n="n" med="a[int(n/2)]" p95="a[int(n*0.95)]}'
done
```

### Synthesise

Aggregate into four buckets in your `dogfood.md`:

```markdown
## End-of-week synthesis (YYYY-MM-DD)

### Skills to rewrite (instruction body is wrong / unclear)
- /<skill> — <what's wrong, what to change>

### Skills to add (you wanted one that didn't exist)
- <name> — <what it should do>

### Skills unused (investigate why)
- /<skill> — <hypothesis: wrong abstraction / niche / bad week sample>

### Catalog API changes wanted
- <change to public API in github.com/dhnt/dhnt/catalog>
```

## Shipping the fixes

Round-1 fixes go upstream in `peers/dhnt`:

1. Edit / add `catalog/md/<phase>/<name>/skill.md` files.
2. `GOWORK=off go test -race ./catalog/...` green.
3. Commit, push to `dhnt` master, tag `v0.2.1-alpha.1` (or
   `v0.2.0-alpha.4` if still in the alpha-tier band — minor bumps
   for new skills, patch for fixes only).
4. In ycode: bump `go.mod`, `go mod tidy`, `make build`, commit,
   push.

Then start week two with the refined catalog.

## What this dogfood week is NOT

- Not a comprehensive evaluation. One week of one user is a
  signal, not a verdict.
- Not a replacement for external-consumer feedback. ycode is
  one harness; the catalog's value is multi-tool. A non-ycode
  consumer (next milestone) is the bigger validation.
- Not a license to add 20 new skills. The fixes should be
  conservative — most friction is in *existing* skill bodies,
  not missing ones.
