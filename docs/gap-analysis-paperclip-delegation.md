# Gap Analysis: Paperclip — External Agent Delegation

**Tool:** Paperclip (TypeScript/pnpm monorepo, MIT license)
**Domain:** Task Delegation to External Agentic Tools
**Date:** 2026-05-04

---

## Where ycode Is Stronger

| Area | ycode | Paperclip |
|------|-------|-----------|
| A2A protocol | Full HTTP client + server | No A2A protocol |
| Container runtime | Embedded Podman | No container support |
| In-process tools | 50+ built-in tools | Delegates to adapters only |
| Self-healing | AI-driven error fixing | Recovery service but no code fixing |

## Gaps Identified

| ID | Feature | Paperclip Implementation | ycode Status | Priority | Effort |
|----|---------|------------------------|--------------|----------|--------|
| D1 | Adapter module interface | ServerAdapterModule: execute(), testEnvironment(), listSkills(), syncSkills(), sessionCodec, sessionManagement, getRuntimeCommandSpec(), onHireApproved(), detectModel(). | ExternalAdapter interface exists but designed for chat platforms, not agent CLIs. | High | Medium |
| D2 | Runtime command spec | Per-adapter: command, detectCommand, installCommand. Enables auto-detection and auto-install of agent CLIs. | No CLI detection or install guidance for external agents. | Medium | Low |
| D3 | External adapter plugin loader | Dynamic loading from npm packages or local paths. Hot-install/uninstall. Override pause/resume for builtin adapters. | No plugin system for external agent adapters. | Medium | High |
| D4 | Environment run orchestrator | 7-step: resolve env → activate → acquire lease → realize workspace → resolve target → resolve transport → release lease. | No environment orchestration for external agent runs. | Medium | Medium |
| D5 | Skill sync per adapter | listSkills()/syncSkills() per adapter. requiresMaterializedRuntimeSkills flag. Compatibility matrix prevents mismatched skills. | SkillSpec has Compatibility field (from prior analysis) but no sync-to-adapter mechanism. | Low | Medium |

## Implementation Plan

### Phase 1: External agent executor interface (D1) — see consolidated implementation
### Phase 2: Runtime command spec (D2) — included in preset registry

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| D3 | Plugin loader | Hot-loading adds significant complexity; ycode's YAML-based agent definitions are simpler |
| D4 | Environment orchestrator | ycode's worktree isolation handles workspace setup; full 7-step orchestration is over-engineered for CLI |
| D5 | Skill sync per adapter | Skills are prompt-injected in ycode, not file-synced; different architecture |
