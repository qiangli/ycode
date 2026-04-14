# Gemini CLI - Tools & Security Analysis

**Project:** Google Gemini CLI
**Language:** TypeScript/Node.js
**Repository:** google-gemini/gemini-cli

---

## Tools (Function Calling) - 20+ tools

### Read-Only Tools (8)
| Tool | Description |
|------|-------------|
| `read_file` | Read file with optional line range |
| `read_many_files` | Batch read with include/exclude patterns |
| `glob` | File pattern matching |
| `grep_search` / `grep_search_ripgrep` | Full-text search with regex |
| `list_directory` | Directory listing with filtering |
| `web_fetch` | HTTP fetch with markdown conversion |
| `google_web_search` | Google web search integration |
| `get_internal_docs` | Internal documentation retrieval |

### Write/Modify Tools (4)
| Tool | Description |
|------|-------------|
| `write_file` | Create or overwrite files |
| `replace` (edit) | In-place file editing with diff confirmation |
| `run_shell_command` | Shell execution with interactive/background modes |
| `write_todos` | Structured todo lists |

### Agent/Context Tools (6)
| Tool | Description |
|------|-------------|
| `delegate_to_agent` | Invoke subagents (agents-as-tools) |
| `save_memory` | Persist facts for context |
| `update_topic` | Track conversation topics |
| `enter_plan_mode` / `exit_plan_mode` | Planning mode control |
| `activate_skill` | Activate and invoke skills |

### Interaction Tools (2)
| Tool | Description |
|------|-------------|
| `ask_user` | Multi-question interactive prompts (text/multi-select/single-select) |
| `complete_task` | Signal agent completion |

### MCP Tools (dynamic)
| Tool | Description |
|------|-------------|
| `mcp_<server>_<tool>` | Dynamically discovered MCP tools with namespace prefixes |

---

## Security & Guardrails

### Policy Engine (Sophisticated)
| Feature | Description |
|---------|-------------|
| **PolicyRule** | ALLOW/DENY/ASK_USER decisions per tool |
| **Tool name matching** | Exact, wildcard (`*`), MCP wildcards (`mcp_servername_*`) |
| **Argument matching** | Regex-based parameter validation |
| **Tool annotations** | Metadata-based matching |
| **Priority system** | Higher priority rules override, fractional priorities |
| **Subagent scoping** | Rules can target specific subagents |
| **HMAC-SHA256** | Policy integrity checking |

### Approval Modes
| Mode | Description |
|------|-------------|
| `DEFAULT` | Always confirm |
| `AUTO_EDIT` | Auto-approve edits, confirm others |
| `YOLO` | Auto-approve all (priority 998) |
| `PLAN` | Separate approval for implementation |

### Confirmation System
- `proceed_once`, `proceed_always`, `proceed_always_and_save`
- `proceed_always_server`, `proceed_always_tool`
- `modify_with_editor` (edit before execution)
- `cancel`

### Sandboxing
| Backend | Description |
|---------|-------------|
| `sandbox-exec` | macOS Seatbelt |
| Docker/Podman | Container isolation |
| gVisor | Google's container sandbox |
| LXC | Linux containers |
| None | No sandbox |

### Additional Security
| Mechanism | Description |
|-----------|-------------|
| **Folder trust** | Project-level trust requirement for config loading |
| **Safety checkers** | In-process (`allowed-path`, `conseca`) + external |
| **Content filtering** | .gitignore, .geminiignore, large file handling |
| **Workspace policy** | Restricts operations to configured boundaries |
| **Browser security** | Domain allowlist, prompt injection protection |
| **Hook security** | Hook source validation (project/user/system/extension) |
| **Tool parallel control** | Read-only parallel; mutators sequential with wait_for_previous |

---

## Notable Patterns

- **Declarative tools:** `DeclarativeTool` base class with schema-driven validation
- **Tool kinds:** Read, Edit, Delete, Move, Search, Execute, Think, Agent, Fetch, etc.

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| Policy-as-Code (TOML with HMAC) | Not implemented | **Medium** - tamper-proof policies |
| `read_many_files` (batch read) | Implemented (`read_multiple_files`) | Done |
| Folder trust system | Not implemented | **Medium** - security |
| Safety checker plugins | Not implemented | **Medium** - extensible validation |
| Tool kind classification | Not implemented | Low |
