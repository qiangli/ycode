# Agent OS — Reference Model & ycode Gap Analysis

> Companion docs: [`agent-os.md`](./agent-os.md) covers the **involuntary-interception axis** (`ycode wrap`); [`lighthouse.md`](./lighthouse.md) covers the **voluntary axis** (MCP beam for opt-in foreign agents). This file is the **framing-level reference** — what an Agent OS is in 2026, and where ycode stands against that bar.

## Context

This document answers two questions in one place so any agent working in this repo can reach the same conclusions:

1. **What does a "true" SOTA Agent OS need to support in 2026?** (the reference model)
2. **Where is ycode still missing capabilities?** (the gap list)

The wedge from [`strategy.md`](./strategy.md) — *local-first, single-binary, runs offline* — is load-bearing. Every gap is evaluated through it; anything that would violate the wedge is marked out-of-scope rather than treated as a missing feature.

---

## 1. Reference frame: three lenses on "Agent OS"

No single canonical definition exists in 2026. Three converging lenses, all useful:

### Lens A — AIOS kernel model (Mei et al., COLM 2025; arXiv 2403.16971)
Treats the agent runtime as a **kernel** with seven modules:

| # | Module | Job |
|---|---|---|
| K1 | LLM system-call interface | Uniform API surface every agent calls into |
| K2 | Agent scheduler | Fairness, priority, preemption across concurrent agents |
| K3 | Context manager | Snapshot/restore, compression, beam-state |
| K4 | Memory manager | Per-agent working + cross-agent sharing pools, isolation |
| K5 | Storage manager | Tiered (hot/warm/cold), retention, retrieval |
| K6 | Tool manager | Registry, contention, rate limits, scheduling |
| K7 | Access manager | Privilege groups, per-agent identity, audit |

### Lens B — 2026 agentic stack (industry surveys: mem0, aimultiple, marktechpost)
Seven horizontal layers + four cross-cutting concerns:

- L1 Perception · L2 Reasoning/Planning · L3 Action/Tools · L4 Memory · L5 Validation/Eval · L6 Multi-agent orchestration · L7 Sandbox/runtime
- Cross-cutting: **MCP** (LF-donated Dec 2025, de facto tool protocol), **hybrid retrieval** (vector + BM25 + entity + RRF), **continual learning / skill evolution**, **observability with replay**

### Lens C — ycode's own lighthouse vision (`strategy.md`, `lighthouse.md`)
A **local-first hub** for *your* matrix of agents — single binary, offline-capable, MCP-server-and-client, federated peer-to-peer rather than centralized. The Agent OS framing is implicit in autonomous-loop + lighthouse + portal but not yet stated as a public positioning goal.

The three lenses agree on the components and disagree on framing (kernel vs. layered stack vs. federated hub). All three apply.

---

## 2. SOTA Component Map — ycode coverage

Legend: ✅ table-stakes met · 🟡 partial / scaffolded · ❌ missing · ⛔ out-of-scope per wedge

