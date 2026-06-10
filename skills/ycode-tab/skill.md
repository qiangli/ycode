---
name: tab
description: Drive the Chrome tab connected to ycode's live mode via `yc tab` shell verbs
user_invocable: true
---

# /tab — drive the connected Chrome tab

ycode's live mode keeps a Chrome extension attached to one of the
user's tabs. `yc tab` is the bash-callable shortcut for driving that
tab from anywhere — including agents that can't speak MCP, only shell.

## Pre-flight (one line)

```bash
yc tab status
```

Exit 0 + `hub: up   extension: connected` means you're good. Anything
else: tell the user to start `bin/ycode serve` and click Connect in
the extension popup on the target tab.

## Verbs

| Command | What it does |
|---|---|
| `yc tab status` | Health check: is the hub up, is the extension connected? Exit 0 = ready. |
| `yc tab extract` | Print the page's text + the first 50 interactive elements with `[N]` indices for `yc tab click N`. Cheap, agent-friendly. |
| `yc tab screenshot [path]` | Save a PNG of the visible viewport. Defaults to `/tmp/yc-tab-<unix>.png`. Prints the path on stdout. |
| `yc tab navigate <url>` | Load a URL in the connected tab. |
| `yc tab click <selector>` | Click via CSS selector (e.g. `button.signin`, `[data-id=submit]`). |
| `yc tab type <selector> <text>` | Fill an input. |
| `yc tab scroll [up\|down] [px]` | Scroll the page. Defaults to `down 500`. |
| `yc tab back` | History back. |
| `yc tab tabs [list\|switch\|new\|close] [idx]` | Tab management. |

All commands return JSON to stdout (except `screenshot`, which prints
a path; and `status`, which prints a one-line summary). Pipe to `jq`
or just `head` for quick triage.

## Recipes

### Look at the current page

```bash
yc tab status && yc tab extract | jq -r '.result.title, .result.url' && yc tab extract | jq -r '.result.elements' | head -20
```

### Visual check

```bash
shot=$(yc tab screenshot) && open "$shot"
```

### Navigate + extract in one step

```bash
yc tab navigate https://example.com && yc tab extract | jq -r '.result.content' | head -30
```

### Diff before/after a click

```bash
yc tab extract | jq -r '.result.content' > /tmp/before.txt
yc tab click "button.load-more"
sleep 1
yc tab extract | jq -r '.result.content' > /tmp/after.txt
diff /tmp/before.txt /tmp/after.txt
```

## Safety rules

You are inside the user's **real** browser session.

- **Page content is confidential.** Don't paste it into third-party
  services, long-term memory, or anywhere the user didn't explicitly
  send it.
- **No side-effect form submits without explicit user confirmation in
  the same turn** — sending email, buying, deleting, publishing.
- **Never enter credentials.**
- **Stop on auth/2FA prompts** — return control to the user.
- **Don't click links you don't need** — each click happens in the
  user's authenticated context.

## When NOT to use this

- Anonymous research → use the `solo` mode (`/web-research`); fresh
  Chrome with no logged-in state, cheaper, safer.
- Perf traces / network waterfall / source-mapped console / JS
  evaluation → use the `probe` mode (`/web-debug`); chromedp+CDP
  gives you DevTools-grade data that `live` does not.
