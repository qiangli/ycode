# Evaluation Framework for ycode

## Grand Vision: The Self-Improving Agent

ycode's ambition is to become the **leader of AI coding agents** and the **best-in-class agentic assistant**. The evaluation framework is not just a testing tool — it is the foundation of a **self-improving feedback loop** that drives ycode toward that goal.

### The Flywheel

```
    ┌─────────────────────────────────────────────────────────┐
    │                                                         │
    │   ┌─────────┐    ┌──────────┐    ┌─────────┐          │
    │   │  TRACK  │───▶│ RESEARCH │───▶│  PLAN   │          │
    │   │         │    │          │    │         │          │
    │   │ Trending │    │ Analyze  │    │ Design  │          │
    │   │ repos &  │    │ priorart │    │ changes │          │
    │   │ agents   │    │ patterns │    │         │          │
    │   └─────────┘    └──────────┘    └────┬────┘          │
    │        ▲                              │               │
    │        │                              ▼               │
    │   ┌────┴────┐    ┌──────────┐    ┌─────────┐          │
    │   │  ADAPT  │◀───│ EVALUATE │◀───│  BUILD  │          │
    │   │         │    │          │    │         │          │
    │   │ Adjust  │    │ Score vs │    │ Implement│          │
    │   │ approach│    │ baseline │    │ features │          │
    │   │ & repeat│    │ & peers  │    │         │          │
    │   └─────────┘    └──────────┘    └─────────┘          │
    │                                                         │
    │            TRACK → RESEARCH → PLAN → BUILD              │
    │                  → EVALUATE → ADAPT → REPEAT            │
    └─────────────────────────────────────────────────────────┘
```

### Phase A: Human-Driven Learning (this plan)

**Current approach** — human + AI collaboration:

