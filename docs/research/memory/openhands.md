# Memory Management in OpenHands (priorart/openhands/)

This document summarizes how OpenHands (the Python autonomous AI agent platform, formerly OpenDevin) handles multi-turn conversations, context engineering, harness engineering, and memory management.

---

## 1. Architecture Overview

OpenHands is a Python-based autonomous agent platform with sandboxed execution (Docker/K8s). Its distinguishing feature is a **sophisticated condenser pipeline** with 9 condenser types that can be chained together, plus a **microagent system** for keyword-triggered knowledge injection.

---

## 2. Agent Architecture

### CodeActAgent (Main Agent)

File: `openhands/agenthub/codeact_agent/codeact_agent.py`

Version: `2.2`

**Available Tools** (configurable):
- `CmdRunTool` — bash execution
- `ThinkTool` — internal reasoning
- `FinishTool` — task completion
- `CondensationRequestTool` — request memory condensation
- `BrowserTool` — web browsing
- `IPythonTool` — Jupyter/Python execution
- `TaskTrackerTool` — task management
- `LLMBasedFileEditTool` — LLM-based file editing
- `StrReplaceEditorTool` — standard file editing

### Step Method Flow

1. Return pending actions from queue if available
2. Check for `/exit` command
3. Get condensed history via condenser
4. Gather initial user message + full message history
5. Call LLM with messages and tools
6. Convert response to actions
7. Queue actions and return first one

---

## 3. State Management

### State Class

File: `openhands/controller/state/state.py`

Key attributes:
- `history: list[Event]` — all events (actions/observations) chronologically
- `agent_state: AgentState` — LOADING, RUNNING, PAUSED, STOPPED, FINISHED, etc.
- `iteration_flag: IterationControlFlag` — step limit tracking
- `budget_flag: BudgetControlFlag` — token/cost budget tracking
- `delegate_level: int` — agent hierarchy level (0=root, +1 per delegation)
- `metrics: Metrics` — global metrics (costs, tokens)
- `extra_data: dict` — includes `condenser_meta` for condensation metadata
- `confirmation_mode: bool` — require action confirmation

### Control Flags

**Iteration limit**: `current_value` incremented per step, raises error at `max_value`
**Budget limit**: `current_value` tracks spend, raises error at `max_value`
Both support `limit_increase_amount` for dynamic expansion.

---

## 4. Multi-Turn Loop (AgentController)

File: `openhands/controller/agent_controller.py`

### Core Step Execution

1. **Pre-step checks**: verify RUNNING state, no pending actions
2. **Budget/iteration sync**: check control flags
3. **Stuck detection**: `_is_stuck()` raises `AgentStuckInLoopError`
4. **Replay vs live**: use recorded action or call `agent.step(state)`
5. **Error handling**: catch LLM errors, trigger condensation on `ContextWindowExceededError`
6. **Security checks**: validate actions in `confirmation_mode`
7. **Execute**: run action in runtime, capture observation

### Event-Driven Architecture

```
User Message → on_event() → should_step() → _step() → agent.step(state)
    → Action → execute in runtime → Observation → on_event() → next step
```

### Delegation/Multi-Agent

- Creates child `AgentController` for delegation
- Posts `MessageAction` with task
- Parent waits for delegate to complete (FINISHED/ERROR/REJECTED)

---

## 5. Condenser System (Memory Compaction)

### Architecture

File: `openhands/memory/condenser/`

The condenser system is OpenHands' most sophisticated feature. 9 condenser types can be used individually or chained in a pipeline.

### View Class (Memory Snapshot)

File: `openhands/memory/view.py`

```python
class View:
    events: list[Event]
    unhandled_condensation_request: bool = False
    forgotten_event_ids: set[int] = set()
```

`View.from_events()` filters out forgotten events and inserts summaries at specified offsets.

### 9 Condenser Types

**1. No-Op** — Returns view unchanged

**2. Observation Masking** (`attention_window: int = 100`)
- Replaces old observation content with `<MASKED>` placeholder
- Recent observations within window remain unmasked

**3. Browser Output Masking** (`attention_window: int = 1`)
- Same as above but specifically for browser outputs

**4. Recent Events** (`keep_first: int = 1, max_events: int = 100`)
- Keeps only initial + recent N events
- Drops old middle events entirely

**5. LLM Summarizing** (`keep_first: 1, max_size: 100, max_event_length: 10000`)
- Most commonly used sophisticated condenser
- Identifies head (keep_first) events to preserve
- Targets `max_size // 2` for kept events
- LLM generates structured summary covering:
  - USER_CONTEXT, TASK_TRACKING, COMPLETED, PENDING
  - CURRENT_STATE, CODE_STATE, TESTS, CHANGES
  - DEPS, VERSION_CONTROL_STATUS
- Returns `CondensationAction` with forgotten IDs + summary

**6. LLM Attention** (`max_size: 100, keep_first: 1`)
- Uses LLM with structured output to rank events by importance
- Returns top K most important events
- Uses `response_schema` for JSON-structured ranking

**7. Amortized Forgetting**
- Gradually forgets events without summarization

**8. Structured Summary** (most sophisticated)
- Uses function calling to force structured JSON generation
- `StateSummary` schema with typed fields:
  - `user_context`, `completed_tasks`, `pending_tasks`, `current_state`
  - `files_modified`, `function_changes`, `data_structures`
  - `tests_written`, `tests_passing`, `failing_tests`, `error_messages`
  - `branch_created`, `branch_name`, `commits_made`, `pr_created`, `pr_status`
  - `dependencies`, `other_relevant_context`
