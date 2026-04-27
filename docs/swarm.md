# Agent Swarm & Multi-Agent Architecture

> Reference documentation for ycode's agent swarm system: custom agents, handoff orchestration, flow types, research loops, Ralph loop, memory enhancements, and structured todo.

---

## Overview

ycode supports sophisticated multi-agent workflows through four interconnected subsystems:

1. **Custom Agent Definitions** — YAML-based user-defined agents with inheritance, model overrides, and AOP advices
2. **Swarm Orchestration** — Handoff-based control transfer, flow types, inter-agent messaging, and triggers
3. **Research & Iteration** — LLM-powered research decomposition, auto-research loops, and the Ralph loop
4. **Memory & Task Management** — Episodic/procedural memory types, cross-agent shared memory, and hierarchical todo boards

---

## Custom Agent Definitions

### Config Location

Agent definitions are loaded from YAML files in these directories (later overrides earlier):

```
~/.agents/ycode/agents/*.yaml    # global (shared across projects)
.agents/ycode/agents/*.yaml      # project-specific
```

### Native Format

```yaml
apiVersion: v1
name: code-reviewer
display: "Code Reviewer"
description: "Reviews code for quality and correctness"
instruction: |
  You are a senior code reviewer. Analyze code for bugs, style issues,
  and potential improvements. Be constructive and specific.
mode: plan          # build | plan | explore
model: claude-sonnet-4-6
tools:
  - read_file
  - grep_search
  - glob_search
max_iterations: 20
max_time: 300       # timeout in seconds
```

### ai-Swarm Format (Compatible)

The loader also supports the `qiangli/ai` swarm multi-document format:

```yaml
###
pack: "mypack"
agents:
  - name: "greeter"
    display: "Greeter Bot"
    model: "default/any"
    instruction: |
      You are a friendly greeter.
    functions:
      - "mypack:hello"
###
kit: "mypack"
tools:
  - name: "hello"
    description: "Say hello"
###
```

### Inheritance (Embed)

Agents can inherit from other definitions:

```yaml
name: base-researcher
instruction: "Research thoroughly using web search and code exploration."
tools: [read_file, grep_search, WebSearch, WebFetch]
mode: explore
---
name: security-researcher
embed: [base-researcher]
instruction: "Focus specifically on security vulnerabilities and CVEs."
tools: [bash]  # added to inherited tools
```

Inherited fields (from `embed`): instruction, context, mode, model, tools (unioned), environment (unioned), entrypoint, advices.

### Advices (AOP)

```yaml
name: safe-agent
instruction: "..."
advices:
  before: [validate-input]    # run before agent starts
  around: [timeout]           # wrap agent execution
  after: [format-output]      # run after agent completes
```

### Agent as Tool

Custom agents can be spawned via the `Agent` tool using their name as `subagent_type`:

```json
{"description": "Review PR", "prompt": "Review this diff...", "subagent_type": "code-reviewer"}
```

---

## Swarm Orchestration

### Handoff

Agents transfer control to other agents via the `Handoff` tool:

```json
{
  "target_agent": "editor-agent",
  "context_vars": {"plan": "...the plan...", "files": "src/main.go"},
  "message": "Implement the plan above"
}
```

The orchestrator detects handoff signals in tool results (via `__handoff__` JSON marker), stops the current agent, and spawns the target with context variables injected into its prompt.

**Cycle prevention:** The orchestrator tracks the handoff chain and rejects cycles (A→B→A).

### Flow Types

Entrypoints compose multiple agents/actions using flow types:

| Flow | Behavior |
|------|----------|
| `sequence` | A→B→C, each receives previous output |
| `chain` | A(B(C(input))) nested calls |
| `parallel` | A,B,C concurrent, combined results |
| `loop` | Repeat until max iterations or condition |
| `fallback` | Try A, if fails try B, if fails try C |
| `choice` | Random selection (A/B testing) |

```yaml
name: comprehensive-review
entrypoint: [security-check, style-check, logic-check]
flow: parallel
```

### Context Variables

Shared state flows between agents during handoffs:

```go
orchestrator.SetContextVar("project_root", "/path/to/project")
// All agents in the swarm can read this via their prompt
```

### Keyword Triggers

Agents auto-activate based on conversation patterns:

```yaml
name: deploy-agent
instruction: "Handle deployment tasks"
triggers:
  - pattern: "(?i)deploy|release|ship"
    max_per_turn: 1
```

### Inter-Agent Messaging

Concurrent agents communicate via the event bus:

```go
swarm.SendToAgent(bus, fromID, toID, "Found relevant file: main.go")
msg, ok := swarm.ReceiveFromAgent(bus, myID, 5*time.Second)
```

---

## Research System

### LLM-Powered Decomposition

Complex queries are decomposed into sub-tasks with a dependency DAG:

```go
plan, err := ParseDecomposition(query, llmResponse)
// plan.Tasks: sub-queries with IDs, agent types, dependencies
// plan.Ready(): tasks whose prerequisites are all completed
```

Falls back to string-based splitting if the LLM call fails.

### Auto-Research Loop

```
decompose → execute (parallel) → synthesize → identify gaps → repeat
```

Configurable: `MaxDepth` (default 2), `MaxBreadth` (default 10), `MaxParallel` (default 4).

### Research Executor

Respects dependency DAG: tasks with no unmet dependencies execute in parallel, up to `MaxParallel` concurrent tasks.

---

## Ralph Loop

The Ralph loop implements tight iterative self-improvement:

```
step → check → commit → repeat
```

