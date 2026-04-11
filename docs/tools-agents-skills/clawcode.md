# Claw Code - Tools, Agents, Skills & Security Analysis

**Project:** Claw Code (reference implementation for ycode)
**Language:** Rust
**Repository:** ultraworkers/claw-code

---

## Tools (Function Calling) - 50 tools

### Core File/System Tools (6)
| Tool | Description | Permission |
|------|-------------|------------|
| `bash` | Execute shell commands | DangerFullAccess |
| `read_file` | Read text files | ReadOnly |
| `write_file` | Write text files | WorkspaceWrite |
| `edit_file` | Replace text in files | WorkspaceWrite |
| `glob_search` | Find files by glob pattern | ReadOnly |
| `grep_search` | Search file contents with regex | ReadOnly |

### Web & Communication Tools (4)
| Tool | Description | Permission |
|------|-------------|------------|
| `WebFetch` | Fetch URLs, convert to text | ReadOnly |
| `WebSearch` | Search the web | ReadOnly |
| `SendUserMessage/Brief` | Send messages to user | ReadOnly |
| `AskUserQuestion` | Ask user and wait for response | ReadOnly |

### Workflow & Planning Tools (4)
| Tool | Description | Permission |
|------|-------------|------------|
| `TodoWrite` | Update structured task list | WorkspaceWrite |
| `Skill` | Load/invoke skill definitions | ReadOnly |
| `Agent` | Launch specialized agent tasks | DangerFullAccess |
| `ToolSearch` | Search for deferred tools | ReadOnly |

### Notebook/Document Tools (2)
| Tool | Description | Permission |
|------|-------------|------------|
| `NotebookEdit` | Edit Jupyter notebook cells | WorkspaceWrite |
| `StructuredOutput` | Return structured output format | ReadOnly |

### Code Execution Tools (3)
| Tool | Description | Permission |
|------|-------------|------------|
| `REPL` | Execute code in REPL subprocess | DangerFullAccess |
| `PowerShell` | Execute PowerShell commands | DangerFullAccess |
| `Sleep` | Wait for duration | ReadOnly |

### Configuration & Plan Tools (3)
| Tool | Description | Permission |
|------|-------------|------------|
| `Config` | Get/set settings | WorkspaceWrite |
| `EnterPlanMode` | Enable planning mode | WorkspaceWrite |
| `ExitPlanMode` | Exit planning mode | WorkspaceWrite |

### Background Task Tools (7)
| Tool | Description | Permission |
|------|-------------|------------|
| `TaskCreate` | Create background task | DangerFullAccess |
| `RunTaskPacket` | Create task from packet | DangerFullAccess |
| `TaskGet` | Get task status | ReadOnly |
| `TaskList` | List all tasks | ReadOnly |
| `TaskStop` | Stop running task | DangerFullAccess |
| `TaskUpdate` | Send message to task | DangerFullAccess |
| `TaskOutput` | Get task output | ReadOnly |

### Worker/Subagent Tools (9)
| Tool | Description | Permission |
|------|-------------|------------|
| `WorkerCreate` | Create worker with trust gates | DangerFullAccess |
| `WorkerGet` | Get worker boot state | ReadOnly |
| `WorkerObserve` | Feed terminal snapshot | ReadOnly |
| `WorkerResolveTrust` | Resolve trust prompt | DangerFullAccess |
| `WorkerAwaitReady` | Wait for ready handshake | ReadOnly |
| `WorkerSendPrompt` | Send task to worker | DangerFullAccess |
| `WorkerRestart` | Restart worker | DangerFullAccess |
| `WorkerTerminate` | Terminate worker | DangerFullAccess |
| `WorkerObserveCompletion` | Report session completion | DangerFullAccess |

### Team & Scheduling Tools (5)
| Tool | Description | Permission |
|------|-------------|------------|
| `TeamCreate` | Create parallel subagent team | DangerFullAccess |
| `TeamDelete` | Delete team and stop tasks | DangerFullAccess |
| `CronCreate` | Create scheduled recurring task | DangerFullAccess |
| `CronDelete` | Delete scheduled task | DangerFullAccess |
| `CronList` | List scheduled tasks | ReadOnly |

### LSP & MCP Tools (5)
| Tool | Description | Permission |
|------|-------------|------------|
| `LSP` | Query Language Server Protocol | ReadOnly |
| `ListMcpResources` | List MCP server resources | ReadOnly |
| `ReadMcpResource` | Read MCP resource | ReadOnly |
| `McpAuth` | Authenticate with MCP server | DangerFullAccess |
| `MCP` | Execute MCP server tool | DangerFullAccess |

### Integration Tools (2)
| Tool | Description | Permission |
|------|-------------|------------|
| `RemoteTrigger` | Trigger remote webhooks | DangerFullAccess |
| `TestingPermission` | Test-only tool for verification | DangerFullAccess |

---

## Agents / Subagents

| Component | Description |
|-----------|-------------|
| **Agent tool** | Launches specialized agent tasks with custom prompts |
| **Agent definitions** | `.claw/agents/*.toml` with name, description, model, reasoning_effort |
| **Worker boot system** | State machine: Spawning → TrustRequired → ReadyForPrompt → Running → Finished/Failed |
| **Team coordination** | Parallel subagent teams via TeamCreate |
| **Cron scheduling** | Recurring task execution |

---

## Skills

| Component | Description |
|-----------|-------------|
| **Skill tool** | Loads SKILL.md files with frontmatter |
| **Discovery** | `.claw/skills/`, `.codex/skills/`, user-level dirs |
| **Legacy support** | `.commands/` directory format |
| **Shadowing** | Project skills override user skills with same name |

### Slash Commands (70+)
Categories: Session, Workspace/Git, Discovery/Debug, Automation, Plugin, Execution Control, Specialized.

---

## Security & Guardrails

| Mechanism | Description |
|-----------|-------------|
| **Permission modes** | ReadOnly, WorkspaceWrite, DangerFullAccess, Prompt, Allow |
| **PermissionEnforcer** | Per-tool permission checks on every invocation |
| **Bash validation** | Command intent classification (ReadOnly/Write/Destructive/Network/etc.) |
| **File boundaries** | Workspace-root isolation, path traversal prevention, 50MB limits |
| **Binary detection** | NUL byte detection prevents binary file corruption |
| **Policy rules** | Allow/Deny/Ask rules with pattern matching |
| **Hook system** | PreToolUse, PostToolUse, PostToolUseFailure events |
| **Sandboxing** | Linux namespaces, network isolation, filesystem allowlists |
| **Container detection** | Docker, Kubernetes, Podman environment detection |
| **Plugin permissions** | Read/Write/Execute per plugin |
| **MCP namespacing** | Server-prefixed tool names prevent conflicts |
| **Config hierarchy** | User → Project → Local with override precedence |

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| All 50 core tools | **Implemented** (50+ tools) | Done |
| Worker boot state machine | **Implemented** | Done |
| Team/Cron tools | **Implemented** | Done |
| MCP/LSP integration | **Implemented** | Done |
| Bash command validation/classification | Partial (basic) | **High** - need intent classification |
| Sandbox (Linux namespaces) | Not implemented | **High** - critical for safety |
| Binary file detection | Not implemented | **Medium** |
| Hook system with permission overrides | Partial (hooks exist) | **Medium** - need override capability |
| RunTaskPacket tool | Not implemented | Low |
| REPL tool | Not implemented | **Medium** - useful for data science |
| PowerShell tool | Not implemented | Low (platform-specific) |
| Container detection tuning | Basic implementation | Low |
