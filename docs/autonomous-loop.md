# Autonomous Self-Improving Agent Loop

Implementation of the complete RESEARCH → PLAN → BUILD → EVALUATE → LEARN loop for ycode. This document covers the architecture, components, and patterns adopted from 9 prior-art projects.

## Overview

The autonomous loop enables ycode to:
1. **Self-improve** — run the loop on its own codebase to add features and fix gaps
2. **Develop autonomously** — run the same loop on third-party repositories
3. **Learn from experience** — evolve skills, persist knowledge, and improve over time

The implementation spans 6 integrated subsystems built across 4 new packages and modifications to 3 existing packages.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Autonomous Loop (autoloop/)               │
│  RESEARCH → PLAN → BUILD → EVALUATE → LEARN                │
├─────────────────────────────────────────────────────────────┤
│  Sprint Runner (sprint/)     │  Skill Engine (skillengine/) │
│  Milestone → Slice → Task   │  Auto-select, Track, Evolve  │
│  State machine + Review      │  FIX / DERIVED / CAPTURED    │
├──────────────────────────────┼──────────────────────────────┤
│  Ralph Loop (ralph/)         │  Mesh Agents (mesh/)         │
│  Step → Check → Commit      │  Diagnose → Fix → Learn      │
│  Full runtime per iteration  │  Background observe + act    │
├──────────────────────────────┼──────────────────────────────┤
│  Conversation Runtime        │  Training Pipeline           │
│  50+ tools, memory, session  │  Trajectories → GRPO → Swap │
└─────────────────────────────────────────────────────────────┘
```

## Components

### 1. Ralph Loop with Full Runtime (`internal/runtime/ralph/`)

The foundation. Each iteration creates a fresh conversation runtime with full tool access (bash, file ops, search, git, memory, web, skills) instead of raw LLM calls.

**Key files:**
- `runtime_step.go` — `NewRuntimeStepFunc` creates a `StepFunc` backed by `conversation.Runtime.Turn()`. Each invocation spawns a fresh session to prevent context bloat.
- `eval_check.go` — `NewBashCheckFunc` for shell-based verification, `NewGitCommitFunc` that stages files by name (never `git add -A`).
- `ralph.go` — Controller with `IterationCallback` for post-iteration hooks (memory persistence, metrics).

**Pattern adopted:** Fresh context per iteration (from Ralph/GSD-2 priorart). State persists via `ralph.State`, `ProgressLog`, and `PRD` — the conversation does not.

**CLI:** `ycode ralph "add health check" --check "go test ./..." --commit --max-iterations 5`

### 2. Mesh Agent Wiring (`internal/mesh/`)

Background agents that observe, diagnose, fix, and learn. Previously scaffolded but disconnected — now wired to real implementations.

**Key files:**
- `wire.go` — `WireCallbacks` injects real implementations into mesh agents:
  - **Fixer** → `selfheal.Healer.AttemptHealing()` for AI-driven error remediation
  - **Researcher** → SearXNG search + memory persistence as reference memories
  - **Learner** → `memory.Manager.Save()` with type-aware categorization (procedural/episodic/reference)
  - **Trainer** → No-op placeholder (wired in training pipeline)
- `tracing.go` — `Unwrap()` method on `TracedAgent` for callback injection through the tracing wrapper.

**Pattern adopted:** Bus-driven coordination (from all priorart). Agents communicate via events, not imports. Enable/disable independently without code changes.

### 3. Swarm Orchestration Tool (`internal/tools/swarm_tool.go`)

Registers a `swarm_run` deferred tool that delegates work to named agents defined in `agents/*.yaml` files. Uses the existing `swarm.Orchestrator` with handoff detection and cycle prevention.

**Pattern adopted:** DAG workflow execution (from Archon). YAML-defined agents with flow types (sequence/chain/parallel/loop/fallback/dag).

### 4. Skill Evolution Engine (`internal/runtime/skillengine/`)

Self-evolving skills that auto-select based on context, track performance, and improve through evolution.

**Key files:**
- `spec.go` — `SkillSpec` with trigger patterns (regex + keywords), performance stats (`SkillStats`), version lineage, and `MatchScore()` for ranking.
- `registry.go` — `Registry` with `FindBestMatch()` (auto-selection), `RecordOutcome()` (stats tracking), `ApplyDecay()` (5%/week decay), and JSON file persistence.
- `engine.go` — `Engine` wraps registry with `SelectSkill()` (called before each turn) and `RecordResult()` (called after, triggers degradation detection).
- `evolution.go` — `Evolver` with three modes:
  - **FIX** — Repair broken skill with updated instructions. Creates new version with parent pointer.
  - **DERIVED** — Specialize from a successful deviation. New skill inherits parent triggers + adds specialization keywords.
  - **CAPTURED** — Extract new skill from recurring procedural memory patterns.
  - **Rollback** — `CheckForRollback()` reverts to parent if evolved version degrades below threshold.

**Patterns adopted:**
- Skill evolution (FIX/DERIVED/CAPTURED) from OpenSpace — demonstrated 4.2x improvement via self-evolving skills
- Context-triggered activation from Superpowers — skills auto-select without slash commands
- Performance decay from GStack — 5%/week prevents stale skills from dominating
- Version DAG from OpenSpace — full lineage tracking with automatic rollback

### 5. Sprint-Structured Workflow (`internal/runtime/sprint/`)

Structured multi-task autonomous development with Milestone → Slice → Task hierarchy.

**Key files:**
- `state.go` — `SprintState` with state machine (`Plan → Execute → Complete → Reassess → ValidateMilestone → Done`), JSON persistence, task/slice navigation, and budget tracking.
- `runner.go` — `Runner` executes the state machine. Each leaf task gets a fresh Ralph iteration with pre-inlined context (description, acceptance criteria, review feedback).
- `planner.go` — `DecomposeGoal()` and `DecomposeWithCriteria()` for task hierarchy creation.

**Patterns adopted:**
- Milestone/Slice/Task hierarchy from GSD-2 — each task fits one context window
- State machine from GSD-2 — Plan → Execute → Complete → Reassess → Validate
- Sprint cycle from GStack — Think → Plan → Build → Review → Test → Ship → Reflect
- Idea-to-PR pipeline from Archon — plan → implement → validate → PR → review

### 6. Autonomous Loop (`internal/runtime/autoloop/`)

The complete RESEARCH → PLAN → BUILD → EVALUATE → LEARN cycle.

**Key file:** `loop.go` — `Loop` with pluggable `Callbacks`:
- **Research** — Web search (SearXNG) + memory search + cross-session FTS + gap analysis
- **Plan** — Decompose gaps into prioritized task list
- **Build** — Execute tasks via sprint runner with skill auto-selection
- **Evaluate** — Run eval suite, compare against pre-implementation baseline
- **Learn** — Skill engine processes memories (CAPTURED/FIX evolution), Dreamer consolidates

Features: OTEL instrumentation, stagnation detection (configurable limit), `FormatSummary()` for reporting.

**CLI (future):** `ycode auto --goal "improve test coverage to 80%" --budget 10000 --max-iterations 5`

### 7. Training Pipeline (`internal/training/collector.go`)

Trajectory collection and wiring for the self-improvement loop.

- `Collector` — Gathers `ScoredTrajectory` from agent sessions, persists as JSONL
- `WireCallbacks` — Connects collector → trainer → evaluator → model swapper for `SelfImproveLoop`

**Patterns adopted from priorart/nanochat and priorart/autoresearch (both MIT licensed):**
- GRPO (Group Relative Policy Optimization) for RL training on math/coding tasks
- Time-budget training (fixed wall-clock) from autoresearch
- Single DEPTH parameter drives all model dimensions from nanochat
- JSONL IPC protocol for Go↔Python training bridge

## SOTA Patterns Adopted

| Pattern | Source Project | ycode Implementation |
|---------|---------------|---------------------|
| Fresh context per unit | GSD-2, Ralph | Ralph `FreshContext=true` + Sprint runner |
| Skill evolution (FIX/DERIVED/CAPTURED) | OpenSpace | Skill engine with version DAG |
| Context-triggered skill activation | Superpowers | Regex + keyword trigger matching |
| Two-stage review (spec + quality) | Superpowers | Sprint review (planned) |
| Milestone/Slice/Task hierarchy | GSD-2 | Sprint planner using SprintState |
| Append-only progress log | Ralph | ProgressLog + procedural memories |
| DAG workflow execution | Archon | Existing agentdef.DAGWorkflow |
| Learning from experience | Hermes | Mesh Learner → memory → skill CAPTURED |
| Idea-to-PR pipeline | Archon | Sprint + Ralph + git tools |
| Sprint cycle | GStack | Think→Plan→Build→Review→Test→Ship→Reflect |
| Bus-driven coordination | All 9 projects | Event bus for mesh agent communication |
| Eval-gated progression | GSD-2 | Autoloop evaluates before/after each cycle |
| GRPO RL training | nanochat | Training pipeline with Python subprocess |
| Time-budget training | autoresearch | Timeout option on autonomous loops |
| Disk-first persistence | Ralph, GSD-2 | JSON state files + JSONL sessions |

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| Fresh context per iteration | Prevents context bloat; each task gets full token budget |
| Bus-driven mesh coordination | Agents communicate via events; enable/disable independently |
| Skills as files, not code | Agent can evolve its own skills by writing JSON without recompilation |
| Eval-gated progression | No milestone completes without eval confirmation of no regression |
| Pluggable callbacks | Each loop phase is a function; swap implementations without touching orchestration |
| Copy-on-persist for stats | Avoid data races when persisting skill stats from goroutines |
| Auto-approve permissions | Ralph runs non-interactively; tools auto-approved with logging |
| Stage files by name | `NewGitCommitFunc` follows project convention of never using `git add -A` |

## Testing

Each package has comprehensive tests:

| Package | Tests | Coverage |
|---------|-------|----------|
| `ralph/` | 35 tests | Controller lifecycle, iteration callback, PRD tracking, bash check, git commit |
| `skillengine/` | 10 tests | Match score, registry CRUD, outcome recording, decay, all 3 evolution modes, rollback, engine select |
| `sprint/` | 6 tests | State lifecycle, save/load, task navigation, budget, decompose |
| `autoloop/` | 5 tests | Basic cycle, stagnation detection, context cancellation, format summary |
| `mesh/` | 3 tests | Fixer wiring, researcher wiring, mesh status |

All tests pass with `-race` flag. `make build` passes all 6 steps (tidy → fmt → vet → compile → test → verify).

## Future Work

1. **CLI commands** — `ycode sprint`, `ycode auto`, `ycode skill` subcommands for direct invocation
2. **LLM-powered planning** — Replace `DecomposeGoal()` with LLM-based task decomposition
3. **Two-stage review** — Implement spec compliance + code quality review in sprint runner
4. **Skill instruction generation** — LLM generates skill instructions from procedural patterns
5. **Training script** — Python GRPO script (`scripts/grpo_train.py`) for local model training via Ollama
6. **Mesh integration in conversation runtime** — Auto-start mesh in `NewRuntime()` when enabled in config
7. **Web research in autoloop** — Wire SearXNG containerized search into the research callback
8. **Leaderboard submissions** — SWE-bench, Aider polyglot, Terminal-Bench once eval framework is operational

## References

- [docs/evaluation.md](./evaluation.md) — Grand vision, eval framework design, Phase A/B loop
- [docs/architecture.md](./architecture.md) — Full system architecture and design decisions
- [docs/memory.md](./memory.md) — Five-layer memory system, search backends, temporal decay
- [docs/swarm.md](./swarm.md) — Agent orchestration, YAML definitions, handoff flows
- [docs/mesh.md](./mesh.md) — Self-improving background agents
- `priorart/nanochat/` — GRPO RL training, MuonAdamW optimizer (MIT)
- `priorart/autoresearch/` — Time-budget training, single-file agent-modifiable training (MIT)
- `priorart/openspace/` — Self-evolving skill engine with FIX/DERIVED/CAPTURED
- `priorart/gsd-2/` — Milestone/Slice/Task hierarchy with state machine
- `priorart/superpowers/` — Context-triggered skills, two-stage review
