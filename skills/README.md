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
| `/build`    | Build binary with full quality checks, fix errors, commit on success |
| `/deploy`   | Deploy to host, ensures build first |
| `/validate` | Run integration/smoke/acceptance tests against running instance |
| `/claude`   | Run Claude Code CLI with a single prompt |
| `/commit`   | Plan and commit local changes with a convention-following message |

## Adding a Skill

1. Create `skills/<name>/skill.md` with the frontmatter above.
2. Write instructions as markdown. The AI reads and follows them literally.
3. Use `{{ARGS}}` anywhere the skill needs the user's input text
   (e.g. a prompt, a target host, a file path).
4. Add the command to the table in this README.
5. No tool-specific config changes needed -- the dispatch rule handles discovery.
