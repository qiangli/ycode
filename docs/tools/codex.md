# Codex CLI - Tools & Security Analysis

**Project:** OpenAI Codex CLI Agent
**Language:** Rust (codex-rs core), TypeScript (codex-cli wrapper)
**Repository:** openai/codex

---

## Tools (Function Calling) - 23+ tools

### Core Execution Tools (4)
| Tool | Description |
|------|-------------|
| `shell` | Execute shell commands with timeout/output limits |
| `exec_command` | Run commands in PTY with session support |
| `write_stdin` | Write to active exec_command session |
| `unified_exec` | Unified interface combining shell and exec_command |

### File & Directory Tools (2)
| Tool | Description |
|------|-------------|
| `list_dir` | List directory with pagination (offset, limit, depth) |
| `apply_patch` | Apply patches (freeform and JSON format, multi-file atomic) |

### Code Execution (3)
| Tool | Description |
|------|-------------|
| `js_repl` | JavaScript REPL with Meriyah parser |
| `js_repl_reset` | Reset JavaScript REPL state |
| `code_mode_execute` / `code_mode_wait` | Specialized code execution context |

### Planning (1)
| Tool | Description |
|------|-------------|
| `update_plan` | Document agent plan/checklist, emits PlanUpdate events |

### Image & Media (1)
| Tool | Description |
|------|-------------|
| `view_image` | Display and view images with configurable detail |

### MCP Tools (4)
| Tool | Description |
|------|-------------|
| `mcp[namespace:toolname]` | Dynamic MCP tool invocation |
| `read_mcp_resource` | Read MCP resources |
| `list_mcp_resources` | List available MCP resources |
| `list_mcp_resource_templates` | List MCP resource templates |

### Permission & Discovery (4)
| Tool | Description |
|------|-------------|
| `request_permissions` | Request runtime permission changes |
| `request_user_input` | Prompt user for input |
| `tool_search` | Search for deferred tools |
| `tool_suggest` | ML-based tool recommendation |

### Multi-Agent Tools (8)
| Tool | Description |
|------|-------------|
| `spawn_agent` (V1/V2) | Create child/sibling agents with task trees |
| `send_input` / `send_message` | Inter-agent messaging |
| `followup_task` | Send message with explicit turn trigger |
| `wait_agent` (V1/V2) | Wait for agent completion or mailbox |
| `list_agents` | List live agents in session tree |
| `close_agent` / `resume_agent` | Agent lifecycle control |

### Batch Tools (2)
| Tool | Description |
|------|-------------|
| `spawn_agents_on_csv` | Batch agent spawning from CSV |
| `report_agent_job_result` | Report batch job results |

---

## Security & Guardrails

### Guardian Review Agent (Unique)
- Dedicated LLM (gpt-5.4) reviews every approval request
- Risk assessment: LOW, MEDIUM, HIGH, CRITICAL
- Authorization levels: LOW, MEDIUM, HIGH
- Outcomes: ALLOW or DENY (fail-closed)
- 90-second timeout (fail-closed on timeout)
- Policy covers: data exfiltration, credential probing, persistent security weakening, destructive actions

### Sandboxing (Platform-Specific)
| Platform | Mechanism |
|----------|-----------|
| **macOS** | Seatbelt (sandbox-exec) - restricts writes, keeps .git read-only |
| **Linux** | Landlock or bubblewrap (bwrap) - namespace isolation |
| **Windows** | Token-restricted or elevated execution |

### Approval System
| Mode | Description |
|------|-------------|
| `Never` | No approval required |
| `OnFailure` | Ask after sandbox failure |
| `OnRequest` | Ask for restricted filesystem access |
| `Granular` | Granular sandbox approval config |
| `UnlessTrusted` | Always ask |

### Additional Security
| Mechanism | Description |
|-----------|-------------|
| **Network approval** | Host-based allowlisting, protocol control (HTTP/HTTPS/SOCKS5) |
| **Execution policy** | Skip/NeedsApproval/Forbidden per command |
| **Path enforcement** | Absolute paths, base path guards, boundary checks |
| **Hook system** | pre_tool_use / post_tool_use hooks |
| **Approval caching** | Session-level, serialized key caching |
| **Proposed amendments** | Auto-suggest policy updates after approval |

---

## Notable Patterns

- **ToolHandler trait:** Generic handler abstraction with mutability detection
- **Guardian lifecycle:** Separate review ID from tool call ID, compact transcript
- **Deferred tool loading:** Tools marked `defer_loading: true`
- **Event-driven:** Tool invocations emit ToolEventCtx for audit trail

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| Guardian review agent (LLM-based approval) | Not implemented | **High** - novel security approach |
| Platform sandboxing (Seatbelt/Landlock/bwrap) | Not implemented | **High** - critical for safety |
| Network approval system | Not implemented | **Medium** - security |
| JS REPL | Not implemented | Low |
| view_image tool | Not implemented | **Medium** - useful for screenshots |
| apply_patch (atomic multi-file) | Not implemented | **Medium** |
| Approval caching | Not implemented | **Medium** - UX improvement |
