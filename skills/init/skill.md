---
name: init
description: Initialize ycode workspace and generate context-aware CLAUDE.md and AGENTS.md
user_invocable: true
---

# /init — Initialize Project

Set up the ycode workspace and generate high-signal CLAUDE.md and AGENTS.md
by analyzing the actual project structure, configuration, and workflows.

User-provided focus or constraints (honor these if non-empty):
{{ARGS}}

## Step 1: Scaffold workspace artifacts

Create the deterministic workspace structure:

1. Create `.agents/ycode/` directory: `mkdir -p .agents/ycode`
2. Create `.agents/ycode.json` if it does not exist. Detect the project's
   languages, frameworks, package manager, and build/test/lint commands from
   manifest files (go.mod, package.json, Cargo.toml, pyproject.toml, Makefile,
   etc.). Write a JSON config like:
   ```json
   {
     "permissions": { "defaultMode": "dontAsk" },
     "project": { "languages": [...], "frameworks": [...] },
     "build": { "command": "..." },
     "test": { "command": "..." },
     "lint": { "command": "..." }
   }
   ```
3. Update `.gitignore` — append these entries if missing (under a
   `# ycode local artifacts` comment):
   ```
   .agents/ycode.json
   .agents/ycode/settings.local.json
   .agents/ycode/sessions/
   .agents/ycode/cache/
   .agents/ycode/logs/
   ```

## Step 2: Investigate the project

Read the highest-value sources first. Read files in parallel where possible.

**Always read (if they exist):**
- README.md, README.rst, README.txt (or any README*)
- Root manifests: package.json, go.mod, go.work, Cargo.toml, pyproject.toml,
  requirements.txt, Makefile, Justfile
- Workspace/monorepo config: pnpm-workspace.yaml, lerna.json, nx.json,
  turbo.json, rush.json
- Lockfiles (just check which one exists — don't read contents):
  package-lock.json, yarn.lock, pnpm-lock.yaml, bun.lockb, go.sum, Cargo.lock
- Build/tool config: tsconfig.json, .eslintrc*, prettier.config*,
  .prettierrc*, biome.json, vitest.config*, jest.config*, .swcrc
- CI workflows: .github/workflows/*.yml, .gitlab-ci.yml, Jenkinsfile,
  .circleci/config.yml
- Pre-commit/task runner: .pre-commit-config.yaml, .husky/*, .lefthook.yml,
  Taskfile.yml
- Existing instruction files: CLAUDE.md, AGENTS.md, INSTRUCTIONS.md,
  .cursor/rules/*, .cursorrules, .github/copilot-instructions.md
- Docker/container: Dockerfile*, docker-compose*.yml, compose*.yml

**If architecture is still unclear**, inspect a small number of representative
source files to find the real entrypoints, package boundaries, and execution
flow. Prefer files that show how the system is wired together over random
leaf files.

Prefer executable sources of truth (Makefile, CI config, package.json scripts)
over prose documentation. If docs conflict with config, trust the executable
source and only keep what you can verify.

## Step 3: Check for existing files

Check whether CLAUDE.md and/or AGENTS.md already exist.

- If **neither exists**: generate both from scratch (Step 4 and 5).
- If **one or both exist**: read the existing content. Update them in-place —
  preserve verified useful guidance, remove fluff or stale claims, and
  reconcile with the current codebase.

## Step 4: Generate CLAUDE.md

Write CLAUDE.md using the Write tool (new file) or Edit tool (updating existing).

### Required sections:

**Header**: `# CLAUDE.md` followed by a 1-2 sentence project description.

**Build/Test/Lint Commands**: The exact commands to build, test, and lint.
Include single-file and single-package invocation patterns when they exist
(e.g., `go test -run TestFoo ./pkg/bar/`). Include the required command
order if it matters (e.g., `fmt → vet → build → test`).

**Project Structure**: Only the directories and files that matter for
understanding the architecture. Not an exhaustive tree — focus on what an
agent needs to navigate the codebase. Include entrypoints, module boundaries,
and ownership of major directories.

**Development Workflow**: Non-obvious workflow requirements: environment
setup, required services, database migrations, codegen steps, dev server
commands, port assignments.

**Testing**: Testing quirks, fixture locations, integration test
prerequisites, snapshot workflows, flaky suites, required services.

**Code Conventions**: Repo-specific style rules that differ from language
defaults. Import ordering, naming conventions, error handling patterns,
logging conventions — only what an agent would get wrong without being told.

**CI/CD**: What the CI pipeline checks. Required checks that must pass
before merge. Deploy process if relevant.

### Writing rules:

- Every line should answer: "Would an agent likely miss this without help?"
  If not, leave it out.
- Prefer commands and code over prose.
- Keep it compact — under 150 lines for small projects, under 300 for large.
- Exclude generic software advice and obvious language conventions.
- Exclude speculative claims or anything not verified from source files.
- If {{ARGS}} is non-empty, give extra attention to the requested focus area.
- Do not fabricate features or commands — only document what you verified.

## Step 5: Generate AGENTS.md

Write AGENTS.md using the Write tool (new file) or Edit tool (updating existing).

AGENTS.md is the tool-agnostic version — guidance useful to any AI coding
assistant (Claude Code, OpenCode, Cursor, Copilot, etc.).

Structure mirrors CLAUDE.md but:
- Omit Claude-specific references
- Reference CLAUDE.md for Claude-specific details
- Keep it even more compact than CLAUDE.md
- Focus on the highest-signal facts: exact commands, architecture boundaries,
  and conventions that differ from defaults

Apply the same writing rules as CLAUDE.md.

## Step 6: Report

Summarize:
- Which files were created or updated
- Key findings: languages, frameworks, notable architectural decisions
- Any important information that could not be determined — mention it so the
  user can fill in gaps manually

## Rules

- Do not overwrite user-customized content without reading it first
- Do not fabricate features or commands — only document what you verified
- Do not include generic boilerplate that any developer would already know
- Prefer short sections and bullets
- If the project is simple, keep the files simple
- If {{ARGS}} is empty, that is fine — analyze the full project
