# Gap Analysis: Paperclip — Agent Orchestration & Workflow

**Tool:** Paperclip (TypeScript/pnpm monorepo, MIT license)
**Domain:** Agent Orchestration & Workflow
**Date:** 2026-05-04

---

## Where ycode Is Stronger

| Area | ycode | Paperclip |
|------|-------|-----------|
| Multi-agent flows | 8 flow types with DAG executor, hierarchical manager, handoff protocol | Issue-centric tree coordination; no explicit flow types |
| Agent definitions | YAML with inheritance, AOP advices, guardrails, output schemas, triggers | Adapter configs in registry; no definition language |
| Self-healing (code fixing) | AI-driven error fixing, 7 failure types, protected paths, rebuild/restart | Recovery service with reassignment/continuation but no AI code fixing |
| Background agents | Mesh: Diagnoser, Fixer, Learner, Researcher with event bus | Heartbeat service + recovery service (no specialized background agents) |
| Skill evolution | Outcome tracking, degradation detection, decay scoring | Company skills with compatibility matrix but no outcome-based evolution |
| Autonomous loop | 5-phase with stagnation detection and replanning | No autonomous loop |
| Sprint execution | State machine with milestones/slices/tasks/budget/retry | Issue execution policy covers phases but no budget/milestone tracking |

## Gaps Identified

| ID | Feature | Paperclip Implementation | ycode Status | Priority | Effort |
|----|---------|------------------------|--------------|----------|--------|
| A1 | Liveness classification as separate service | Detects stuck/stranded issues via output silence. Suspicion threshold (1hr), critical threshold (4hr). Decoupled from recovery action. Multiple detection origins share classification. | selfheal handles errors reactively. No proactive silence-based liveness detection. No decoupled detection→action pipeline. | High | Medium |
| A2 | Skill compatibility matrix | Per-adapter compatibility metadata (claude_local, codex_local, cursor_local). Trust levels (built_in/trusted/unverified). Prevents skill injection into incompatible agents. | Skills loaded uniformly for all agents. No per-agent-type compatibility check. No trust levels. | High | Low |
| A3 | Issue-centric coordination with pause holds | Work modeled as issue tree. Pause holds provide generic suspension (parent blocks children). Status propagates through tree. Comments enable lightweight multi-agent communication. | Sprint has task hierarchy (milestone→slice→task) but no generic pause/hold mechanism. No tree-propagated status. | Medium | Medium |
| A4 | Routine scheduling with catch-up | Persistent cron-based routines with timezone awareness. Catch-up logic executes missed runs (max 25). Template variables for interpolation. | autoloop has iterations but no cron-based scheduling with catch-up for missed runs. | Medium | Medium |
| A5 | Plugin job coordinator pattern | Bridges lifecycle manager + scheduler + job store via event-driven coordinator. Fire-and-forget async handlers. Stable references prevent listener leaks. | No coordinator pattern for bridging independent services. Components are directly coupled or use bus.Bus. | Medium | Low |
| A6 | Feedback voting with consent | Structured feedback bundles with redaction. Votes on runs (helpful/unhelpful). Consent versioning. Context capture for training. | Memory system captures feedback but no structured voting or consent infrastructure. | Low | Medium |

## Implementation Plan

### Phase 1: Liveness Classification Service (A1)

**Files to create:** `internal/runtime/agentpool/liveness.go`

Implement proactive liveness detection:
1. `LivenessClassifier` monitors time since last output per agent
2. Thresholds: `Healthy`(< 5min) → `Suspicious`(5-30min) → `Critical`(30min+) → `Stranded`(2hr+)
3. Classification decoupled from action: classifier emits events, recovery decides action
4. Integrate with mesh Patrol agent (from gastown gap A6) for periodic scanning

### Phase 2: Skill Compatibility Matrix (A2)

**Files to modify:** `internal/runtime/skillengine/registry.go`, `internal/runtime/agentdef/definition.go`

Add compatibility checking:
1. `CompatibleAgents []string` field on SkillSpec (e.g., ["claude", "openai", "ollama"])
2. `TrustLevel` enum on SkillSpec: builtin/trusted/unverified
3. `SelectSkill()` filters by agent compatibility before scoring
4. Agent type derived from provider in RuntimeContext

### Phase 3: Catch-Up Scheduling (A4)

**Files to modify:** `internal/runtime/autoloop/loop.go`

Add missed-run catch-up:
1. Track `lastCompletedAt` timestamp per loop
2. On resume: calculate missed iterations since lastCompletedAt
3. Execute up to `MaxCatchUpRuns` (default 5) before resuming normal schedule
4. Log catch-up runs distinctly for observability

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| A3 | Issue-centric coordination | ycode's sprint model serves a similar purpose; pause holds add complexity for marginal benefit |
| A5 | Plugin job coordinator | Good pattern but ycode's direct coupling is simpler and sufficient at current scale |
| A6 | Feedback voting | Memory system already captures feedback; structured voting adds UX complexity |

## Verification

- Unit test: liveness classifier transitions through states based on elapsed time
- Unit test: skill compatibility filtering excludes incompatible agents
- Unit test: catch-up scheduling executes correct number of missed runs
- Integration test: liveness classification triggers recovery event
- `make build` must pass
