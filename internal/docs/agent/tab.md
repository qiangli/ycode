---
topic: tab
summary: drive the user's live Chrome tab in real time
when: the user is logged into a real session you need to inspect or act on, and a headless browser can't reach it
audience: agent
max_lines: 100
---

`yc tab` drives the Chrome tab the user is currently sitting in,
through ycode's live-mode hub. It is the bridge to surfaces a
headless browser can't reach: anything behind OAuth/SSO, anything
that depends on the user's existing cookies, anything where a
captcha or 2FA would prompt the user mid-flow.

Unlike `yc browser` (which can run probe/solo/headless modes against
a fresh browser instance), `yc tab` always targets the **already-open
tab** in the user's session. The hub at `127.0.0.1:58082` (override
`YCODE_LIVE_PORT`) routes every command to that tab.

## When to use this

- The page requires the user's auth state (corporate SSO, GitHub
  org membership, a customer dashboard) and the user is already
  signed in.
- You want to take a screenshot of what the user is currently looking
  at to ground your next suggestion.
- You need to drive a multi-step form using the user's selections —
  copy clipboard text, click a button, scroll to see what changed.
- The page makes a request that a probe-mode browser would fail
  because of fingerprinting / anti-bot heuristics. The live tab is
  the user's actual browser.

If the page is public and stateless, prefer `yc browser fetch` —
faster, no hub round-trip, no dependency on the live mode being up.

## Tool surface

Every subcommand hits the local hub's `/dispatch` endpoint and prints
JSON-shaped output suitable for piping to `jq`.

| Subcommand | What it does |
|---|---|
| `yc tab status` | Hub reachable? Which tab is connected? — start here |
| `yc tab extract` | Structured page extraction (title, headings, links, main text) |
| `yc tab screenshot [path]` | PNG of the visible viewport |
| `yc tab navigate <url>` | Drive the tab to a new URL |
| `yc tab click <selector>` | CSS-selector click |
| `yc tab type <selector> <text>` | Focus + type (joins remaining args as text) |
| `yc tab scroll [up\|down] [px]` | Default: down 500px |
| `yc tab back` | History back |
| `yc tab tabs [list\|switch\|new\|close] [index]` | Tab management |

## Failure modes

| Symptom | Fix |
|---|---|
| `connection refused` to 127.0.0.1:58082 | Live mode isn't running. Tell the user to enable it (`ycode browser` setup or `ycode serve` with live mode on). |
| `no tab connected` | The browser extension isn't talking to the hub. User needs to focus the target tab and re-enable the extension. |
| `selector not found` | Page may have lazy-rendered; try `yc tab scroll` first, or check with `yc tab extract`. |
| Screenshot is blank | Tab may be on `chrome://` or another protected URL; navigate first. |

## Exact calls

- Confirm hub is up: `yc tab status`
- See what's on screen: `yc tab extract | jq .title`
- Grab a screenshot: `yc tab screenshot /tmp/page.png`
- Drive a form: `yc tab type 'input[name=q]' 'search text'`
- Click a button by selector: `yc tab click 'button.submit'`
- Scroll: `yc tab scroll down 1200`
- Navigate: `yc tab navigate https://example.com/path`
- List tabs: `yc tab tabs list`
- Switch tab: `yc tab tabs switch 2`
