# Gap Analysis: AutoGen — Built-in Tool System & Tool Use

**Tool:** AutoGen (Python multi-agent framework)
**Source:** `priorart/autogen/`
**Date:** 2026-05-03

## Where ycode Is Stronger

| Area | ycode | AutoGen |
|------|-------|---------|
| Permission model | Tiered modes (ReadOnly→WorkspaceWrite→DangerFullAccess) with per-tool policy rules and PermissionMatcher | Schema-based validation only; no permission tiers |
| Bash security | Destructive command detection, read-only allowlist, signal handling, eval blocking | No bash security; delegated to executor sandboxing |
| Category-aware scheduling | Interactive/Agent/Standard/LLM tool categories for concurrency control | Simple asyncio.gather() for all tools |
| Semantic tool discovery | Bleve search index for deferred tool discovery (prevents token explosion) | Eager loading of all tools; no search |
| Tool middleware | Global + per-tool middleware chain with retry/timeout/event-writer (new from LangGraph) | No middleware pattern |
| Quality monitoring | Success rate tracking per tool for decision-making | No metrics collection |
| File operation safety | Binary detection, sensitive file warnings, VFS traversal prevention | No file operations in core |
| Web tool security | SSRF protection, domain filtering, redirect validation, size limits | HttpTool with basic REST wrapping |
| LSP integration | Native code intelligence (definitions, references, hover, call hierarchy) | No LSP support |
| Background task management | Job registry for long-running commands with signal support | CancellationToken only |

## AutoGen Tool Features

AutoGen's tool system includes:
- **Protocol-based design**: Tool interface with FunctionTool, BaseTool, StreamTool
- **MCP integration**: McpToolAdapter bridging MCP tools with session lifecycle management
- **Code execution environments**: Docker (with GPU), Jupyter, Azure ACA, local
- **Streaming tools**: BaseStreamTool with async generators for intermediate results
- **Workbench pattern**: list_tools/call_tool with state persistence and lifecycle management

## Gaps Identified

No actionable gaps identified. AutoGen's strengths (MCP adapter, code execution environments, streaming tools) are either already covered by ycode's MCP support and container tools, or represent different architectural choices (Python async vs Go concurrency). ycode's permission model and category-based scheduling are architecturally superior for a CLI agent.

## Verification

N/A — no implementation changes for this domain.
