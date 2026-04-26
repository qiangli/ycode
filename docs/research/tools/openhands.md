# OpenHands - Tools & Security Analysis

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

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| IPython/Jupyter execution tool | Not implemented | **Medium** - useful for data work |
| Browser automation (BrowseInteractive) | Not implemented | **High** - web research |
| Security analyzer framework (pluggable) | Not implemented | **High** - extensible security |
| Docker runtime sandboxing | Not implemented | **High** - critical for safety |
| Invariant analyzer (secret leak detection) | Not implemented | **Medium** |
| Action risk levels (LOW/MEDIUM/HIGH) | Not implemented | **Medium** - per-tool risk |
| "Almost stuck" detection | Not implemented | **Medium** - recovery |
