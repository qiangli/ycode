# Cline - Tools, Agents, Skills & Security Analysis

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

## Agents / Subagents

| Component | Description |
|-----------|-------------|
| **SubagentBuilder** | Configures autonomous subagent instances with model overrides and system prompts |
| **SubagentRunner** | Executes up to 5 parallel subagents, tracks token usage |
| **AgentConfigLoader** | Loads agent configurations from files with tool allowlists |
| **Default subagent tools** | Read-only: FILE_READ, LIST_FILES, SEARCH, LIST_CODE_DEF, BASH (readonly), USE_SKILL |

---

## Skills

| Component | Description |
|-----------|-------------|
| **Discovery** | Scans `~/.cline/skills/` (global) and `.cline/skills/` (project) |
| **Format** | Subdirectory with `SKILL.md` file, YAML frontmatter (name, description) |
| **Activation** | Via `use_skill` tool, instructions injected into context |
| **Toggles** | Global and project-level skill enable/disable |

### Slash Commands (Built-in)
`/newtask`, `/smol` (`/compact`), `/newrule`, `/reportbug`, `/deep-planning`, `/explain-changes`

### Custom Workflows
File-based from `.cline/workflows/` and `~/.cline/workflows/`, plus remote workflows.

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
- **PromptRegistry:** Singleton managing all system prompts with variant system
- **gRPC/Protobuf communication** between extension and webview
- **Hook system:** User prompt submit hooks for input interception

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| Browser automation (Puppeteer) | Not implemented | **High** - unique capability |
| `list_code_definition_names` (symbol indexing) | Partial (LSP exists) | Low - LSP covers this |
| Parallel subagents (up to 5) | Implemented (Agent tool) | Done |
| `.clineignore` / `.ycodeignore` | Not implemented | **Medium** - privacy feature |
| Model-variant tool definitions | Not implemented | Low - over-engineering for CLI |
| Auto-approval system with per-action granularity | Not implemented | **Medium** - UX improvement |
| Custom workflows (file-based) | Skills cover this | Done |
| Shell redirect blocking | Not implemented | **Medium** - security |
| Long-running command detection | Not implemented | Low |
| Consecutive mistake counting | Not implemented | Low |
