# Gap Analysis: ADK-Python — Built-in Tool System & Tool Use

**Tool:** Google Agent Development Kit (ADK-Python)
**Source:** `priorart/adk-python/`
**Date:** 2026-05-03

## Where ycode Is Stronger

| Area | ycode | ADK-Python |
|------|-------|------------|
| Permission model | Tiered modes with per-tool policy rules and PermissionMatcher | Callback-based permission only |
| Category-aware scheduling | Interactive/Agent/Standard/LLM categories for concurrency control | All tools treated equally |
| Streaming executor | Starts parallel tools before all calls arrive (reduces latency) | Standard asyncio.gather() |
| Bash security | Destructive command detection, read-only allowlist, eval blocking | Resource limits but no command-level validation |
| TTY executor | Interactive terminal commands (ssh, sudo) in TUI mode | No interactive command support |
| Context modification | ContextModifier hooks for sequential tool chaining | No equivalent |
| Quality monitoring | Per-tool success rate tracking for decision-making | No metrics collection |
| Tool middleware chain | Global + per-tool composable middleware (new from LangGraph) | No middleware pattern |
| Semantic tool discovery | Bleve search index for deferred discovery | No search-based discovery |

## ADK-Python Tool Features

ADK implements a comprehensive tool ecosystem:
- **Rich schema generation**: Pydantic-based with complex types (Enums, Optional, Lists)
- **Authentication framework**: OAuth2, service account, credential exchange per tool
- **MCP integration**: MCPToolset with dynamic tool loading from MCP servers
- **Specialized tools**: Computer use (coordinate normalization), Google APIs (OpenAPI-based)
- **Tool confirmation flow**: Blocking user approval for sensitive operations
- **Long-running tools**: Resource ID tracking for async completion
- **Lazy imports**: `__getattr__` for 40+ tools without startup overhead

## Gaps Identified

No actionable gaps identified. ADK's strengths (authentication framework, computer use, OpenAPI parsing) are either already covered by ycode's existing tools or represent Google-specific integrations not applicable to a generic CLI agent. ycode's permission model, scheduling, and middleware are architecturally superior.

## Verification

N/A — no implementation changes for this domain.
