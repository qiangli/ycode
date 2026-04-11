# OpenHands - Tools, Agents, Skills & Security Analysis

**Project:** OpenHands (formerly OpenDevin)
**Language:** Python (backend) + React (frontend)
**Repository:** All-Hands-AI/OpenHands

---

## Tools (Function Calling) - 15+ action types

### Bash/Command Execution
| Tool | Description |
|------|-------------|
| `execute_bash` (CmdRunAction) | Linux bash with persistent sessions, soft/hard timeouts, interactive STDIN |

### Python Execution
| Tool | Description |
|------|-------------|
| `execute_ipython_cell` | IPython/Jupyter cell execution with magic commands and persistent kernel |

### File Operations
| Tool | Description |
|------|-------------|
| `str_replace_editor` (FileEditAction) | String replacement editor: view, create, str_replace, insert, undo_edit |
| `edit_file` (LLM-based) | LLM content generation with partial edits and line ranges |

### Web Browsing
| Tool | Description |
|------|-------------|
| `browser` (BrowseInteractiveAction) | Programmatic browser: forms, navigation, clicks, drag-and-drop |
| `web_read` | Webpage to markdown conversion |
| `browse` (BrowseURLAction) | URL fetching and page retrieval |

### Agent Control
| Tool | Description |
|------|-------------|
| `think` (AgentThinkAction) | Internal reasoning logging |
| `finish` (AgentFinishAction) | Task completion signal |
| `delegate_to_browsing_agent` | Agent-to-agent delegation |
| `condensation_request` | Memory compression trigger |

### Task Management
| Tool | Description |
|------|-------------|
| `task_tracker` (TaskTrackingAction) | Plan, view, update, clear task lists |

### MCP Integration
| Tool | Description |
|------|-------------|
| MCPAction | Dynamic MCP tool discovery and invocation |

### Security Risk Levels per Tool
| Level | Value | Behavior |
|-------|-------|----------|
| LOW | 0 | Safe, auto-execute |
| MEDIUM | 1 | Confirm if confirmation_mode enabled |
| HIGH | 2 | Always confirm |
| UNKNOWN | -1 | Default, needs evaluation |

---

## Agents / Subagents

### Primary Agents (V0 - Currently Active)
| Agent | Description |
|-------|-------------|
| **CodeActAgent** | Main all-purpose agent: bash, file editing, Python, browsing |
| **BrowsingAgent** | Specialized web browsing and information retrieval |
| **VisualBrowsingAgent** | Screenshot-based browser interaction |
| **ReadOnlyAgent** | Safe exploration: grep, glob, view, think, finish, web_read |
| **LocAgent** | Lines-of-code specialized variant |
| **DummyAgent** | Test/stub agent |

### Microagents (Domain-Specific)
- Keyword-triggered knowledge agents
- Repository-specific agents from `.openhands/microagents/repo.md`
- Auto-loaded per repository context

### V1 Modern Architecture
- Software Agent SDK (separate repo)
- Application server architecture
- Replaces legacy V0 agent system

### Delegation Pattern
- Root task → subtask hierarchy
- State tracking with delegate levels
- Global and local iteration counters
- CodeActAgent → BrowsingAgent delegation

---

## Skills (26 shared microagents)

