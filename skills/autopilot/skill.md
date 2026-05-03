---
name: autopilot
description: Autonomously analyze agentic tools, identify gaps in three focused domains, implement, test, and commit — fully unattended
user_invocable: true
---

# /autopilot — Autonomous Agentic Tool Analysis

Fully autonomous, end-to-end analysis of agentic tool(s) at a given path. Detects and classifies tools, then runs three complete independent cycles — one per focus domain — each going from research through commit without stopping for approval.

`{{ARGS}}` is required. A filesystem path containing agentic tool source code.

Examples:
- `./priorart/aider` — single tool
- `./priorart/` — all tools in the directory
- `~/projects/myrepo/src/` — arbitrary path

If `{{ARGS}}` is empty, ask the user for a path. That is the **only** prompt. After receiving the path, run to completion autonomously.

If `--push` appears in `{{ARGS}}`, push commits to remote after each domain.

---

## Step 1: DETECT — Identify and Classify Tools

Scan `{{ARGS}}`:

1. `ls` the path. Determine single-tool (source tree) vs multi-tool (directory of subdirectories).
2. For each candidate, check for agentic tool indicators:
   - LLM API imports (anthropic, openai, ollama, litellm, google.generativeai)
   - Tool/function-calling schemas or registry
   - Conversation loop, message array, streaming response handler
   - CLI/TUI entry point (cobra, urfave, ink, bubble tea, click)
3. Record: name, language, type (CLI agent / framework / SDK), key indicators.
4. Log detected tools and skipped directories. Proceed immediately.

---

## Step 2: For Each Tool — Run Three Domain Cycles

For each detected tool, execute three complete cycles sequentially. Each cycle covers one focus domain end-to-end: RESEARCH → PLAN → BUILD → TEST → FIX → COMMIT.

---

### Cycle 1 of 3: Domain A — Agent Orchestration & Workflow

Gap ID prefix: `A`

**Study areas:**
- Multi-agent coordination (coordinator, swarm, mesh, DAG, leader-worker)
- Task delegation and lifecycle (local, remote, background, forked agents)
- Workflow phases (init, explore, plan, build, test, evaluate, commit)
- Hook system (pre/post tool use, session lifecycle, permission hooks)
- Skill system (built-in, disk-based, auto-selection, evolution, gating)
- Feedback loops (self-improvement, loop/stuck detection, recovery, self-healing)
- Scheduling (cron, recurring loop, triggers, background jobs)
- Git integration (worktree isolation, commit conventions, PR workflows)

**ycode reference packages:**
`internal/runtime/conversation/`, `internal/runtime/swarm/`, `internal/mesh/`, `internal/runtime/hooks/`, `internal/runtime/skillengine/`, `internal/selfheal/`, `internal/runtime/autoloop/`, `internal/runtime/sprint/`, `internal/runtime/agentpool/`, `internal/runtime/agentdef/`

#### A-RESEARCH

1. Deep-read the tool's source for all Domain A study areas. Use parallel exploration. Record what the tool implements, quality, and novel approaches with file paths.
2. Simultaneously read ycode's Domain A packages. Record what ycode already has, what is partial, what is missing, and where ycode is **stronger**.

#### A-PLAN

Write `docs/gap-analysis-<toolname>-orchestration.md`:
- **Where ycode Is Stronger** — table
- **Gaps Identified** — table: ID, Feature, Tool Implementation, ycode Status, Priority, Effort
- **Implementation Plan** — phased with files to create/modify and design notes
- **Deferred** — low-priority items with reason
- **Verification** — how to test

If no gaps: write the analysis documenting strengths, state "No actionable gaps identified", skip A-BUILD.

#### A-BUILD

For each High/Medium priority item:
1. Read target files first
2. Write implementation + unit tests (follow ycode conventions: no global state, `slog` logging, `t.TempDir()`, permissive licenses, `priorart/` read-only)
3. Run `make build` — if it fails, fix and re-run until `=== Build PASSED ===`

#### A-COMMIT

```bash
git add <specific files only>
git commit -m "$(cat <<'EOF'
feat: add orchestration gaps from <tool-name> analysis

<summary of changes by phase>
EOF
)"
```

If `--push`: `git push`

---

### Cycle 2 of 3: Domain M — Memory Management & Context Engineering

Gap ID prefix: `M`

**Study areas:**
- Conversation history (persistence, replay, resume, session management)
- Context window (token counting, estimation, budget, auto-compact thresholds)
- Compaction (heuristic, LLM-based, microcompact, thinking block management)
- Pruning (observation masking, soft trim, hard clear, image/document stripping)
- Prompt caching (warming, fingerprinting, cache-safe params, TTL, completion cache)
- Memory persistence (file-based, SQLite, vector, Bleve, search index)
- Memory retrieval (keyword, semantic, RRF fusion, MMR re-ranking, adaptive depth)
- Memory extraction (post-turn, background agent, heuristic, entity linking)
- Prompt assembly (system prompt sections, dynamic boundary, JIT discovery)
- Post-compaction (file restoration, instruction refresh, diagnostics)

