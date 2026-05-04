# Gap Analysis: Ralph Claude Code — External Agent Delegation

**Tool:** Ralph Claude Code (Bash/JS, MIT license)
**Domain:** Task Delegation to External Agentic Tools
**Date:** 2026-05-04

---

## Where ycode Is Stronger

| Area | ycode | Ralph |
|------|-------|-------|
| Multi-agent coordination | Swarm, DAG, mesh, hierarchical manager | Single-agent only |
| A2A protocol | Full HTTP client + server | None |
| Container runtime | Embedded Podman | None |
| Agent definitions | YAML with inheritance, guardrails, triggers | No agent definition system |

## Gaps Identified

| ID | Feature | Ralph Implementation | ycode Status | Priority | Effort |
|----|---------|---------------------|--------------|----------|--------|
| D1 | CLI command builder with flag array | build_claude_command() constructs safe command array: --output-format json, --allowedTools, --model, --effort, --resume, --append-system-prompt, -p. Shell-injection safe. | No command builder for external agent CLIs. | High | Low |
| D2 | Multi-format output parsing | Detects JSON vs text. Supports 3 JSON formats (flat, CLI object, CLI array). Session ID extraction with fallback. RALPH_STATUS block parsing. Two-stage error filtering. | No output parsing for external agent results. | High | Medium |
| D3 | Session continuity via --resume | Persists session ID in .ralph/.claude_session_id. 24-hour expiration. Never persists error sessions. Auto-reset on circuit break. | No session resume for external agents. A2A has session concept but not for CLI. | High | Low |
| D4 | Productive timeout detection | On timeout (exit 124): checks if agent made file changes. If productive → success path. If idle → error. Prevents discarding useful partial work. | No timeout-aware result handling. Bash exec has timeout but binary success/fail. | Medium | Low |
| D5 | Allowed tools configuration | Granular ALLOWED_TOOLS with pattern-based restrictions. Blocks destructive git commands while allowing safe ones. Permission denial detection in output. | No tool restriction pass-through for external agents. | Medium | Low |
| D6 | Loop context injection | build_loop_context(): loop number, remaining tasks, circuit state, previous summary, corrective guidance. Injected via --append-system-prompt. | autoloop has Callbacks but no context injection into external agents. | Medium | Low |

## Implementation Plan

### Phase 1: Command builder with flag array (D1) — included in preset registry
### Phase 2: Output parsing (D2) — see consolidated implementation
### Phase 3: Session continuity (D3) — included in agent executor

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| D5 | Tool restriction pass-through | Can be added to preset config incrementally |
| D6 | Loop context injection | autoloop Callbacks already provide injection points; external agent context is format-specific |