- Highly queryable and consistent vs free-form summaries

**9. Pipeline** — chains multiple condensers in sequence
```yaml
type: 'pipeline'
condensers:
  - type: 'observation_masking'
    attention_window: 100
  - type: 'llm_attention'
    max_size: 100
```
Short-circuits if any condenser returns Condensation.

### Triggering

- `should_condense(view)` — checks if event count exceeds `max_size` or unhandled condensation request exists
- `ContextWindowExceededError` → triggers `CondensationRequestAction` if `enable_history_truncation=true`
- Agent can explicitly request via `CondensationRequestTool`

### Metadata Tracking

Condensation metadata stored in `state.extra_data['condenser_meta']` as list of dicts containing:
- LLM response, metrics (tokens, cost)
- Forgotten event count, summary text
- Timestamps

---

## 6. Prompt Engineering

### System Prompt

File: `openhands/agenthub/codeact_agent/prompts/system_prompt.j2`

Jinja2 template with sections:
- ROLE, EFFICIENCY, FILE_SYSTEM_GUIDELINES, CODE_QUALITY
- VERSION_CONTROL, PULL_REQUESTS, PROBLEM_SOLVING_WORKFLOW
- SECURITY, SECURITY_RISK_ASSESSMENT, EXTERNAL_SERVICES
- ENVIRONMENT_SETUP, TROUBLESHOOTING, DOCUMENTATION

Prompt variants: `system_prompt_interactive.j2`, `system_prompt_tech_philosophy.j2`, `system_prompt_long_horizon.j2`

### Prompt Manager

File: `openhands/utils/prompt.py`

- `get_system_message(**context)` — renders system prompt with Jinja2
- `build_workspace_context()` — repo, runtime, instructions info
- `build_microagent_info()` — triggered microagents content
- `add_turns_left_reminder()` — iteration count reminder in messages

### Conversation Memory

File: `openhands/memory/conversation_memory.py`

`ConversationMemory.process_events()` converts event history into LLM messages:
1. Ensures system message exists
2. Ensures initial user message
3. Processes each action → tool calls or messages
4. Processes each observation → tool results
5. Applies prompt caching if active

---

## 7. Microagent System

File: `openhands/microagent/microagent.py`

### 3 Microagent Types

**1. KnowledgeMicroagent**
- Triggered by keyword matching in conversation
- Provides specialized knowledge/best practices
- YAML frontmatter with `triggers: [keyword1, keyword2]`
- `match_trigger(message)` checks for keyword presence

**2. RepoMicroagent**
- Loaded from `.openhands/microagents/repo.md` or `.openhands_instructions`
- Always active for that repository
- Repository-specific guidelines and team practices

**3. TaskMicroagent**
- Triggered by `/{agent_name}` format
- Requires user input variables (`${variable_name}`)
- Structured task execution with inputs

### Microagent Format

```markdown
---
name: python_testing
type: knowledge
version: 1.0
triggers:
  - pytest
  - unittest
---
# Python Testing Best Practices
When writing tests: ...
```

### Loading

Also loads third-party instruction files:
- `.cursorrules` (Cursor IDE)
- `AGENTS.md`, `agent.md` (case-insensitive)

---

## 8. Runtime / Sandbox

Available runtimes:
- **Docker** — container-based execution (primary)
- **Local** — local machine execution
- **CLI** — CLI tool execution
- **Remote** — remote server execution
- **Kubernetes** — K8s cluster execution

Actions executed in sandboxed environment with captured output as Observations.

---

## 9. Key Constants

| Component | Default | Description |
|-----------|---------|-------------|
| Iteration limit | Configurable | `IterationControlFlag.max_value` |
| Limit increase | 100 | `limit_increase_amount` |
| LLM Summarizer max_size | 100 events | Trigger condensation |
| LLM Summarizer max_event_length | 10,000 chars | Per-event truncation |
| Observation Masking window | 100 events | Keep recent unmasked |
| Browser Masking window | 1 event | Keep only latest |
| Recent Events keep_first | 1 | Preserve initial events |
| Recent Events max_events | 100 | Maximum total events |
| Agent version | 2.2 | CodeActAgent |

---

## 10. Comparison with ycode

| Feature | OpenHands | ycode |
|---------|-----------|-------|
| **Condenser** | 9 types + pipeline chaining | 3-layer (prune → compact → flush) |
| **Summarization** | LLM-based structured summary | Heuristic 7-field summary |
| **Microagents** | Keyword-triggered knowledge injection | CLAUDE.md instruction files |
| **Stuck detection** | Built-in loop detection | None |
| **Budget control** | Iteration + cost limits | Token threshold only |
| **Runtime** | Docker/K8s sandboxed | Local bash |
| **Delegation** | Multi-agent hierarchy | Recursive subagents |
| **Tool: condensation** | Agent can request condensation | No self-managed compaction |
| **Event model** | Action/Observation events | User/Assistant/Tool messages |

### Key Features ycode Could Adopt

1. **LLM-based summarization** — use model for structured summaries instead of heuristic extraction
2. **Condenser pipeline** — chain multiple compaction strategies (mask → summarize → trim)
3. **Structured summary schema** — typed fields for queryable summaries
4. **Agent-requested condensation** — tool that lets the agent request its own memory compaction
5. **Stuck detection** — detect repetitive agent behavior and break loops
6. **Microagent/knowledge injection** — keyword-triggered context injection from knowledge files
7. **Budget control flags** — iteration + cost limits with dynamic expansion
8. **Observation masking** — mask old tool outputs before full compaction

---

*This analysis is based on the OpenHands codebase as of April 2025.*
