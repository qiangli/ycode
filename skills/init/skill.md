---
name: init
description: Initialize ycode workspace and generate context-aware AGENTS.md
user_invocable: true
---

# /init — Enhance Project Instructions

The workspace scaffold is already complete (directories, config, gitignore,
template AGENTS.md). Your job is to **review and enhance** the generated
AGENTS.md based on what the project actually contains.

User-provided focus or constraints (honor these if non-empty):
{{ARGS}}

## Tool usage rules

**Use ONLY built-in tools. NEVER use Bash for file discovery or reading.**

- Use **Glob** to find files (not `find`, `ls`, or `tree`)
- Use **Grep** to search file contents (not `grep`, `rg`, or `cat`)
- Use **Read** to read files (not `cat`, `head`, or `tail`)
- Use **Write** or **Edit** to create or modify files
- Run Glob/Read calls **in parallel** when checking multiple files

## Step 1: Quick project scan

Use Glob to check which of these files exist (one Glob call with pattern,
or parallel Read calls — do NOT run shell commands):

- `README.md`, `USAGE.md`, `INSTRUCTIONS.md`
- `CLAUDE.md`, `AGENTS.md`
- `Makefile`, `go.mod`, `package.json`, `Cargo.toml`, `pyproject.toml`

Read only the files that exist. Skip files that are not found.
Read at most 5 files total — prioritize README.md, USAGE.md, and the
primary manifest (go.mod, package.json, etc.).

## Step 2: Enhance AGENTS.md

Read the generated AGENTS.md (from the scaffold step). Update it using the
Edit tool with any project-specific additions worth keeping:

### Required content:

**Header**: `# AGENTS.md` followed by a 1-sentence project description
derived from README.md or the manifest file.

**Reference to USAGE.md**: If USAGE.md exists:
`**Read [USAGE.md](./USAGE.md)** for detailed build commands, configuration,
tools, and workflows.`

**Reference to CLAUDE.md**: If CLAUDE.md exists:
`**Read [CLAUDE.md](./CLAUDE.md)** for additional project conventions and
Claude Code-specific guidance.`

**Quick commands**: Only if USAGE.md does NOT exist — add build/test/lint
commands. If USAGE.md covers these, a reference is sufficient.

**Repo-specific guidance**: Only include facts an agent would miss:
- Non-obvious architectural boundaries
- Testing quirks or prerequisites
- Conventions differing from language defaults

### Writing rules:

- Keep it under 30 lines. Reference other docs instead of duplicating.
- Every line must answer: "Would an agent miss this without help?"
- Do not fabricate commands or features not verified from source files.

## Step 3: Report

One short paragraph: what was created, key findings, any gaps the user
should fill manually.
