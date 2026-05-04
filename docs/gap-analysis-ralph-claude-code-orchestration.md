# Gap Analysis: Ralph Claude Code — Agent Orchestration & Workflow

**Tool:** Ralph Claude Code (Bash/JS, MIT license)
**Domain:** Agent Orchestration & Workflow
**Date:** 2026-05-04

---

## Where ycode Is Stronger

| Area | ycode | Ralph |
|------|-------|-------|
| Multi-agent coordination | Swarm orchestrator, DAG executor, hierarchical manager, mesh | Single-agent only; no multi-agent coordination |
| Agent definitions | YAML with inheritance, AOP advices, guardrails, triggers | No agent definition system |
| Self-healing (code fixing) | AI-driven error fixing, 7 failure types, protected paths | Circuit breaker halts execution; no AI-driven fixing |
| Skill system | Disk-based skills with evolution, degradation detection | No skill system; delegates entirely to Claude's native tools |
| Sprint execution | State machine with milestones/slices/tasks/budget | Single task-at-a-time from fix_plan.md |
| Hook system | Registry with pattern matching, shell protocol, 7 event types | Static ALLOWED_TOOLS list; no hook system |
| Background agents | Mesh: Diagnoser, Fixer, Learner, Researcher | No background agents |
| Git integration | Worktree isolation, branch naming, cleanup | Backup branches only; no worktree isolation |

## Gaps Identified

| ID | Feature | Ralph Implementation | ycode Status | Priority | Effort |
|----|---------|---------------------|--------------|----------|--------|
| A1 | Question-loop suppression | Detects when agent asks questions instead of acting (regex patterns). Injects corrective guidance next iteration. Holds circuit breaker counter steady to prevent false-positive opening. | No question detection. If agent asks questions in headless/autoloop mode, loop may stall or waste iterations. | High | Low |
| A2 | Dual-layer exit control | Heuristic detection (completion_indicators >= 2) AND explicit EXIT_SIGNAL from agent. Both required to prevent false positives from documentation keywords. Separate work_type classification. | autoloop has stagnation detection but no explicit agent-signaled exit. Completion determined by score thresholds only. | High | Low |
| A3 | Circuit breaker with cooldown recovery | Three-state (Closed/HalfOpen/Open). 5 configurable thresholds: no-progress, same-error, permission-denial, output-decline, cooldown. OPEN→wait→HALF_OPEN→progress→CLOSED. | selfheal has max attempts but no circuit breaker pattern. No cooldown-based recovery from failure states. | Medium | Medium |
| A4 | Two-stage error filtering | Stage 1: filter JSON field patterns that contain "error" but aren't real errors. Stage 2: match actual error contexts with multi-line matching. Prevents context echoes from triggering false positives. | selfheal classifies errors by type but no false-positive filtering for context echoes or JSON field artifacts. | Medium | Low |
| A5 | Rate limiting with dual metrics | Call count + token budget per hour. Counters reset hourly. Blocks further calls when limit reached. Unattended auto-wait with timeout. | No rate limiting at agent level. Provider-level retries exist but no budget enforcement. | Medium | Low |
| A6 | Task source abstraction | Unified import from Beads, GitHub Issues, PRD files. Format normalization with fallback parsing. | Sprint tasks defined inline. No external task source import (GitHub issues, PRDs, etc.). | Low | Medium |

## Implementation Plan

### Phase 1: Question-Loop Suppression (A1)

**Files to modify:** `internal/runtime/autoloop/loop.go`

Add question detection:
1. After each agent turn in autoloop, scan response for question patterns:
   - "should I", "do you want", "would you like", "which option", "can you clarify"
   - Threshold: 2+ question patterns in response
2. If detected: inject corrective system message next iteration:
   "You asked questions. This is autonomous mode. Choose the most conservative default and proceed."
3. Do NOT increment stagnation counter on question turns (prevents false circuit break)

### Phase 2: Dual-Layer Exit Control (A2)

**Files to modify:** `internal/runtime/autoloop/loop.go`

Add explicit exit signaling:
1. Define status block protocol in autoloop prompt:
   `STATUS: IN_PROGRESS|COMPLETE` and `EXIT_SIGNAL: true|false`
2. Parse agent response for status block
3. Exit only when BOTH: (a) heuristic completion indicators met AND (b) agent signals EXIT_SIGNAL=true
4. Single indicator alone is insufficient — prevents false positives from documentation about "completion"

### Phase 3: Circuit Breaker (A3)

**Files to create:** `internal/runtime/autoloop/circuit_breaker.go`

Three-state circuit breaker:
1. States: Closed (normal) → Open (halted) → HalfOpen (probing)
2. Thresholds: consecutive-no-progress (3), consecutive-same-error (5), output-decline (70%)
3. OPEN: wait cooldown period (configurable, default 30s for CLI)
4. HALF_OPEN: run one iteration; if progress → CLOSED, if fail → OPEN
5. Integrate with autoloop: check circuit state before each iteration

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| A5 | Rate limiting | Provider-level retries handle API limits; agent-level budget enforcement useful but not critical for local CLI |
| A6 | Task source abstraction | Sprint tasks work for current use cases; external import is a feature request, not a gap |

## Verification

- Unit test: question detection identifies question patterns in sample responses
- Unit test: corrective injection added when questions detected
- Unit test: dual-layer exit requires both conditions
- Unit test: circuit breaker transitions through states correctly
- Unit test: cooldown recovery from OPEN→HALF_OPEN→CLOSED on progress
- `make build` must pass
