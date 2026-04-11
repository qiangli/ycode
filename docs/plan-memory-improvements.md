# Memory & Context Management Improvements Plan

## Problem Statement

ycode's context management is reactive-only: compaction triggers **after** a token limit error from the API. Best-in-class agents (OpenClaw, Claude Code) use a multi-layered proactive defense. The gap analysis identified 5 critical improvements.

## Implementation Plan

### 1. Proactive Auto-Compaction in Turn Loop

**Gap**: `NeedsCompaction()` exists but is never called. Compaction only happens reactively after API rejection.

**Fix**: Add token estimation before each API call in the agentic loop. When estimated tokens exceed `CompactionThreshold`, compact proactively before sending the request.

**Files**: `internal/cli/app.go`, `internal/runtime/conversation/runtime.go`

### 2. Context Pruning (Layer 1 - Soft/Hard Trim)

**Gap**: No pre-compaction tool result trimming. Large tool outputs (file reads, grep results) consume context budget unnecessarily.

**Fix**: Implement OpenClaw-style context pruning with two tiers:
- **Soft trim** (at 60% of compaction threshold = 60K tokens): Truncate old tool results keeping head + tail with `[...]` marker
- **Hard clear** (at 80% = 80K tokens): Replace old tool results with placeholder text

**Files**: New `internal/runtime/session/pruning.go`

### 3. Post-Compaction Context Refresh

**Gap**: After compaction, critical instructions from CLAUDE.md are lost. OpenClaw re-injects key sections.

**Fix**: After compaction, re-inject a condensed version of critical instruction sections (from CLAUDE.md) into the continuation message.

**Files**: `internal/runtime/conversation/runtime.go`, `internal/runtime/prompt/refresh.go`

### 4. Memory Flush (Layer 3 - Emergency Session Restart)

**Gap**: No last-resort mechanism when even compaction isn't enough.

**Fix**: When compaction + retry still fails, perform a memory flush: save a summary to persistent memory, create a minimal continuation message with just the last user request + summary.

**Files**: `internal/runtime/conversation/runtime.go`

### 5. Proactive Token Monitoring with Warning

**Gap**: No visibility into context health during long-running sessions.

**Fix**: Log context health metrics after each turn. Emit a warning when approaching thresholds.

**Files**: `internal/cli/app.go`

## Implementation Order

1. Context pruning (foundation for all other layers)
2. Proactive auto-compaction (uses pruning + existing compaction)
3. Post-compaction context refresh
4. Memory flush (emergency fallback)
5. Token monitoring/warnings

## Design Principles

- Keep OpenClaw's 3-layer defense pattern but adapt to ycode's Go architecture
- No external dependencies (no SQLite, no vector DB)
- All changes backward-compatible with existing session format
- Pruning is in-memory only (doesn't modify persisted session)

## Status: COMPLETED (2026-04-10)

All 5 improvements implemented, tested, and documented. See docs/todo.md Phase 9.
