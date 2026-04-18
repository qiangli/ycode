---
name: init
description: Initialize ycode workspace and generate context-aware AGENTS.md
user_invocable: true
---

# /init — Enhance Project Instructions

The workspace scaffold is already complete (directories, config, gitignore,
template AGENTS.md). Your job is to **review and enhance** the generated
AGENTS.md based on what the project actually contains.

This project may be written in any language or may be a data repository
with no code at all. Do not assume anything about the project type.

User-provided focus or constraints (honor these if non-empty):
{{ARGS}}

## What you must NOT do

- **NEVER** build, compile, run, or execute the project
- **NEVER** run `make`, `go build`, `npm install`, `cargo build`, or any build command
- **NEVER** run the project binary or any project scripts
- **NEVER** use Bash to run shell commands — use only built-in tools listed below
- **NEVER** install dependencies or modify project state beyond AGENTS.md

## Tool usage rules

Use ONLY these built-in tools:

- **read_file** — read file contents (not `cat`, `head`, `tail`)
- **glob_search** — find files by pattern (not `find`, `ls`, `tree`)
- **grep_search** — search file contents by regex (not `grep`, `rg`)
- **edit_file** — modify existing files
- **write_file** — create new files

Run read_file/glob_search calls **in parallel** when checking multiple files.

## Step 1: Quick project scan

Use glob_search and read_file to check which of these files exist:

- `README.md`, `USAGE.md`, `INSTRUCTIONS.md`
- `CLAUDE.md`, `AGENTS.md`
- `Makefile`, `go.mod`, `package.json`, `Cargo.toml`, `pyproject.toml`

Read only the files that exist. Skip files not found.
Read at most 5 files — prioritize README.md, USAGE.md, and the primary
manifest file.

## Step 2: Enhance AGENTS.md

Read the generated AGENTS.md. Update it using edit_file with any project-specific
additions worth keeping:

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
commands found in manifest files. If USAGE.md covers these, omit them.

**Repo-specific guidance**: Only facts an agent would miss without help:
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
