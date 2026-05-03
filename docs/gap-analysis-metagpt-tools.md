# Gap Analysis: MetaGPT — Built-in Tool System & Tool Use

**Tool:** MetaGPT (Python multi-agent framework)
**Source:** `priorart/metagpt/`
**Date:** 2026-05-03

## Where ycode Is Stronger

| Area | ycode | MetaGPT |
|------|-------|---------|
| Permission model | Tiered modes with per-tool policy rules, PermissionMatcher, approval flows | No permission enforcement |
| Category-aware scheduling | Interactive/Agent/Standard/LLM categories for concurrency control | No tool categorization |
| Bash security | Destructive command detection, read-only allowlist, eval blocking, SSRF protection | No command-level validation |
| Streaming executor | Starts parallel tools before all calls arrive | No streaming tool execution |
| Semantic tool discovery | Bleve search index for deferred discovery (prevents token explosion) | Name/tag lookup only |
| Tool middleware chain | Global + per-tool composable middleware (retry, timeout, events) | No middleware pattern |
| Context modification | ContextModifier hooks for sequential tool chaining | No equivalent |
| Quality monitoring | Per-tool success rate tracking for decision-making | No metrics |
| MCP integration | Full MCP client (stdio + SSE) for external tool servers | No MCP support |

## MetaGPT Tool Features

MetaGPT has a Python-centric tool system:
- **Decorator registration**: `@register_tool` with auto-capture of source code and file path
- **AST schema extraction**: Converts function signatures to JSON schemas via AST analysis
- **Tag-based discovery**: Two-layer dict (tools_by_tags) for categorized lookup
- **Browser automation**: Direct Playwright integration (not containerized)
- **Multiple search engines**: SerpAPI, Serper, DuckDuckGo, Bing via dynamic loading
- **Method-level selection**: Fine-grained tool selection (e.g., `Editor:read,write`)

## Gaps Identified

No actionable gaps identified. MetaGPT's decorator-based registration and AST schema extraction are elegant Python patterns but not applicable to Go. ycode's permission model, middleware chain, and streaming executor are architecturally superior for a production CLI agent.

## Verification

N/A — no implementation changes for this domain.
