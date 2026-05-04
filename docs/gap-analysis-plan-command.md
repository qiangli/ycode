# Gap Analysis: Plan Command & Parallel Subagent Orchestration

## Sources Studied

1. **Claude Code** (`~/projects/poc/claude/snapshot/src/`) — shift-tab mode cycle, EnterPlanMode/ExitPlanMode tools, 5-phase plan workflow, Plan/Explore built-in agents
2. **OpenCode** (`./priorart/opencode/`) — plan/build agent separation, plan_enter/plan_exit tools, 5-phase workflow with parallel explore subagents, task tool for concurrent execution
3. **ycode** (`internal/`) — existing plan mode toggle, Agent tool with Explore/Plan types, research executor with DAG-based parallelism

## Where ycode Is Stronger

| Area | ycode Advantage |
|------|-----------------|
| DAG-based research | `ResearchPlanV2` with dependency graphs and parallel execution — neither Claude Code nor OpenCode has this |
| Agent pool metrics | Full tracking with atomic tool counts, token usage, OTEL spans, episodic memory |
| Lane-based concurrency | `LaneScheduler` for bounded concurrent subagent slots |
| Custom agent definitions | YAML-based agent defs with flows (sequence, parallel, DAG, router, fallback) |
| Swarm orchestration | Handoff chains with cycle detection, context variable passing |
| Mode transition reminders | System-reminder injection on shift-tab toggle |

## Gaps Identified

| ID | Feature | Claude/OpenCode Implementation | ycode Status | Priority | Effort |
|----|---------|-------------------------------|-------------|----------|--------|
| P1 | `/plan` slash command | Claude: `/plan` command toggles mode and optionally queries. OpenCode: `plan_enter` tool | **Missing** — only shift-tab toggle and LLM tools exist, no slash command | High | Low |
| P2 | Plan file persistence | Claude: plan saved to disk per session. OpenCode: `.opencode/plans/*.md` | **Missing** — plan output is ephemeral in conversation | Medium | Medium |
| P3 | Plan mode system prompt: parallel explore guidance | Claude/OpenCode: Phase 1 says "launch up to 3 explore agents in parallel" | **Present** — already in `PlanModeSection()` | N/A | N/A |
| P4 | Plan mode: Phase 2 Design subagents | Claude: "launch 1-3 Plan agents for design". OpenCode: same | **Missing** — Phase 2 doesn't instruct spawning Plan subagents | High | Low |
| P5 | Plan mode: concise plan file format | Claude: `getPewterLedgerVariant()` caps plan at 40 lines. OpenCode: plan file structure | **Missing** — no plan file format guidance | Medium | Low |
| P6 | Plan approval on exit | Claude: team lead approval via mailbox. OpenCode: `plan_exit` prompts user | **Missing** — ExitPlanMode just restores mode silently | Medium | Low |

## Implementation Plan

### Phase 1: `/plan` Slash Command (P1)

Add `/plan` to `internal/commands/handlers.go`:
- `/plan` with no args: toggle mode (same as shift-tab)
- `/plan <query>`: enter plan mode + inject query as plan context
- `/plan open`: show current plan file (future, after P2)

### Phase 2: Enhanced Plan Mode Prompt (P4, P5)

Update `PlanModeSection()` in `internal/runtime/prompt/sections.go`:
- Phase 2: instruct spawning 1-3 Plan subagents for design work
- Phase 4: add plan file format guidance (concise, actionable, max 40 lines)
- Phase 5: show plan summary to user before exiting

### Phase 3: Plan File Persistence (P2)

Add plan file support:
- Save plan to `.agents/ycode/plans/<session-id>.md`
- ExitPlanMode returns plan content for user review

### Phase 4: Plan Approval on Exit (P6)

Update ExitPlanMode to show plan summary and ask for confirmation before switching to build mode.

## Deferred

- Team lead approval workflow (Claude-specific, not relevant for solo use)
- Plan file experiment variants (trim/cut/cap — premature optimization)

## Verification

1. `make build` passes
2. `/plan` command toggles mode correctly
3. In plan mode, explore subagents are spawned in parallel
4. Plan prompt includes design phase subagent guidance
5. Unit tests for new `/plan` command handler