| Component | ycode today | Status | Key code path |
|---|---|---|---|
| **K1 / L3 — LLM call interface** | `internal/tools/registry.go` (always-available + deferred + ToolSearch, TTL=8), MCP server mode | ✅ | `internal/tools/registry.go`, `internal/runtime/mcp/`, `cmd/ycode/mcp.go` |
| **K2 — Agent scheduler** | Collab orchestrator: N parallel workers, fork checkouts, claim labels, merger auto-merge. **No priorities, no preemption, no per-agent budget, no fairness.** Backlog priority is p1/p2/p3 only at issue layer. | 🟡 | `internal/gitserver/collab/orchestrator.go`, `internal/gitserver/queue/` |
| **K3 — Context manager** | 4-layer defense (budget-mask, prune, LLM-compact, emergency-flush), ghost snapshots, prompt-cache detection. **No beam-state snapshot/restore. No cross-agent context reuse.** | ✅ for single agent; 🟡 across agents | `internal/runtime/conversation/runtime.go` (`TurnWithRecovery`, lines 925-1167) |
| **K4 — Memory manager** | 5 layers (working/episodic/compaction/procedural/persistent), RRF+MMR retrieval, entity linking, temporal validity, dreaming/consolidation. **No per-agent isolation as a security boundary; no shared cross-agent pools as first-class.** | ✅ for retrieval; 🟡 for multi-agent | `pkg/memex/memory/memory.go`, `pkg/memex/memory/types.go`, `pkg/memex/memory/consolidation.go` |
| **K5 — Storage manager** | JSONL sessions w/ rotation, optional SQL dual-write, Bleve FTS, vector store, bonsai graph DB. **No tiered hot/warm/cold policy, no retention/eviction policy, no at-rest encryption.** | 🟡 | `internal/runtime/session/session.go`, `pkg/memex/store/`, `pkg/memex/graph/` |
| **K6 — Tool manager** | Permission modes (RO / WW / Danger), middleware chain, panic guard, deferred activation. **No tool-call concurrency caps, no per-tool rate limits, no circuit breaker on repeated failure, no declarative quotas.** | 🟡 | `internal/tools/registry.go`, `internal/runtime/permission/` |
| **K7 — Access manager** | 3-tier permission, VFS boundaries, allowed-directories list, remote permission prompter over NATS. **No per-agent identity / capability tokens; no per-skill grants; audit trail is OTEL only, not a structured permission ledger.** | 🟡 | `internal/runtime/permission/`, `internal/service/permission.go` |
| **L1 — Perception** | Repomap, treesitter, code-knowledge graph, file-access notifications. **No ambient file-watch, no git-event watch, no OS-event watch ("watch mode").** | 🟡 | `internal/runtime/repomap/`, `internal/runtime/treesitter/` |
| **L2 — Planning** | Sprint state machine (Milestone→Slice→Task), backlog reconciler. **No first-class plan-then-execute mode in user UX; no plan-critique loop; no plan-repair on failure.** | 🟡 | `internal/runtime/sprint/`, `internal/gitserver/backlog/` |
| **L5 — Validation / eval** | `make eval-*` targets (contract, smoke, behavioral, e2e), eval-gated progression in autoloop. **No public leaderboard scores yet (SWE-bench, Aider polyglot, Terminal-Bench), no continuous benchmark dashboard, no PR-time regression gate.** | 🟡 | `make eval-*` (root Makefile), `internal/runtime/autoloop/` |
| **L6 — Multi-agent orchestration** | Collab + Foreman/Worker + Loom workspaces + NATS bus + 40+ event types. **Swarm YAML agent definitions referenced in `swarm.md` but loader/schema not surfaced; no handoff patterns documented (manager-worker, debate, hierarchical, blackboard); no dynamic spawn by planner.** | 🟡 | `internal/gitserver/collab/`, `cmd/ycode/foreman.go`, `pkg/loom/`, `internal/bus/bus.go` |
| **L7 — Sandbox / runtime** | mvdan/sh interpreter with security ExecHandler, Podman pods + pools, machine management, `yc sandbox`. **No persistent sandbox sessions, no sandbox snapshot/restore, language-specific kernel images (py/node/rust) not standardized.** | ✅ for isolation; 🟡 for persistence | `internal/runtime/bash/`, `internal/container/` |
| **Cross — MCP** | Full client + server (`ycode mcp serve`), composite handler (treesitter/shell/skills/memex/ollama + http: gitea/loom/pulse), lighthouse manifest at `~/.agents/ycode/manifest.json` | ✅ | `internal/runtime/mcp/`, `cmd/ycode/mcp.go`, [`lighthouse.md`](./lighthouse.md) |
| **Cross — Hybrid retrieval** | RRF across vector + Bleve + keyword + entity, MMR diversity, temporal validity | ✅ | `pkg/memex/memory/memory.go` |
| **Cross — Continual learning** | Skill engine (FIX/DERIVED/CAPTURED), Mesh agents (Researcher/Diagnoser/Fixer/Learner), GRPO training scaffold, dreaming/consolidation. **No active training on user's repo; no user-facing skill-curation UX.** | 🟡 | `internal/runtime/skillengine/`, `internal/mesh/`, `internal/training/` |
| **Cross — Observability + replay** | OTLP gRPC/HTTP, VictoriaLogs/Jaeger/Prometheus/Alertmanager/Perses, ~25 telemetry MCP tools, OTEL spans on agent identity/tools/permissions. **No developer-facing replay UI (OTEL trace → timeline of decisions).** | ✅ for capture; ❌ for replay UX | `internal/observability/` |
| **Cross — Cost / budget** | Iteration budget in conversation, usage events on bus. **No live cost meter, no soft budget caps, no auto-router (cheap/expensive model per task).** | ❌ | `internal/runtime/conversation/budget.go` |
| **Wrap (involuntary axis)** | PATH shim + Python/Node runtime hooks + OTel attribution for foreign agents (`claude`, `opencode`, `aider`, `gemini`, `codex`) | ✅ for opencode/aider/gemini; 🟡 for claude (Bun limit), codex (Rust limit) | [`agent-os.md`](./agent-os.md), `cmd/ycode/wrap.go` |