**ycode reference packages:**
`internal/runtime/session/`, `internal/runtime/memory/`, `internal/runtime/prompt/`, `internal/api/prompt_cache.go`, `internal/api/cache_warmer.go`, `internal/api/completion_cache.go`, `internal/storage/`

#### M-RESEARCH

1. Deep-read the tool's source for all Domain M study areas in parallel.
2. Simultaneously read ycode's Domain M packages. Record strengths and gaps.

#### M-PLAN

Write `docs/gap-analysis-<toolname>-memory.md` (same structure as Domain A).

If no gaps: document strengths, skip M-BUILD.

#### M-BUILD

Same loop as A-BUILD: implement → test → `make build` → fix → repeat until green.

#### M-COMMIT

```bash
git add <specific files only>
git commit -m "$(cat <<'EOF'
feat: add memory/context engineering gaps from <tool-name> analysis

<summary of changes by phase>
EOF
)"
```

If `--push`: `git push`

---

### Cycle 3 of 3: Domain T — Built-in Tool System & Tool Use

Gap ID prefix: `T`

**Study areas:**
- Tool registry and discovery (always-available vs deferred, ToolSearch, categories)
- Bash/shell (subprocess, in-process interpreter, process groups, signal handling)
- Bash security (validators, command substitution, sed interception, eval blocking, unicode control)
- File operations (read, write, edit with fuzzy matching, glob, grep, encoding/line-ending preservation, device blocking)
- Web (search provider, fetch with HTML-to-markdown, domain filtering, caching)
- Browser automation (container-based, MCP-based, computer use)
- Tool parallelism (concurrent vs serial partitioning, streaming executor, context modifiers)
- Tool output (distillation, truncation, disk save, large output preview)
- Permission model (modes, rules, policy engine, per-tool matching, classifier)
- LSP / code intelligence (definitions, references, hover, call hierarchy)
- Background tasks (stall watchdog, interactive prompt detection, size limits)

**ycode reference packages:**
`internal/tools/`, `internal/runtime/bash/`, `internal/runtime/fileops/`, `internal/runtime/searxng/`, `internal/runtime/browseruse/`, `internal/runtime/toolexec/`, `internal/runtime/permission/`, `internal/runtime/lsp/`, `internal/runtime/containertool/`

#### T-RESEARCH

1. Deep-read the tool's source for all Domain T study areas in parallel.
2. Simultaneously read ycode's Domain T packages. Record strengths and gaps.

#### T-PLAN

Write `docs/gap-analysis-<toolname>-tools.md` (same structure as Domain A).

If no gaps: document strengths, skip T-BUILD.

#### T-BUILD

Same loop: implement → test → `make build` → fix → repeat until green.

#### T-COMMIT

```bash
git add <specific files only>
git commit -m "$(cat <<'EOF'
feat: add tool system gaps from <tool-name> analysis

<summary of changes by phase>
EOF
)"
```

If `--push`: `git push`

---

## Step 3: Next Tool (Multi-Tool Mode)

If multiple tools were detected, move to the next tool and repeat Step 2 (all three domain cycles). Continue until all tools are processed.

---

## Step 4: Final Summary

After all tools and all domains are complete, output:

```
## Autopilot Complete

### Tools Analyzed
- <tool1> (<language>, <type>)
- <tool2> ...

### Changes by Domain
| Domain | Tool | Gaps Found | Implemented | Files | Lines |
|--------|------|------------|-------------|-------|-------|
| A — Orchestration | <tool> | N | N | N | N |
| M — Memory | <tool> | N | N | N | N |
| T — Tools | <tool> | N | N | N | N |

### Commits
- <hash> <message>

### Gap Analysis Documents
- docs/gap-analysis-<tool>-orchestration.md
- docs/gap-analysis-<tool>-memory.md
- docs/gap-analysis-<tool>-tools.md
```

---

## Rules

- **Fully autonomous.** Run to completion after receiving the path. Never ask for confirmation, approval, or permission between steps.
- **Three complete cycles per tool.** Each domain (A, M, T) gets its own full RESEARCH → PLAN → BUILD → TEST → COMMIT cycle. Complete one domain entirely before starting the next.
- **Do not fabricate.** Only document capabilities verified by reading actual source code.
- **Be honest.** Always document where ycode is ahead. If a domain has no gaps, say so and skip build/commit for that domain.
- **Never commit broken code.** `make build` must pass before every commit.
- **Scope aggressively.** Implement High and Medium priority gaps only. Low goes to Deferred.
- **priorart/ is read-only.** Never modify anything under `priorart/`.
- **Stage by name.** Never `git add -A` or `git add .`.
- **Parallel research, sequential build.** Explore the tool across domains in parallel. Build and commit one domain at a time.
- **Reusable.** Works with any path. No hardcoded project names or directories.
- **No push unless asked.** Only push if `--push` is in `{{ARGS}}`.
