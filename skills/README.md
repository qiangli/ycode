# Skills

Reusable, tool-agnostic slash commands for AI coding assistants.

## Convention

```
skills/
  <name>/
    skill.md      # frontmatter + instructions
```

Each `skill.md` has YAML frontmatter:

```yaml
---
name: build
description: Build the project, fix errors, commit on success
user_invocable: true
---
```

## Dispatch Rule

Include this paragraph verbatim in your AI tool's instruction file
(CLAUDE.md, .cursorrules, .github/copilot-instructions.md, etc.):

> **Skills**: When the user's message starts with `/<name>` (e.g. `/build`,
> `/deploy`), read `skills/<name>/skill.md` and follow its instructions
> exactly. Everything after `/<name> ` (the rest of the message) is `ARGS` --
> pass it to the skill wherever the skill references `{{ARGS}}`. If the skill
> does not use `{{ARGS}}` and `ARGS` is non-empty, ignore it. If no matching
> skill exists, tell the user. To list available skills, run:
> `ls skills/*/skill.md`.

## Available Skills

| Command     | Description |
|-------------|-------------|
| `/autopilot`| Autonomously analyze agentic tools, identify gaps in 3 domains, implement, test, commit |
| `/build`    | Build binary with full quality checks, fix errors, commit on success |
| `/claude`   | Run Claude Code CLI with a single prompt |
| `/deploy`   | Deploy to host, ensures build first |
| `/learn`    | Study a prior-art project or new topic, produce gap analysis, plan, and TODO |
| `/setup`    | Set up the ycode development environment and verify the build |
| `/validate` | Run integration/smoke/acceptance tests against running instance |

## Adding a Skill

1. Create `skills/<name>/skill.md` with the frontmatter above.
2. Write instructions as markdown. The AI reads and follows them literally.
3. Use `{{ARGS}}` anywhere the skill needs the user's input text
   (e.g. a prompt, a target host, a file path).
4. Add the command to the table in this README.
5. No tool-specific config changes needed -- the dispatch rule handles discovery.
