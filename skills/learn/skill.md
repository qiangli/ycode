---
name: learn
description: Study a prior-art project or new topic, produce gap analysis, plan, and TODO
user_invocable: true
---

# /learn — Study Prior Art and Plan Adoption

Analyze a reference project (existing or new) to identify features this project can adopt. Produces three deliverables: gap analysis, implementation plan, and tracking TODO.

`{{ARGS}}` is required. It is either:
- A project name that exists under `priorart/` (e.g., `aider`, `cline`, `opencode`)
- A topic or technology to research (e.g., `mcp protocol`, `code review`, `terminal ui`)

If `{{ARGS}}` is empty, ask the user what they want to study.

## Mode Detection

Determine which mode to use:

1. Run `ls priorart/` to get the list of existing prior-art projects.
2. If `{{ARGS}}` matches a directory name under `priorart/` (case-insensitive), use **Mode 1: Study Existing Project**.
3. Otherwise, use **Mode 2: Research New Topic**.

---

## Mode 1: Study Existing Project

The target project is `priorart/{{ARGS}}`.

### Step 1: Explore the reference project

Thoroughly analyze the target project's codebase. Focus on these domains:

- **Architecture**: directory structure, main entry points, module boundaries, language/frameworks
- **Session and memory management**: conversation history, context window, persistence, compaction
- **Internal tools/skills/agents**: built-in tools, agent orchestration, delegation, skill systems
- **Messaging and communication**: event systems, queues, inter-component messaging, protocols
- **Observability**: tracing, metrics, logging, OTEL integration, dashboards
- **Other notable features**: anything novel or well-engineered worth adopting

Read the project's README, main entry points, configuration files, and key source directories. Take notes on every significant feature domain.

Use parallel exploration where possible -- launch multiple searches or reads concurrently to cover different parts of the codebase efficiently.

### Step 2: Explore this project's current state

For each feature domain identified in Step 1, examine this project's implementation:

- Check `internal/`, `pkg/`, `cmd/` for the corresponding code
- Read existing docs: `docs/architecture.md`, `USAGE.md`, and relevant `docs/*.md` files
- Note what this project already has, what is partially implemented, and what is missing
- Note where this project is **ahead** of the reference project

Run the two exploration steps (Step 1 and Step 2) **in parallel** when possible.

### Step 3: Write gap analysis

Before writing, read at least one existing `docs/gap-analysis-*.md` file to match the formatting conventions.

Create `docs/gap-analysis-{{ARGS}}.md` with this structure:

```
# Gap Analysis: <This Project> vs <Reference Project>

<1-2 sentence overview. Note the reference project's language/framework vs this project's stack.>

---

## 1. <Domain Name>

### What This Project Already Has
<Bullet list of existing capabilities in this domain>

### Gaps Identified

| # | Feature | <Reference> | This Project Status | Priority |
|---|---------|-------------|---------------------|----------|
| <ID> | **<Feature name>** | <How reference does it> | <Current state> | High/Medium/Low |

---

## 2. <Next Domain>
...
```

Use short IDs that group by domain initial:
- S1, S2... for **S**ession/memory
- T1, T2... for **T**ools/agents/orchestration
- M1, M2... for **M**essaging/communication
- O1, O2... for **O**bservability/OTEL
- Other prefixes as appropriate for additional domains

Assign priority based on:
- **High**: Core workflow improvement, significant user value, or architectural prerequisite
- **Medium**: Enhances existing feature, nice-to-have improvement
- **Low**: Minor enhancement, cosmetic, or niche use case

### Step 4: Write implementation plan

Create `docs/plan-{{ARGS}}-gaps.md` with this structure:

```
# Implementation Plan: Closing <Reference Project> Gaps

## Phased Approach

Work is organized into N phases. Each phase builds on the previous one.
Within each phase, items can be worked in parallel.

**Scoping decisions:**
- <Features explicitly deferred and why>
- <Features where this project's approach is preferred>

---

## Phase 1: <Theme> — <Goal>

### 1.1 <Feature Name> (S/M/L) — Gap <ID>
- <Implementation steps>
- <Key files to create/modify>
- <Dependencies on other items>

### 1.2 <Next Feature>
...

---

## Phase 2: <Theme>
...

---

## Deferred

| # | Feature | Reason |
|---|---------|--------|
```

Effort estimates: **S** = small (< 1 day), **M** = medium (1-3 days), **L** = large (3-5 days).

Order phases so foundational/infrastructure work comes first and user-facing features build on it. Each item should reference the gap ID from the analysis.

### Step 5: Write tracking TODO

