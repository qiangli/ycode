---
name: claude
description: Run Claude Code CLI with a single prompt and return the output
user_invocable: true
---

# /claude — Run Claude Code CLI

Run `claude -p` with the user's prompt. One Bash call, one response.

`{{ARGS}}` is the prompt text. It is required -- if empty, ask the user what to run.

## Execution

Run this single command:

```bash
which claude 2>/dev/null && claude -p "{{ARGS}}" || { found=$(ls ~/.npm-global/bin/claude ~/.local/bin/claude /usr/local/bin/claude 2>/dev/null; ls ~/.nvm/versions/node/*/bin/claude 2>/dev/null); if [ -n "$found" ]; then echo "FOUND_NOT_IN_PATH: $found"; else echo "NOT_INSTALLED"; fi; }
```

- **Normal output**: display it to the user. Done.
- **`FOUND_NOT_IN_PATH: <path>`**: tell the user to add the parent directory to PATH (`export PATH="<dir>:$PATH"` in `~/.zshrc`).
- **`NOT_INSTALLED`**: tell the user to install: `npm install -g @anthropic-ai/claude-code` (requires Node.js 18+).

Pass `{{ARGS}}` exactly as given. Do not modify it. Use up to 10 minute timeout.