---

## 2a. Foreign-agent coverage matrix

`ycode wrap` and the MCP-server (`ycode mcp serve`) together cover the two interception axes — involuntary and voluntary. Each foreign agent gets a different mix of coverage depending on its runtime. Be honest about what's hooked and what isn't so operators don't expect more than they have.

| Agent | Runtime | PATH-shim | Language hook | MCP (via `~/.claude.json` / equivalent) | Notes |
|---|---|---|---|---|---|
| **claude** (Anthropic Claude Code) | Bun-compiled binary | ✅ | ❌ (Bun ignores `NODE_OPTIONS=--require`) | ✅ — opt-in via `ycode init --register-foreign-agents` or repo-root `.mcp.json` | Wrap emits a one-line `[ycode wrap] claude: Bun runtime …` notice on stderr at start. **Supported integration path is MCP.** |
| **opencode** | Bun/Node CLI | ✅ | ✅ (Node `--require`) | ✅ | Full coverage on both axes. |
| **codex** (OpenAI Codex CLI) | Rust + Node helper | ✅ | ❌ (no language-level hook for Rust) | ⛔ — Codex MCP support TBD | Wrap emits a `[ycode wrap] codex: Rust runtime …` notice. |
| **aider** | Python CLI | ✅ | ✅ (`sitecustomize.py` patches `subprocess`) | ⛔ — Aider MCP support TBD | Full Python-runtime coverage; absolute-path shell-outs from Python caught by the runtime hook. |
| **gemini** (Google Gemini CLI) | Node CLI | ✅ | ✅ (Node `--require`) | ⛔ — Gemini MCP support TBD | Full Node-runtime coverage. |

**Worked example — Claude Code calling ycode tools via MCP** (post-M1):

```bash
# 1. Opt in: register ycode in ~/.claude.json (and seed L2 instructions).
ycode init --register-foreign-agents

# 2. Run a Claude Code session in any directory.
claude --print "use the ycode mcp server to build_repomap and summarize the codebase"
```

Claude's tool surface now includes `mcp__ycode-stdio__build_repomap`, `mcp__ycode-stdio__graph_summary`, `mcp__ycode-stdio__sandbox_exec`, `mcp__ycode-stdio__github_list_prs`, plus the existing `list_symbols`, `agent_shell`, `list_skills`, `memex_recall`, and `ollama_chat`. These resolve to in-process Go implementations — no shelling out to `gh`, `rg`, or `podman` from the model's tool-use loop.

For projects opening Claude Code inside the ycode source tree, the committed `.mcp.json` at the repo root advertises the same servers automatically, without any `ycode init` step.

## 3. Gaps grouped by tier

### Tier 1 — Must-have to credibly be called an Agent OS

Each blocks a core SOTA claim:

