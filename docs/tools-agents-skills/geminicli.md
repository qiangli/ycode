# Gemini CLI - Tools, Agents, Skills & Security Analysis

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

## Agents / Subagents

### Built-in Local Agents (5)
| Agent | Description | Timeout | Max Turns |
|-------|-------------|---------|-----------|
| **generalist** | General-purpose, all tools, non-interactive | 10 min | 20 |
| **codebase_investigator** | Read-only code analysis, low temperature (0.1), structured JSON output | 10 min | 50 |
| **cli_help** | CLI documentation agent | - | - |
| **browser_agent** | Autonomous web browser via Chrome accessibility tree (experimental) | 10 min | 30 |
| **memory_manager** | Long-term memory persistence, scoped workspace | - | - |

### Remote Agents (A2A Protocol)
| Feature | Description |
|---------|-------------|
| **A2A protocol** | Agent-to-Agent execution via URL-based agent cards |
| **Authentication** | OAuth2, API Key, HTTP, Google Credentials |
| **Identity verification** | Hash-based agent acknowledgment |
| **Agent cards** | URL or inline JSON format |

---

## Skills (11 built-in)

| Skill | Description |
|-------|-------------|
| `async-pr-review` | Asynchronous PR review with GitHub integration |
| `behavioral-evals` | Test/evaluation framework |
| `ci` | CI/CD integration |
| `code-reviewer` | Structured code review analysis |
| `docs-changelog` | Documentation changelog generation |
| `docs-writer` | Documentation writing assistance |
| `github-issue-creator` | Automated GitHub issue creation |
| `pr-address-comments` | Automated PR comment addressing |
| `pr-creator` | Automated PR creation |
| `review-duplication` | Code duplication detection |
| `string-reviewer` | Text content review |

### Skill Architecture
- Precedence: Built-in < Extensions < User < Workspace
- Activation via `activate_skill` tool
- Script-based or tool-based implementation

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
- **ReAct loop:** Agent execution via LocalAgentExecutor with configurable turns
- **Agent-as-Tool:** Agents callable via `delegate_to_agent`
- **Policy-as-Code:** TOML-based policy files with HMAC integrity
- **Message bus:** Async tool-UI communication with correlation IDs
- **Tool kinds:** Read, Edit, Delete, Move, Search, Execute, Think, Agent, Fetch, etc.

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| Browser agent (Chrome automation) | Not implemented | **High** - unique capability |
| A2A remote agents | Not implemented | **Medium** - future interop |
| Policy-as-Code (TOML with HMAC) | Not implemented | **Medium** - tamper-proof policies |
| Codebase investigator agent | Partial (Explore agent) | Low - similar concept |
| `read_many_files` (batch read) | Implemented (`read_multiple_files`) | Done |
| `ask_user` with multi-select | Partial (AskUserQuestion) | Low |
| Folder trust system | Not implemented | **Medium** - security |
| Safety checker plugins | Not implemented | **Medium** - extensible validation |
| `modify_with_editor` approval option | Not implemented | Low |
| Tool kind classification | Not implemented | Low |
| Memory manager agent | Partial (memory system exists) | Low |
| `save_memory` tool | Partial (skill-based) | Low |
| 11 built-in skills | 6 skills implemented | **Medium** - add more skills |
