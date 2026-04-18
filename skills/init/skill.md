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

Check whether AGENTS.md and/or CLAUDE.md already exist.

- If **AGENTS.md exists**: read it. You will update it in-place in Step 4,
  preserving verified useful guidance and removing stale content.
- If **CLAUDE.md exists**: read it. You will reference it from AGENTS.md and
  follow its instructions alongside AGENTS.md.

## Step 4: Generate AGENTS.md

Write AGENTS.md using the Write tool (new file) or Edit tool (updating existing).

AGENTS.md is the **primary instruction file** for AI coding assistants
working in this repository. Keep it minimal and high-signal.

### Required content:

**Header**: `# AGENTS.md` followed by a 1-sentence project description.

**Reference to USAGE.md**: If `USAGE.md` exists, add a line:
`**Read [USAGE.md](./USAGE.md)** for detailed instructions on build commands,
configuration, tools, and workflows.`

**Reference to CLAUDE.md**: If `CLAUDE.md` exists, add a line:
`**Read [CLAUDE.md](./CLAUDE.md)** for additional project conventions and
Claude Code-specific guidance.`

**Quick commands**: Only include build/test/lint commands if USAGE.md does
NOT exist or does not cover them. If USAGE.md already documents these,
a reference is sufficient — do not duplicate.

**Repo-specific guidance**: Include only facts an agent would miss without
help and that are not already covered by USAGE.md or CLAUDE.md:
- Non-obvious architectural boundaries or entrypoints
- Testing quirks or integration prerequisites
- Conventions that differ from language/framework defaults

### Writing rules:

- Every line should answer: "Would an agent likely miss this without help?"
- Keep it compact — prefer under 30 lines. Reference USAGE.md and CLAUDE.md
  for details instead of duplicating content.
- Exclude generic software advice and obvious language conventions.
- Exclude speculative claims or anything not verified from source files.
- Do not fabricate features or commands — only document what you verified.
- If {{ARGS}} is non-empty, give extra attention to the requested focus area.

## Step 5: Report

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