1. **Agent scheduler with priorities + preemption** (K2). Today's collab orchestrator is a worker pool, not a scheduler. Without priorities/preemption you cannot do mixed P1/P2 workloads on shared compute fairly. *Closes K2.*
2. **Per-agent identity + capability tokens + audit ledger** (K7). The remote permission prompter handles a single human-in-the-loop; multi-agent fanout needs each worker to carry its own grant set and have decisions logged in a structured ledger, not just OTEL spans. *Closes K7.*
3. **Tool quotas + rate limits + circuit breakers** (K6). Required as soon as multiple agents share the same tool pool (LLM API, MCP server, podman). Without it, one runaway worker starves the rest. *Closes K6.*
4. **Eval-as-CI**: leaderboard scores (SWE-bench Verified, Aider polyglot, Terminal-Bench) + PR-time regression gate (L5). Without external benchmarks, "Agent OS" is a marketing claim; with them, it's verifiable. Already Phase 1 of `strategy.md`. *Closes L5 + the wedge proof.*
5. **`ycode auto` user-facing autonomous loop** (cross). The substrate exists (Ralph + Sprint + Mesh + SkillEngine) but is not exposed as a CLI verb. Without it, the self-improvement story is invisible to users.

### Tier 2 — Should-have to be SOTA-credible

6. **Cross-agent shared memory pools** (K4). Foreman/Worker handoffs need a first-class shared scratchpad, not "post artifacts to Gitea and hope the next worker reads them." Today every agent reads global+project memex; nothing in between.
7. **Swarm patterns + YAML agent definitions surfaced** (L6). `swarm.md` references handoff flows but the loader/schema are not user-facing. Document and surface: manager-worker, debate, hierarchical, blackboard.
8. **Plan-then-execute as first-class user mode** (L2). Sprint state machine exists; needs a TUI/CLI surface that's not bound to the backlog reconciler. "Plan-first mode promoted to first-class workflow" is in Phase 3 of strategy.
9. **Ambient perception / watch mode** (L1). File-watch, git-event watch, on-save auto-test/auto-lint. Phase 4 differentiator.
10. **Replay UI** (cross). OTEL traces already captured; needs a developer-friendly timeline of decisions. Phase 4 differentiator.
11. **Cost meter + budget caps + auto-router** (cross). Telemetry exists on the bus; needs to land in TUI + status bar. Phase 3 of strategy.

### Tier 3 — Differentiators (would distinguish from peers if uniquely shipped)

12. **Cross-agent context reuse with prompt-cache awareness** (K3). Today every worker spawns its own fresh context. A scheduler that snapshots agent prefixes for cache reuse across siblings would meaningfully beat AIOS's own demo numbers.
13. **Sandbox snapshot/restore + persistent sandbox sessions** (L7). E2B and Modal compete on this; ycode's podman foundation supports it but `yc sandbox` doesn't expose it.
14. **Skill evolution as a user-visible loop** (cross). FIX/DERIVED/CAPTURED is wired; a `ycode skill list/diff/rollback/promote` CLI + dashboard makes it legible. Today it's a black box.
15. **Tiered storage policy** (K5). Hot session JSONL → warm SQLite → cold compressed archive with retention rules. Helps the "runs offline forever on one disk" claim age well.
16. **Speculative parallelism at tool dispatch** (cross). Phase 4 differentiator in strategy.

### Out of scope per wedge

- ⛔ Cloud-hosted scheduler / cloud control plane — violates local-first
- ⛔ External runtime dependencies (Python, Node) in default install
- ⛔ Centralized federation hub ("the matrix's hub" rather than "the hub of *your* matrix")

---

## 4. Recommended sequencing (aligned to `strategy.md`)

The strategy doc already has Phase 0–6. This doc **does not propose a new phase plan**; it slots gap-closure into existing phases.

