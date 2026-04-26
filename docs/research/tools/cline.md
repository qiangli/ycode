# Cline - Tools & Security Analysis

**Project:** Cline (autonomous AI coding VS Code extension)
**Language:** TypeScript
**Repository:** cline/cline

---

## Tools (Function Calling) - 24+ tools

### File Operations (7)
| Tool | Description |
|------|-------------|
| `read_file` | Read file contents |
| `write_to_file` | Create or overwrite files |
| `replace_in_file` | Edit existing file content |
| `apply_patch` | Apply unified diffs |
| `search_files` | Search contents with regex |
| `list_files` | List directory contents |
| `list_code_definition_names` | Get symbol/definition names via language indexing |

### Command Execution (1)
| Tool | Description |
|------|-------------|
| `execute_command` | Run shell commands with permission validation |

### Browser Automation (1)
| Tool | Description |
|------|-------------|
| `browser_action` | Control Puppeteer browser (launch, click, type, scroll, close) |

### Web Access (2)
| Tool | Description |
|------|-------------|
| `web_search` | Search the web |
| `web_fetch` | Fetch URL content |

### Agent Coordination (4)
| Tool | Description |
|------|-------------|
| `use_skill` | Load and activate skills |
| `use_subagents` | Spawn up to 5 parallel research subagents |
| `new_task` | Create and track subtasks |
| `attempt_completion` | Signal task completion |

### Interaction (1)
| Tool | Description |
|------|-------------|
| `ask_followup_question` | Query user for clarification |

### System/Workflow (8)
| Tool | Description |
|------|-------------|
| `condense` | Summarize context for token efficiency |
| `summarize_task` | Generate task summaries |
| `plan_mode_respond` | Respond in planning mode |
| `act_mode_respond` | Respond in action mode |
| `generate_explanation` | Explain code changes |
| `new_rule` | Create custom rules/guidelines |
| `report_bug` | Report extension issues |
| `focus_chain` | Task progress tracking (TODO) |

### MCP Integration (3)
| Tool | Description |
|------|-------------|
| `use_mcp_tool` | Execute MCP server tools |
| `access_mcp_resource` | Access MCP data |
| `load_mcp_documentation` | Load MCP server docs |

---

## Security & Guardrails

| Mechanism | Description |
|-----------|-------------|
| **CommandPermissionController** | JSON config with allow/deny glob patterns, redirect blocking |
| **Auto-approval system** | Per-action granular control (readFiles, editFiles, executeSafeCommands, etc.) |
| **YOLO mode** | Auto-approve all (opt-in extreme trust) |
| **Path validation** | Resolves relative paths, validates workspace boundaries |
| **ClineIgnoreController** | Respect `.clineignore` patterns |
| **Shell operator detection** | Blocks backticks, newlines, unsafe separators outside quotes |
| **Redirect blocking** | Blocks `>`, `>>`, `<`, `>&` unless explicitly allowed |
| **Long-running detection** | Auto-timeout: 300s for builds, 30s default |
| **Content filtering** | Model-specific fixes, content size limits, consecutive mistake counting |
| **File ops safety** | Diff pre-validation, patch parsing, atomic writes, backup on edit |
| **Multi-root workspace** | Path resolution across multiple workspace roots |

### Tool Approval Matrix
- **Read tools:** readFiles + readFilesExternally settings
- **Write tools:** editFiles + editFilesExternally settings
- **Bash:** executeSafeCommands or executeAllCommands
- **Browser/Web:** useBrowser setting
- **MCP:** useMcp setting, per-tool autoApprove

---

## Notable Patterns

- **Model-variant tool definitions:** GENERIC, NATIVE_GPT_5, NATIVE_NEXT_GEN, GEMINI_3, etc.
- **Hook system:** User prompt submit hooks for input interception

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| Browser automation (Puppeteer) | Not implemented | **High** - unique capability |
| `.clineignore` / `.ycodeignore` | Not implemented | **Medium** - privacy feature |
| Auto-approval system with per-action granularity | Not implemented | **Medium** - UX improvement |
| Shell redirect blocking | Not implemented | **Medium** - security |
| Long-running command detection | Not implemented | Low |
| Consecutive mistake counting | Not implemented | Low |