Create `docs/todo-{{ARGS}}-gaps.md` with this structure:

```
# TODO: <Reference Project> Gap Implementation

Tracking checklist. See gap-analysis-{{ARGS}}.md for full analysis.

---

## Phase 1: <Theme>

> Goal: <one-line goal>

- [ ] **<ID> — <Feature Name>** (<effort>)
  - [ ] <Sub-task 1>
  - [ ] <Sub-task 2>
  - [ ] Unit tests

---

## Phase 2: <Theme>
...

---

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
```

### Step 6: Report and pause

Summarize to the user:
- Number of domains compared
- Number of gaps found (by priority breakdown)
- Domains where this project is ahead
- Paths to the three output documents

Then ask: **"Would you like to begin implementation? If so, which phase(s)?"**

Do **not** start implementing until the user explicitly approves.

### Step 7: Implement (only if approved)

If the user approves implementation:

1. Read the TODO file for the approved phase(s)
2. Work through items in order, respecting dependencies
3. After completing each item, mark it `[x]` in the TODO with the commit hash
4. Run tests after each item (`go test -short -race ./path/to/package/`)
5. Commit each logical unit of work separately
6. After completing a phase, report progress and ask before continuing to the next

---

## Mode 2: Research New Topic

### Step 1: Understand the topic

Parse `{{ARGS}}` as a topic or technology. If the intent is ambiguous, ask the user to clarify. For example:
- "mcp" — does this mean the Model Context Protocol spec, or a specific MCP implementation?
- "review" — code review tooling, or PR review automation?

### Step 2: Search for candidate projects

Search for well-known, highly-starred GitHub projects related to the topic. Look for:

- **Permissive license required**: only consider projects under BSD, MIT, or Apache 2.0 licenses. Reject GPL, AGPL, LGPL, SSPL, BSL, or any copyleft/source-available license.
- Projects with 1000+ stars (prefer well-maintained, recently active)
- Projects that are implementation references (not just docs or specs)
- Projects in any language (the goal is to learn patterns, not copy code)
- Diversity of approach (pick projects that solve the problem differently)

If web search or GitHub search is unavailable, ask the user to provide candidate project URLs manually.

Propose 3-5 candidate projects to the user:

```
### <Project Name>
- **URL**: https://github.com/<owner>/<repo>
- **License**: <MIT/BSD-3-Clause/Apache-2.0>
- **Stars**: <count>
- **Language**: <primary language>
- **Last active**: <approximate date>
- **Why relevant**: <1-2 sentences on what this project can learn from it>
```

### Step 3: Verify license and get user approval

Before proposing, verify each candidate's license by checking the repository's `LICENSE` file or GitHub metadata. **Only propose projects with a permissive license (MIT, BSD, or Apache 2.0).** If a promising project has a non-permissive license, mention it as excluded and state the reason.

Ask which project(s) to add. **Wait for explicit approval.** Do not proceed without it.

### Step 4: Add as git submodule

For each approved project:

```bash
git submodule add <url> priorart/<short-name>
git add .gitmodules priorart/<short-name>
git commit -m "feat: add <short-name> to priorart for <topic> study"
```

Use a short, lowercase name for the directory (matching existing convention: `aider`, `cline`, `codex`, `openclaw`, `opencode`, `openhands`, `geminicli`, `clawcode`, `continue`).

### Step 5: Proceed as Mode 1

Execute Mode 1 Steps 1-7 against the newly added project.

---

## Rules

- **Do not skip steps.** Execute each step in order. Analysis must be thorough before writing deliverables.
- **Do not fabricate features.** Only document capabilities verified by reading actual source code.
- **Preserve existing documents.** If output files already exist, read them first. Ask the user whether to update or create a fresh version.
- **Be honest about strengths.** The gap analysis must note domains where this project is ahead, not just where it lags.
- **Scope aggressively.** The plan should explicitly defer low-priority items and explain why. Not every gap needs to be closed.
- **Match existing doc style.** Read at least one existing `docs/gap-analysis-*.md`, `docs/plan-*.md`, and `docs/todo-*.md` before writing new ones. Follow their formatting conventions.
- **Agent-agnostic.** Do not rely on any tool-specific capabilities. Use standard file operations, git commands, and shell. If web search is unavailable in Mode 2, fall back to asking the user for URLs.
- **No implementation without permission.** After producing the three documents, stop and ask before implementing anything.
- **Permissive licenses only.** Only add projects licensed under MIT, BSD, or Apache 2.0 as submodules. Never add GPL, AGPL, LGPL, SSPL, BSL, or other copyleft/source-available licensed projects.