| Phase | Slot in | Rationale |
|---|---|---|
| Phase 1 (Wedge & Proof) | #4 Eval-as-CI (leaderboards + PR regression gate) | Already the phase's exit-gate; gap-closure = phase-completion |
| Phase 1 / 2 | #5 `ycode auto` CLI surface | Substrate exists; surfacing it is a small change with large narrative payoff |
| Phase 2 (On-Ramp) | #11 Cost meter (status bar) + #15 (tiered storage hidden under the hood) | Both reduce first-week abandonment |
| Phase 3 (Daily Ergonomics) | #8 Plan-then-execute first-class, #1 Scheduler (priorities), #3 Tool quotas | All three become felt-pain as users run longer multi-agent sessions |
| Phase 3 / 4 | #2 Per-agent identity + audit, #6 Cross-agent shared memory, #7 Swarm YAML surfacing | Required for multi-agent dogfooding to scale beyond Foreman/Worker-of-one |
| Phase 4 (Differentiators) | #9 Watch mode, #10 Replay UI, #12 Cross-agent prompt-cache reuse, #13 Sandbox snapshot, #14 Skill evolution UX, #16 Speculative parallelism | "Uniquely ycode" pitches; defer until wedge proof + ergonomics are won |

Reordering inside each phase should be driven by `docs/backlog/<slug>.md` files (the existing source of truth) — this doc is the input, not a replacement.

---

## 5. Critical files for further reading

- Strategy & roadmap: [`strategy.md`](./strategy.md), [`lighthouse.md`](./lighthouse.md), [`lighthouse-roadmap.md`](./lighthouse-roadmap.md), [`autonomous-loop.md`](./autonomous-loop.md)
- Runtime: `internal/runtime/conversation/runtime.go` (`Turn`, `TurnWithRecovery`), `internal/runtime/conversation/budget.go`
- Tools & permissions: `internal/tools/registry.go`, `internal/runtime/permission/mode.go`, `internal/service/permission.go`
- Multi-agent: `internal/gitserver/collab/orchestrator.go`, `internal/gitserver/queue/`, `cmd/ycode/foreman.go`, `pkg/loom/`
- Memory: `pkg/memex/memory/memory.go`, `pkg/memex/memory/types.go`, `pkg/memex/memory/consolidation.go`
- Sandbox: `internal/runtime/bash/interpreter.go`, `internal/container/`
- Observability: `internal/observability/`
- Bus / events: `internal/bus/bus.go`
- Autonomous loop substrate: `internal/runtime/autoloop/`, `internal/runtime/ralph/`, `internal/runtime/skillengine/`, `internal/runtime/sprint/`, `internal/mesh/`, `internal/training/`

## 6. Verification

How to confirm the gap analysis is still faithful (read-only checks any agent can run):

- `rg -n "FIFO|priority|preempt|fairness" internal/gitserver/collab/ internal/gitserver/queue/` — confirms no scheduler primitives beyond label priority
- `rg -n "rate.?limit|quota|circuit.?breaker" internal/tools/ internal/runtime/` — confirms tool-mgr quota gap
- `rg -n "capability.?token|agent.?identity" internal/runtime/permission/ internal/service/` — confirms K7 gap
- `rg -n "shared.?(pool|memory)|cross.?agent" pkg/memex/` — confirms K4 multi-agent gap
- `ls cmd/ycode/auto*.go 2>/dev/null` and `rg -n "ycode auto" cmd/` — confirms whether `ycode auto` CLI is exposed yet
- `make eval-smoke` / `make eval-behavioral` — confirms current evals run; check for SWE-bench/Aider/TB harness presence with `rg -n "swe.?bench|aider.?polyglot|terminal.?bench" .`
- `rg -n "type Schedule|Scheduler" internal/` — confirms no real scheduler type today

Any of the Tier-1/2/3 items can become a `docs/backlog/<slug>.md` task in its own right; the Foreman/Worker loop is the correct execution surface.

## 7. Open framing questions

These remain unresolved at the strategy level and affect prioritization:

1. **Public framing**: should ycode adopt the **"Agent OS"** label publicly, or keep it as an internal architectural mental model while the public framing stays "local-first AI coding agent" (Phase 1 wedge)? The gap list applies either way; priority changes.
2. **Tier-1 packaging**: of the five Tier-1 items, batch into one milestone or land independently as ready?
3. **Multi-tenant scope**: K7 (per-agent identity + audit) is much heavier if ycode ever runs N humans through one daemon. Single-user-multi-agent (current) or multi-user-multi-agent (future portal)?