### Configuration

```go
cfg := &ralph.Config{
    MaxIterations:   10,
    TargetScore:     0.95,  // stop when eval score >= 0.95
    CheckCommand:    "go test ./...",
    CommitOnSuccess: true,
    StagnationLimit: 3,     // stop if score unchanged for 3 iterations
    Timeout:         30 * time.Minute,
}
```

### Termination Conditions

1. **Target score reached** — eval score meets or exceeds threshold
2. **Stagnation** — score unchanged for N consecutive iterations
3. **Max iterations** — hard stop
4. **Context cancellation** — timeout or external cancel

### State Persistence

State is saved to JSON for crash recovery:

```go
state.Save("/path/to/ralph-state.json")
state, _ := ralph.LoadState("/path/to/ralph-state.json")
```

---

## Architect/Editor Delegation

Two-model delegation (Aider pattern):

```go
cfg := &swarm.ArchitectEditorConfig{
    ArchitectModel: "claude-haiku-4-5",    // cheap/fast for planning
    EditorModel:    "claude-sonnet-4-6",   // capable for implementation
}
result, err := swarm.RunArchitectEditor(ctx, cfg, spawner, "Add caching to the API", logger)
```

1. Architect creates a structured plan (plan mode, read-only tools)
2. Plan is validated (minimum quality check)
3. Editor implements the plan (build mode, full tools)

---

## Memory Enhancements

### New Memory Types

| Type | Description | Created By |
|------|-------------|------------|
| `episodic` | Specific agent experiences with temporal context | Auto on agent completion |
| `procedural` | Workflow patterns, decision heuristics | Manual or dreaming consolidation |
| `task` | Persistent structured task state | Todo system |

### Episodic Memory

Auto-created when agents complete, recording what happened:

```go
mem := memory.NewEpisodicMemory(&memory.EpisodicMetadata{
    AgentType:   "Explore",
    TaskSummary: "Investigated authentication flow",
    ToolsUsed:   []string{"read_file", "grep_search"},
    Duration:    45 * time.Second,
    Success:     true,
})
```

### Procedural Memory

Learned patterns promoted from repeated episodic memories:

```go
mem := memory.NewProceduralMemory(&memory.ProceduralPattern{
    Name:      "test-driven-fix",
    Description: "Fix bugs by writing a failing test first",
    Steps:     []string{"Write failing test", "Identify root cause", "Fix", "Verify test passes"},
    Context:   "When fixing bugs in Go code",
    Rationale: "Prevents regressions and validates the fix",
})
```

### Cross-Agent Shared Memory

Agents in a swarm share a `SharedMemoryView`:

```go
view := swarm.NewSharedMemoryView(50)
view.Add(swarm.SharedMemoryEntry{
    Name:    "api-structure",
    Content: "The API uses REST with JSON responses...",
    Source:  "explore-agent",
})
// All swarm agents can read this via view.FormatForPrompt()
```

---

## Task Trees & Mailbox

### Hierarchical Tasks

```go
tree := task.NewTaskTree()
root := tree.CreateRoot("Implement feature X")
child, _ := tree.CreateChild(root.ID, "Write unit tests")
```

### Inter-Task Messaging

```go
// Parent sends to child
child.Inbox.Send(task.TaskMessage{
    From: root.ID, To: child.ID, Type: "request", Payload: "Focus on edge cases",
})

// Child receives
msg, ok := child.Inbox.Receive(5 * time.Second)
```

---

## Structured Todo

### Board Operations

```go
board := todo.NewBoard()
item := board.Create("Fix auth bug", "Token refresh fails after 1h", "", 1)
board.Assign(item.ID, "fix-agent")
board.AddDependency(item.ID, otherItem.ID)
board.Update(item.ID, todo.StatusDone)
```

### Persistence

```go
board.Save("scratchpad/tasks.json")
board, _ := todo.LoadBoard("scratchpad/tasks.json")
```

### Prompt Injection

```go
markdown := board.RenderMarkdown()
// Inject into system prompt as dynamic section
```

---

## Package Map

| Package | Purpose |
|---------|---------|
| `internal/runtime/agentdef/` | Agent definition, YAML loading, registry, flow types |
| `internal/runtime/swarm/` | Orchestrator, handoff, context vars, messaging, triggers, shared memory, architect/editor |
| `internal/runtime/conversation/` | Research decomposition, executor, research loop (+ existing runtime) |
| `internal/runtime/ralph/` | Ralph loop controller, state persistence |
| `internal/runtime/task/` | Task tree, mailbox (+ existing flat registry) |
| `internal/runtime/todo/` | Hierarchical todo board, persistence |
| `internal/runtime/memory/` | Episodic, procedural memory types (+ existing 5-layer system) |
| `internal/tools/` | Handoff tool handler (+ existing Agent tool with custom agent support) |
| `internal/bus/` | EventAgentHandoff, EventAgentMessage, EventFlowStep (+ existing events) |

---

## Design Decisions

- **No import cycles**: The `tools` package cannot import `swarm` — handoff signal creation is duplicated in `tools/handoff.go` and `swarm/handoff.go` using the same JSON format.
- **Backward compatible**: Existing 6 hardcoded agent types continue to work. Custom definitions are additive.
- **YAML format detection**: The loader uses `yaml.Node` to detect document type before decoding, avoiding field conflicts between native and ai-swarm formats.
- **Context propagation**: All swarm operations use `context.Context` for cancellation and timeout.
- **Thread safety**: All registries, boards, and shared views are safe for concurrent use.
