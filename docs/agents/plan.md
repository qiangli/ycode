# Implementation Plan: Agent Enhancements

> Based on analysis of 9 prior-art projects. Prioritized by impact and feasibility.
> Generated 2026-04-11.

---

## Phase 1: Custom Agent Definitions (P2)

### 1.1 User-Defined Agents
**Effort:** Medium | **Reference:** OpenCode, OpenClaw, Claw Code
**Files:** `internal/tools/agent.go`, new `internal/runtime/agents/`

- [ ] Support `.ycode/agents/*.toml` files for custom agent definitions
- [ ] Fields: name, description, model, reasoning_effort, tool_allowlist, system_prompt
- [ ] Discovery: scan project → user dirs with shadowing
- [ ] Integrate with Agent tool: `subagent_type` maps to custom agent names
- [ ] Slash command: `/agents` to list available agents

---

## Phase 2: Keyword-Triggered Agents (P2)

### 2.1 Auto-Activation
**Effort:** Medium | **Reference:** OpenHands
**Files:** `internal/tools/skill.go`, `internal/runtime/prompt/`

- [ ] Add `triggers` field to SKILL.md frontmatter
- [ ] Scan user messages for trigger keywords
- [ ] Auto-inject matching skill content into system prompt
- [ ] Limit to 2-3 triggered skills per message to avoid prompt bloat

---

## Phase 3: Inter-Agent Messaging (P2)

### 3.1 Agent Communication
**Effort:** Medium | **Reference:** Codex V2
**Files:** `internal/tools/agent.go`

- [ ] Add `SendMessage` tool for agent-to-agent communication
- [ ] Mailbox-based: messages queued for next turn
- [ ] Agent can wait for messages or status changes
- [ ] Useful for coordination in multi-agent workflows

---

## Implementation Order (Recommended)

```
Phase 1 (Custom Agents) ──→ Phase 2 (Keyword-Triggered) ──→ Phase 3 (Inter-Agent Messaging)
```

---

## Dependencies

| Item | Depends On |
|------|------------|
| Inter-agent messaging | Custom agent definitions |
| Keyword-triggered agents | Skill gating (see skills/plan.md) |