| Skill | Description | Triggers |
|-------|-------------|----------|
| `github.md` | GitHub PR/issue management | GitHub keywords |
| `gitlab.md` | GitLab integration | GitLab keywords |
| `azure_devops.md` | Azure DevOps integration | Azure keywords |
| `bitbucket.md` | Bitbucket integration | Bitbucket keywords |
| `docker.md` | Docker usage guidelines | Docker keywords |
| `kubernetes.md` | K8s deployment | K8s keywords |
| `ssh.md` | SSH operations | SSH keywords |
| `security.md` | Security best practices | Security keywords |
| `code-review.md` | Code review workflow | Review keywords |
| `fix_test.md` | Test fixing workflow | Test keywords |
| `add_agent.md` | Create new microagents | "new agent", "create agent" |
| `add_repo_inst.md` | Generate repo instructions | - |
| `agent_memory.md` | Memory/context management | - |
| `agent-builder.md` | Build custom agents | - |
| `npm.md` | NPM package management | NPM keywords |
| `onboarding.md` | User onboarding | - |
| `pdflatex.md` | PDF/LaTeX handling | LaTeX keywords |
| `address_pr_comments.md` | Address PR comments | - |
| `update_pr_description.md` | Update PR descriptions | - |
| `update_test.md` | Test updating workflow | - |
| `swift-linux.md` | Swift on Linux | Swift keywords |
| `default-tools.md` | Default tools docs | - |
| Plus 4 more specialized skills | | |

### Skill Structure (YAML frontmatter)
```yaml
name: skill_name
type: knowledge | repo
version: 1.0.0
agent: CodeActAgent
triggers: [keyword1, keyword2]
```

---

## Security & Guardrails

### Security Analyzer Framework (3 analyzers)
| Analyzer | Description |
|----------|-------------|
| **LLM Risk Analyzer** | Uses LLM-provided security_risk attribute (default) |
| **Invariant Analyzer** | Detects secret leaks, malicious commands, harmful content |
| **Gray Swan Analyzer** | External Cygnal API for advanced AI safety monitoring |

### Action Confirmation System
| Status | Description |
|--------|-------------|
| CONFIRMED | User approved |
| REJECTED | User declined |
| AWAITING_CONFIRMATION | Pending response |

### Runtime Sandboxing (4 types)
| Runtime | Description |
|---------|-------------|
| **Docker** (default) | Container isolation per session, full environment control |
| **Local** | Direct host execution (no isolation - dev only) |
| **Remote** | Distributed HTTP-based execution |
| **Kubernetes** | Container orchestration, scalable |

### Additional Security
| Mechanism | Description |
|-----------|-------------|
| **Input validation** | Type checking, required params, JSON parsing with FunctionCallValidationError |
| **Output truncation** | Long command results truncated |
| **Process management** | Timeouts (soft 10s, hard configurable), signal handling |
| **Plugin requirements** | PluginRequirement class for dependency declaration |
| **File operation sandboxing** | Path-based access within container |
| **"Almost stuck" detection** | Recovery mechanism when agent is stuck |

---

## Notable Patterns

- **Action-Observation loop:** Event-driven execution: Action → Observation → State update
- **Multi-runtime architecture:** Abstract Runtime base → Docker/Local/Remote/K8s implementations
- **Distributed execution:** HTTP-based ActionExecutionServer separates agent logic from execution
- **Multi-agent state:** Complex delegation with global/local iteration counters
- **MCP integration:** Dynamic tool discovery from MCP servers
- **Dual architecture:** V0 (stable) + V1 (SDK-based modern) coexist during migration
- **Keyword-triggered skills:** Conversation content activates domain expertise

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| IPython/Jupyter execution tool | Not implemented | **Medium** - useful for data work |
| Browser automation (BrowseInteractive) | Not implemented | **High** - web research |
| ReadOnlyAgent (safe exploration) | Implemented (Explore agent) | Done |
| Security analyzer framework (pluggable) | Not implemented | **High** - extensible security |
| Docker runtime sandboxing | Not implemented | **High** - critical for safety |
| Invariant analyzer (secret leak detection) | Not implemented | **Medium** |
| Action risk levels (LOW/MEDIUM/HIGH) | Not implemented | **Medium** - per-tool risk |
| Keyword-triggered skills | Not implemented | **Medium** - auto-activation |
| "Almost stuck" detection | Not implemented | **Medium** - recovery |
| Multi-runtime abstraction | Not applicable | N/A - CLI runs locally |
| Condensation request tool | Implemented (session compaction) | Done |
| Task tracker tool | Implemented (TodoWrite/Tasks) | Done |