1. **Track** daily [github.com/trending](https://github.com/trending) repositories and developers, Hacker News, arxiv, and other sources to discover the best up-to-date capabilities, features, and emerging patterns in agentic tools.

2. **Research** — add promising projects to `priorart/` as git submodules. Study their architectures, eval frameworks, tool implementations, and prompt engineering. ycode already tracks 9+ prior art projects (Aider, Cline, Codex, Gemini CLI, OpenClaw, OpenCode, OpenHands, Continue, Kimi CLI).

3. **Plan** — design how to incorporate the best ideas into ycode, adapted to its pure-Go, single-binary, self-contained architecture.

4. **Build** — implement the features, vendor external code into `external/` with attribution where needed.

5. **Evaluate** — run the eval framework (this plan) to measure improvement. Compare composite scores, pass@k, tool accuracy, cost efficiency against the baseline.

6. **Adapt** — if scores improve, ship it. If they regress, diagnose and fix. Adjust the approach based on what the numbers say. Repeat.

### Phase B: Autonomous Self-Improvement (future plan)

**Future vision** — ycode improves itself via an autonomous agent loop:

An **auto-evolution agent** runs on a schedule (daily/weekly) using ycode's built-in `CronRegistry` and `loop.Scheduler`:

1. **Track** — agent scrapes GitHub trending, monitors starred repos, fetches release notes from tracked projects, identifies new capabilities appearing in competitor agents.

2. **Research** — agent adds new submodules to `priorart/`, reads their code, generates gap analyses (like the existing `docs/research/` reports), and identifies features ycode is missing.

3. **Plan** — agent creates implementation plans, prioritized by expected eval score improvement, using ycode's plan mode.

4. **Build** — agent implements changes in isolated git branches (via embedded Gitea), runs `make build` to validate.

5. **Evaluate** — agent runs the full eval suite against the new branch. Compares composite score, pass@k, leaderboard-specific metrics against `main`.

6. **Adapt** — if eval scores improve beyond threshold (e.g., composite +5%), agent creates a PR for human review. If scores regress, agent discards the branch and logs what it learned. Adjusts research priorities based on which areas showed the most improvement potential.

This is a **separate plan** to be designed after the eval framework is operational — the eval framework is the prerequisite that makes autonomous self-improvement measurable and safe.

### The Competitive Moat

Most AI coding tools are thin wrappers around frontier models. ycode's moat is the **self-contained infrastructure** (Ollama, Podman, git server, OTEL, 50 tools, 5-layer memory) combined with the **self-improving feedback loop**. The eval framework makes improvement measurable. The learning loop makes it continuous. The autonomous agent makes it scalable.

---

## Context

ycode is a self-contained, air-gap-ready agentic tool with embedded Ollama, Podman containers, git server, OTEL observability, cron scheduling, and SQLite/Bleve storage — all in a single binary. It has 50 tools, 5-layer memory, multi-provider LLM support, and comprehensive test coverage, but **no evaluation framework** to detect regressions in agentic behavior across releases.

This plan designs an eval system that is **fully self-contained within the ycode ecosystem** — no external CI/CD, no GitHub Actions. Scheduled eval runs use ycode's built-in cron. Results are stored in SQLite and visualized via embedded Perses/Prometheus dashboards. Local Ollama is the default provider; external LLM access is opt-in.

Eval tooling does **not** need to be in Go — it can use any permissive-licensed language (Python, TypeScript, etc.). Eval tools can run **directly on the host** with installed dependencies (e.g. `pip install inspect-ai deepeval`), or optionally in Podman containers for sandboxed E2E tasks. Only the orchestration layer and Go contract tests live in the ycode binary.

---

## Numerical Scoring System

### Core Metrics (all produce 0.0–1.0 scores)

**1. pass@k** — "Can the agent solve this?" (industry standard from HumanEval/Codex)
```
pass@k = 1 - C(n-c, k) / C(n, k)
```
- n = total trials, c = passing trials, k = samples evaluated
- Range: [0.0, 1.0]. Higher = better.
- Used by: HumanEval, SWE-bench, Cline

**2. pass^k** — "How reliable is the agent?" (consistency measure)
```
pass^k = C(c, k) / C(n, k)
```
- Probability ALL k trials succeed. Measures reliability, not just capability.
- Range: [0.0, 1.0]. Higher = more consistent.
- Used by: Cline evals

**3. Flakiness** — "How unpredictable is this scenario?" (binary entropy)
```
flakiness = -p·log₂(p) - (1-p)·log₂(1-p)    where p = pass_rate
```
- Range: [0.0, 1.0]. 0 = deterministic, 1 = maximum variance.
- Used by: Cline, Gemini CLI

**4. Edit Precision** — "How surgically does the agent edit code?"
```
edit_precision = 1 - (unintended_changes / total_lines_in_file)
```
- Range: [0.0, 1.0]. 1.0 = only target lines changed.

**5. Tool Accuracy** — "Does the agent pick the right tools?"
```
tool_accuracy = |expected_tools ∩ actual_tools| / |expected_tools ∪ actual_tools|
```
- Jaccard similarity of tool-call sets.
- Range: [0.0, 1.0].

**6. Trajectory Similarity** — "Does the agent follow the expected path?"
```
trajectory_score = LCS(expected_tool_sequence, actual_tool_sequence) / max(len(expected), len(actual))
```
- Longest Common Subsequence normalized. Captures ordering.
- Range: [0.0, 1.0].

**7. Cost Efficiency** — "How many tokens per successful task?"
```
cost_efficiency = 1 / (total_tokens / successful_tasks)    (normalized to baseline)
```

### Composite Score (single number per eval run)

```
eval_score = w₁·pass@k + w₂·pass^k + w₃·(1-flakiness) + w₄·tool_accuracy + w₅·cost_efficiency
```
Default weights: w₁=0.35, w₂=0.25, w₃=0.15, w₄=0.15, w₅=0.10

Range: [0.0, 1.0] — displayed as 0–100 points. Easy headline: "ycode v2.1 scores 78/100 (up from 74)."

### Regression Detection (version A vs B)

**Percentage change** per metric:
```
delta = ((score_B - score_A) / score_A) × 100%
```

**Regression severity** thresholds:
| Severity | Delta | Action |
|----------|-------|--------|
| None | < 5% drop | Pass |
| Warning | 5–15% drop | Log, continue |
| Regression | > 15% drop | Block release |

**Statistical significance** via Wilson score interval (used by Aider):
```
wilson_lower = (p + z²/2n - z·√(p(1-p)/n + z²/4n²)) / (1 + z²/n)
```
- z = 1.96 for 95% confidence
- If wilson_lower(B) < wilson_lower(A), regression is statistically significant

---

## Architecture

### Self-Contained Infrastructure Map

| Need | ycode Component | File Path |
|------|----------------|-----------|
| Scheduling eval runs | `loop.Scheduler` + `team.CronRegistry` | `internal/runtime/loop/`, `internal/runtime/team/` |
| Local LLM inference | `inference.OllamaComponent` | `internal/inference/` |
| Sandboxed workspaces | `container.Engine` (Podman) | `internal/container/` |
| Test git repos | `gitserver.Client` (Gitea) | `internal/gitserver/` |
| Result storage | `storage.SQLStore` (SQLite) + `storage.SearchIndex` (Bleve) | `internal/storage/` |
| Metrics & dashboards | `observability.StackManager` (Prometheus + Perses) | `internal/observability/` |
| Event publishing | `bus.Bus` | `internal/bus/` |
| Parallel execution | `taskqueue.Executor` | `internal/runtime/taskqueue/` |
| HTTP API for results | `server.Server` | `internal/server/` |

### Directory Structure

```
internal/eval/                       # Go orchestration layer
  eval.go                            # Scenario, RunResult, Tier, Policy types
  assertions.go                      # Built-in assertion helpers
  scoring.go                         # pass@k, pass^k, flakiness, composite score formulas
  report.go                          # SQLite persistence, regression detection, Wilson intervals
  judge.go                           # LLM-as-judge interface (calls local Ollama or external)
  schedule.go                        # Integration with loop.Scheduler / team.CronRegistry
  metrics.go                         # OTEL metric emitters (eval.pass_rate, eval.score, etc.)

  contract/                          # Tier 1: No LLM, deterministic (Go tests)
    tool_dispatch_test.go
    permission_test.go
    session_test.go
    prompt_assembly_test.go
    tool_schema_test.go

  smoke/                             # Tier 2: Real LLM, fast, pass@k (Go tests)
    smoke_test.go
    scenarios.go

  behavioral/                        # Tier 3: Multi-step trajectory analysis (Go tests)
    behavioral_test.go
    trajectory.go

  e2e/                               # Tier 4: Full coding tasks (containerized, any language)
    e2e_test.go                      # Go orchestrator: spins up Podman containers
    workspace.go                     # Container + git repo setup/teardown
    tasks/                           # Task definitions (YAML + seed files)
      fix-go-test/
      implement-function/
      refactor-extract/
      git-workflow/
      multi-file-handler/

  harness/                           # External eval runners (permissive-licensed)
    requirements.txt                 # inspect-ai, swebench, deepeval, etc.
    setup.sh                         # Install dependencies on host (pip install, etc.)
    Containerfile                    # Optional: containerized image for sandboxed runs
    run_swebench.py                  # SWE-bench subset runner
    run_inspect.py                   # Inspect AI eval runner
    run_aider_bench.py               # Aider polyglot benchmark runner
    scoring.py                       # Shared scoring utilities
```

### External Eval Tools (permissive-licensed)

| Tool | License | Purpose | Runs On |
|------|---------|---------|---------|
| [Inspect AI](https://github.com/UKGovernmentBEIS/inspect_ai) | MIT | General agent eval framework, scorers, sandboxing | Host or Podman |
| [SWE-bench harness](https://github.com/princeton-nlp/SWE-bench) | MIT | Real GitHub issue resolution | Host or Podman |
| [Aider benchmark](https://github.com/aider-ai/aider) | Apache-2.0 | Polyglot Exercism exercises (225+ problems) | Host or Podman |
| [DeepEval](https://github.com/confident-ai/deepeval) | Apache-2.0 | LLM-as-judge, tool correctness metrics | Host |
| [go-llm eval](https://github.com/natexcvi/go-llm) | MIT | Go-native agent evaluation | Host (native Go) |

Default: install on host via `pip install` / `go install`. Podman containers available for isolation when running untrusted agent-generated code (E2E tasks). ycode orchestrates, collects results, stores in SQLite.

---

## Evaluation Tiers

### Tier 1: Contract (no LLM, deterministic, always runs)
**Language**: Go. **Build tag**: none. **Time**: <10s.

Tests agent machinery without any LLM call. Uses `internal/testutil/mockapi`.
- Tool dispatch correctness
- Permission enforcement (ReadOnly rejects writes)
- Session save/load round-trip
- Compaction trigger thresholds
- System prompt assembly (correct sections per mode)
- Tool schema validation (all 50 tools have valid JSON Schema)

**Scoring**: Binary pass/fail per test. Aggregate: pass_rate = passed/total.

### Tier 2: Smoke (real LLM, fast, pass@k)
**Language**: Go. **Build tag**: `eval`. **Time**: <3 min. **Trials**: 3.

5-8 focused scenarios against local Ollama (or opt-in external provider).

| # | Scenario | Assertion | Numerical Metric |
|---|----------|-----------|-----------------|
| 1 | Arithmetic: "What is 2+2?" | Response contains "4" | pass@3 |
| 2 | Tool selection: "Read file X" | read_file called with correct path | tool_accuracy |
| 3 | File creation: "Create hello.go" | File exists, `go build` succeeds | pass@3 |
| 4 | Edit precision: Fix seeded bug | Only target line changed | edit_precision |
| 5 | Search: "Find TODO files" | grep_search called, correct files | tool_accuracy + pass@3 |

### Tier 3: Behavioral (trajectory analysis, scheduled)
**Language**: Go. **Build tag**: `eval_behavioral`. **Time**: <30 min. **Trials**: 3.

| # | Scenario | Trajectory Assertion | Numerical Metric |
|---|----------|---------------------|-----------------|
| 1 | Multi-step creation | write_file×2 → bash(go build) → success | trajectory_score |
| 2 | Error recovery | read → edit → build(fail) → edit → build(pass) | pass@3 + trajectory_score |
| 3 | Memory persistence | Session 1: memory_save; Session 2: memory_recall | pass@3 |
| 4 | Permission adherence | ReadOnly → no write tools called | tool_accuracy (inverted) |
| 5 | Subagent delegation | Agent tool spawned with correct type | tool_accuracy |
| 6 | Context compaction | Compaction triggers, coherence preserved | pass@3 + LLM-judge score |
| 7 | Frugal reading | read_file uses offset/limit on large file | tool_accuracy |

### Tier 4: E2E (full coding tasks, scheduled)
**Language**: Any (Python/Go). **Build tag**: `eval_e2e`. **Time**: <45 min.

Tasks run on host (temp directories) or in Podman containers for isolation. Seeded git repos via embedded Gitea.

| # | Task | Verification | Metrics |
|---|------|-------------|---------|
| 1 | Fix failing Go test | `go test` passes | pass@3, cost_efficiency |
| 2 | Implement function from signature | `go test` passes | pass@3, edit_precision |
| 3 | Refactor: extract function | Compiles + behavior preserved | pass@3, trajectory_score |
| 4 | Git workflow: branch + commit | Branch exists, commit message matches style | pass@3 |
| 5 | Multi-file REST handler | `go test` + `curl` verification | pass@3, cost_efficiency |

Optional (containerized external harnesses):
- SWE-bench Pro subset (10 tasks)
- Aider polyglot (subset of 225 Exercism exercises)
- Inspect AI custom scenarios

---

## Scheduling (self-contained, no external CI)

Uses ycode's built-in `loop.Scheduler` and `team.CronRegistry`:

```
ycode eval contract                    # Run on demand
ycode eval smoke                       # Run on demand
ycode eval run                         # Full eval run (all tiers)
ycode eval schedule --interval 24h     # Schedule nightly via CronRegistry
ycode eval report                      # Show latest results + regression analysis
ycode eval history                     # Trend over last N runs
ycode eval compare v2.0 v2.1           # Compare two versions head-to-head
```

**Scheduling implementation**: New `eval` subcommand in cobra CLI. Uses `team.CronRegistry` to persist cron entries. Loop state (iteration count, last scores, trend) persists via `loop.IterationContext`.

**Default schedule**:
- Contract: every build (part of `make build`)
- Smoke: daily at 3 AM (local Ollama)
- Behavioral: daily at 3 AM (after smoke)
- E2E: weekly Sunday 6 AM

---

## Result Storage & Reporting

### SQLite Schema (via `storage.SQLStore`)

```sql
CREATE TABLE eval_runs (
    id          TEXT PRIMARY KEY,
    version     TEXT NOT NULL,        -- git SHA
    timestamp   DATETIME NOT NULL,
    provider    TEXT NOT NULL,        -- "ollama", "anthropic", "openai"
    model       TEXT NOT NULL,
    tier        TEXT NOT NULL,        -- "contract", "smoke", "behavioral", "e2e"
    composite_score REAL,            -- 0.0-1.0 composite
    pass_at_k   REAL,               -- aggregate pass@k
    pass_pow_k  REAL,               -- aggregate pass^k
    flakiness   REAL,               -- aggregate flakiness
    total_tokens INTEGER,
    total_cost_usd REAL,
    duration_ms INTEGER
);

CREATE TABLE eval_scenarios (
    id          TEXT PRIMARY KEY,
    run_id      TEXT REFERENCES eval_runs(id),
    name        TEXT NOT NULL,
    tier        TEXT NOT NULL,
    trial       INTEGER NOT NULL,
    passed      BOOLEAN NOT NULL,
    pass_at_k   REAL,
    edit_precision REAL,
    tool_accuracy REAL,
    trajectory_score REAL,
    input_tokens INTEGER,
    output_tokens INTEGER,
    cost_usd    REAL,
    latency_ms  INTEGER,
    turns       INTEGER,
    tool_calls  TEXT,               -- JSON array of tool names
    error       TEXT
);
```

### OTEL Metrics (via `observability.StackManager`)

Emit as Prometheus metrics, queryable via PromQL, visualizable in Perses:

```
ycode_eval_composite_score{version, provider, model, tier}     gauge  0.0-1.0
ycode_eval_pass_rate{version, provider, model, tier, scenario} gauge  0.0-1.0
ycode_eval_flakiness{version, provider, model, tier, scenario} gauge  0.0-1.0
ycode_eval_cost_usd{version, provider, model, tier}            counter
ycode_eval_latency_ms{version, provider, model, tier}          histogram
ycode_eval_tool_accuracy{version, provider, model, scenario}   gauge  0.0-1.0
ycode_eval_trajectory_score{version, provider, model, scenario} gauge  0.0-1.0
```

### Regression Report (human-readable)

```
ycode eval report
```

Output:
```
Eval Report: v2.1 (abc123) vs v2.0 (def456)
============================================
Composite Score:  78/100 → 82/100  (+5.1%)  ✓
pass@3:           0.87   → 0.91    (+4.6%)  ✓
pass^3:           0.72   → 0.78    (+8.3%)  ✓
Flakiness:        0.18   → 0.12    (-33.3%) ✓ (lower is better)
Tool Accuracy:    0.91   → 0.93    (+2.2%)  ✓
Cost (tokens/task): 4,200 → 3,800  (-9.5%)  ✓

Regressions (>15% drop):
  None

Warnings (5-15% drop):
  memory_persistence: pass@3 dropped 0.89 → 0.78 (-12.4%)

Improvements:
  edit_precision: 0.82 → 0.94 (+14.6%)
  error_recovery: trajectory_score 0.71 → 0.85 (+19.7%)
```

### Perses Dashboard

Auto-provisioned dashboard with panels:
- Composite score trend (line chart over time)
- pass@k by scenario (heatmap)
- Flakiness by scenario (bar chart)
- Cost per task trend
- Provider comparison (grouped bar)

---

## Policy System

```go
type Policy int
const (
    AlwaysPasses Policy = iota  // Must pass every trial. Part of build gate.
    UsuallyPasses               // Must pass threshold. Scheduled runs only.
)
```

- New scenarios start as `UsuallyPasses`
- Promotion to `AlwaysPasses` after 7 consecutive runs with pass@3 = 1.0
- Auto-demotion on failure (logged to eval_runs table)
- `AlwaysPasses` contract + smoke scenarios run as part of `make build`

---

## Local Model Guidance (Ollama)

### Are local models sufficient for evals?

**Yes, for Tiers 1-3.** Local models are sufficient for contract tests (no LLM needed), smoke tests (basic tool calling), and most behavioral evals. For Tier 4 E2E and leaderboard submissions, external frontier models are recommended.

### Minimum Model Sizes by Use Case

| Use Case | Minimum | Recommended | VRAM | Notes |
|----------|---------|-------------|------|-------|
| Tool calling (agent under test) | 7B | 14B | 6-12 GB | Quality cliff between 3B and 7B |
| LLM-as-judge | 7B | 14B+ | 6-12 GB | Flow Judge 3.8B viable for binary pass/fail |
| Code generation (eval tasks) | 7B | 32B | 6-24 GB | 32B hits 92.7% HumanEval |
| Leaderboard submissions | N/A | Frontier | N/A | Use Anthropic/OpenAI for competitive scores |

### Recommended Local Models

| Model | Params | HumanEval | VRAM (Q4) | Best For |
|-------|--------|-----------|-----------|----------|
| **Qwen2.5-Coder 7B** | 7B | ~75% | 6 GB | Minimum viable: smoke + behavioral evals |
| **Qwen2.5-Coder 14B** | 14B | ~84% | 10-12 GB | Sweet spot: good quality, reasonable resources |
| **Qwen2.5-Coder 32B** | 32B | 92.7% | 22-24 GB | Best local quality: approaches frontier models |
| **DeepSeek-Coder 6.7B** | 6.7B | ~78% | 7-8 GB | Alternative 7B with strong coding |

### Dual-Model Setup (agent + judge on same machine)

For simultaneous agent execution and LLM-as-judge scoring:
- **16 GB VRAM**: Qwen2.5-Coder 7B (agent) + Flow Judge 3.8B (judge) = ~13 GB
- **24 GB VRAM**: Qwen2.5-Coder 14B (agent) + Qwen2.5-Coder 7B (judge) = ~18 GB
- **48 GB VRAM**: Qwen2.5-Coder 32B (agent+judge) = ~24 GB

### Quality Cliff

Below 3B parameters, tool calling accuracy degrades significantly and evals become unreliable. The 3B→7B jump is the largest quality improvement per parameter increase. For meaningful agentic evals, **7B is the practical minimum**.

---

## What Claude Code Uses (Reference)

Anthropic's evaluation methodology for Claude Code, for reference:

| Method | Description |
|--------|-------------|
| **SWE-bench** (Verified + Pro) | Primary benchmark; Claude Opus 4.7 scores 87.6% Verified, 64.3% Pro |
| **Terminal-Bench 2.0** | 89 terminal tasks; tests real CLI agent behavior |
| **Bloom** (open-source, MIT) | Automated behavioral eval framework; 4-stage pipeline |
| **Statistical rigor** | Paired differences, power analysis, Wilson confidence intervals |
| **Infrastructure noise research** | Resource config swings scores 6+ points; treated as experimental variable |
| **Tool Search evaluation** | Deferred tool loading improved accuracy 49%→74% (Opus 4) |
| **Daily regression tracker** | N=50 SWE-bench-Pro subset daily, 7/30-day rolling averages |

Key Anthropic publications:
- [Demystifying evals for AI agents](https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents)
- [Statistical approach to model evaluations](https://www.anthropic.com/research/statistical-approach-to-model-evals)
- [Infrastructure noise in agentic coding evals](https://www.anthropic.com/engineering/infrastructure-noise)
- [Bloom: automated behavioral evaluations](https://www.anthropic.com/research/bloom) | [GitHub](https://github.com/safety-research/bloom)

---

## Target Leaderboards (Path to Leadership)

### Ultimate Goal: Leader of AI coding agents and best-in-class agentic assistant

ycode's competitive advantage is **scaffolding quality** — same model with different agent harness produces 30-60% score variance. ycode's embedded tooling (Ollama, Podman, git server, OTEL, 50 tools) is the differentiator.

### Priority 1: Primary Targets (full-agent evaluation, open submission)

| Leaderboard | URL | What It Tests | Why It Matters |
|-------------|-----|---------------|----------------|
| **SWE-bench Pro** | [swebench.com](https://www.swebench.com/) | Real GitHub issue resolution | Gold standard; contamination-resistant; tests full agent |
| **Aider Polyglot** | [aider.chat/leaderboards](https://aider.chat/docs/leaderboards/) | 225 Exercism problems, 6 languages | Tests edit+retry loop; Aider itself is a CLI competitor |
| **Terminal-Bench 2.0** | [tbench.ai](https://www.tbench.ai/leaderboard) | 89 terminal tasks (compile, debug, config) | Terminal-native; directly tests CLI agent behavior |
| **Sigmabench** | [sigmabench.com](https://sigmabench.com/) | Production codebase tasks (Python/Java/Go/TS) | Real-world; scaffolding matters 30-60%; newest benchmark |

### Priority 2: Agent Capability Targets

| Leaderboard | URL | What It Tests | Why It Matters |
|-------------|-----|---------------|----------------|
| **BFCL V4 Agentic** | [gorilla.cs.berkeley.edu](https://gorilla.cs.berkeley.edu/leaderboard.html) | Function/tool calling accuracy | Directly measures ycode's tool dispatch quality |
| **HAL** | [hal.cs.princeton.edu](https://hal.cs.princeton.edu/) | Reliability + consistency + cost | Tests what users actually care about: reliable + cheap |
| **PR Arena** | [prarena.ai](https://prarena.ai/) | Real GitHub PR generation | Tests end-to-end artifact production |
| **LiveCodeBench** | [livecodebench.github.io](https://livecodebench.github.io/) | Live competitive coding + self-repair | Fresh problems weekly; contamination-free |

### Leaderboard Strategy (phased)

**Phase A (with evals)**: Run SWE-bench Pro + Aider Polyglot + Terminal-Bench internally against ycode with various models. Establish baseline scores. Identify weakest areas.

**Phase B (improve)**: Target specific weaknesses revealed by benchmarks. Improve tool dispatch, edit precision, error recovery, context management.

**Phase C (submit)**: Submit to public leaderboards once competitive. Start with Terminal-Bench and Sigmabench (newest, least saturated). Then Aider Polyglot. Then SWE-bench Pro.

**Phase D (lead)**: Optimize scaffolding for each benchmark. The 30-60% scaffolding variance means ycode can potentially outperform tools using the same underlying model through superior agent harness quality.

### Current Competitive Landscape (April 2026)

| Agent | SWE-bench Verified | SWE-bench Pro | Terminal-Bench 2.0 |
|-------|-------------------|---------------|-------------------|
| Claude Code (Opus 4.7) | 87.6% | 64.3% | — |
| Codex CLI (GPT-5.3) | 85% | 56.8% | — |
| Devin | — | — | — |
| Aider (best config) | — | — | — |
| **ycode** | **TBD** | **TBD** | **TBD** |

---

## Provider Matrix

| Provider | Default | When |
|----------|---------|------|
| Local Ollama | Yes | All scheduled runs, development, Tiers 1-3 |
| Anthropic | Opt-in | Leaderboard submissions, Tier 4, `--provider anthropic` |
| OpenAI | Opt-in | Leaderboard submissions, Tier 4, `--provider openai` |

Local Ollama uses `inference.NewLocalProvider()` — zero API keys, zero network. External providers for competitive benchmarking and leaderboard submissions.

Model selection: `EVAL_MODEL` env var or `--model` flag. Default: largest Ollama model available locally.

---

## Phased Rollout

### Phase 1: Foundation + Contract + Scoring (2 weeks)
- `internal/eval/eval.go`: Core types (Scenario, RunResult, Tier, Policy)
- `internal/eval/scoring.go`: pass@k, pass^k, flakiness, composite score, Wilson intervals
- `internal/eval/assertions.go`: ResponseContains, ToolCalled, FileExists, EditPrecision, TrajectoryScore
- `internal/eval/contract/*_test.go`: 10-15 contract tests
- `internal/eval/report.go`: SQLite schema + basic report CLI
- `cmd/ycode/eval.go`: `ycode eval contract` subcommand
- Makefile target `eval-contract` (added to `make build` gate)

### Phase 2: Smoke + Local Ollama (2 weeks)
- `internal/eval/smoke/`: 5 smoke scenarios with 3-trial pass@k
- Integration with `inference.OllamaComponent` for local provider
- Trajectory-capturing middleware on `tools.Registry`
- `ycode eval smoke` subcommand
- OTEL metric emitters for eval scores
- Makefile target `eval-smoke`

### Phase 3: Behavioral + Scheduling + Dashboards (3 weeks)
- `internal/eval/behavioral/`: 7 behavioral scenarios with trajectory assertions
- `internal/eval/schedule.go`: Integration with `team.CronRegistry`
- `internal/eval/judge.go`: LLM-as-judge via local Ollama
- `ycode eval schedule` subcommand
- Perses dashboard provisioning for eval metrics
- `ycode eval report` with regression detection + Wilson intervals
- `ycode eval compare` for version-to-version comparison

### Phase 4: E2E + External Harnesses (3-4 weeks)
- `internal/eval/e2e/`: 5 coding tasks (host temp dirs, optional Podman isolation)
- `internal/eval/e2e/workspace.go`: Temp dir + Gitea repo setup
- `internal/eval/harness/`: setup.sh + requirements.txt + Python eval runners
- Install & integrate Inspect AI, SWE-bench subset, Aider benchmark on host
- Optional Containerfile for sandboxed execution of untrusted agent code
- `ycode eval e2e` subcommand
- `ycode eval history` trend analysis

### Phase 5: Leaderboard Readiness (ongoing)
- Expand E2E to 20+ tasks
- Run full SWE-bench Pro harness (containerized Python) — establish baseline
- Run full Aider polyglot (225 Exercism exercises) — establish baseline
- Run Terminal-Bench 2.0 tasks — establish baseline
- Identify weakest areas, improve scaffolding (tool dispatch, error recovery, context)
- Auto-promotion automation (7 stable runs → AlwaysPasses)
- Multi-provider matrix runs (Ollama + Anthropic + OpenAI comparison)
- Event bus integration (`bus.Event` for eval completion)
- HTTP API endpoints for eval results (`GET /api/evals`)
- Submit to public leaderboards: Terminal-Bench → Sigmabench → Aider → SWE-bench Pro

---

## Key Files to Modify/Create

| File | Action | Purpose |
|------|--------|---------|
| `internal/eval/eval.go` | Create | Core types |
| `internal/eval/scoring.go` | Create | All numerical formulas |
| `internal/eval/assertions.go` | Create | Assertion helpers |
| `internal/eval/report.go` | Create | SQLite storage + regression detection |
| `internal/eval/schedule.go` | Create | CronRegistry integration |
| `internal/eval/metrics.go` | Create | OTEL metric emitters |
| `internal/eval/judge.go` | Create | LLM-as-judge |
| `internal/eval/contract/*_test.go` | Create | 10-15 contract tests |
| `internal/eval/smoke/smoke_test.go` | Create | 5-8 smoke scenarios |
| `internal/eval/behavioral/behavioral_test.go` | Create | 7 behavioral scenarios |
| `internal/eval/e2e/e2e_test.go` | Create | E2E orchestrator |
| `internal/eval/harness/setup.sh` | Create | Host dependency installer (pip install inspect-ai, etc.) |
| `internal/eval/harness/Containerfile` | Create | Optional containerized eval image |
| `cmd/ycode/eval.go` | Create | `ycode eval` subcommand |
| `Makefile` | Edit | Add eval-* targets |
| `pkg/ycode/ycode.go` | Read | Agent API for programmatic execution |
| `internal/tools/registry.go` | Read | Middleware for trajectory capture |
| `internal/inference/provider.go` | Read | Local Ollama provider |
| `internal/container/engine.go` | Read | Podman container management |
| `internal/gitserver/client.go` | Read | Gitea API for test repos |
| `internal/storage/sqlite/sqlite.go` | Read | SQLite for result storage |
| `internal/observability/perses.go` | Read | Dashboard provisioning |
| `internal/runtime/loop/scheduler.go` | Read | Cron scheduling |
| `internal/runtime/team/cron.go` | Read | CronRegistry |

---

## Verification

1. `make eval-contract` — passes in <10s, no LLM, no network
2. `ycode eval smoke` — runs 5×3 trials against local Ollama, produces pass@k and composite score
3. `ycode eval schedule --interval 24h` — creates cron entry, persists across restarts
4. `ycode eval report` — shows scores, deltas, regression flags with Wilson confidence intervals
5. `ycode eval compare HEAD~5 HEAD` — side-by-side numerical comparison
6. Intentionally break a system prompt → `ycode eval smoke` detects regression (composite drops >15%)
7. Perses dashboard shows eval score trend line over last 30 days
