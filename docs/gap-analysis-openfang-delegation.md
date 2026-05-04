# Gap Analysis: OpenFang — External Agent Delegation

**Tool:** OpenFang (Rust, 14 crates, MIT/Apache-2.0 license)
**Domain:** Task Delegation to External Agentic Tools
**Date:** 2026-05-04

---

## Where ycode Is Stronger

| Area | ycode | OpenFang |
|------|-------|----------|
| Container runtime | Embedded Podman, auto-provisioning | No container support |
| Skill system | Outcome tracking, degradation detection, decay | Hot-reload registry but no outcome-based evolution |
| Sprint execution | Full state machine with milestones/tasks/budget | No sprint concept |

## Gaps Identified

| ID | Feature | OpenFang Implementation | ycode Status | Priority | Effort |
|----|---------|------------------------|--------------|----------|--------|
| D1 | ProcessManager for persistent subprocesses | Ring-buffered stdout/stderr (1000 lines), per-agent limits, start/write/poll/kill lifecycle. Background tokio tasks stream output. | Bash exec runs commands but no persistent process tracking with output buffering | High | Medium |
| D2 | KernelHandle trait for agent delegation | Async trait: spawn_agent(), send_to_agent(), list_agents(), kill_agent(), task_post(), task_claim(). Avoids circular deps. | Swarm Orchestrator has Spawner callback but no standardized delegation interface | Medium | Medium |
| D3 | 20+ LLM driver backends | Anthropic, Gemini, OpenAI, Bedrock, Copilot, Ollama, vLLM, etc. Config-driven selection. | Provider interface supports Anthropic + OpenAI-compatible. Fewer backends but extensible. | Low | N/A |
| D4 | Agent manifest TOML for dynamic spawning | Runtime-parseable TOML with model, tools, tags, fallback_models. Agents can spawn other agents from manifests. | AgentDef YAML exists but agents can't spawn other agents from definitions at runtime | Medium | Low |

## Implementation Plan

### Phase 1: Process lifecycle in external agent executor — see consolidated implementation

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| D3 | 20+ LLM drivers | ycode's Provider interface is extensible; adding drivers is incremental work, not architectural gap |
| D4 | Dynamic agent spawning from manifests | Useful but requires runtime agent creation; defer until multi-agent workflows mature |
