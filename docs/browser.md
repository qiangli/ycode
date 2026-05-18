# Browser modes

ycode ships a pure-Go browser automation stack with three operating
modes. All three modes feed the same `browser_*` tool surface and
share a common reliability layer ported from
[openchrome](https://github.com/shaun0927/openchrome) (MIT).

**Tool surface (21 tools)** — core navigation/interaction:
`navigate, click, type, scroll, screenshot, extract, back, tabs, eval`.
Polled primitives: `wait_for_selector, keyboard_press`.
Live-mode chrome.* reads: `clipboard_read, clipboard_write,
cookies_get, storage_get`. DevTools (probe/solo only):
`perf_start, perf_stop, network_list, console_get, lighthouse`.
Diagnostic: `capabilities`.

The modes live behind the **`experimental` build tag**, which is
**enabled by default** in `make compile` / `make build` while ycode is
pre-release (see [`docs/strategy.md`](./strategy.md#feature-tiers)).

```bash
make compile          # includes experimental
make build            # full quality gate, same tags
```

For a stable-only build (no experimental features), override the tag
list:

```bash
make compile TAG_LIST="sqlite,sqlite_unlock_notify,bindata"
```

## The three modes

| Mode | What it does | When to use | License |
|---|---|---|---|
| `live` | ycode-owned MV3 Chrome extension over a local WebSocket; drives the user's real, logged-in Chrome | Authenticated/live pages — Gmail, internal SSO, banking, anti-bot sites | Apache-2.0 (ycode-owned, sources in repo) |
| `probe` | chromedp attaches CDP to a Chrome started with `--remote-debugging-port` | Performance traces, network waterfalls, source-mapped console, Lighthouse, debugging existing Chrome | chromedp MIT |
| `solo` | chromedp launches a fresh isolated Chrome with its own profile | Anonymous research, scraping, parallel automation, anything that does not need the user's session | chromedp MIT |

## Selection

```bash
ycode config set browser.mode solo    # default for unattended work
ycode config set browser.mode probe   # for perf/debug
ycode config set browser.mode live    # for logged-in flows
```

Or per-project / per-checkout via the three-tier merge:

- User: `~/.config/ycode/settings.json`
- Project: `<repo>/.agents/ycode/settings.json`
- Local: `<repo>/.agents/ycode/settings.local.json`

## Configuration

```json
{
  "browser": {
    "mode": "solo",

    "livePort": 58082,
    "probeURL": "http://localhost:9222",

    "soloChromePath": "",
    "soloHeaded": false,
    "soloUserDataDir": "",

    "hintEngine": true,
    "ralphFallback": true,
    "circuitBreaker": true,
    "compactDOM": true,
    "patternLearner": true
  }
}
```

All `*` reliability toggles default to **on**. Set explicit `false` to
disable. The reliability layer wraps every mode uniformly.

## CLI

```bash
ycode browser setup live          # extracts MV3 extension to ~/Downloads/ycode-chrome-ext/
ycode browser launch              # starts host Chrome with --remote-debugging-port for probe
ycode browser doctor              # diagnose readiness of each mode
ycode browser install <mode>      # no-op today (kept for symmetry)
```

## Robustness recipes

### Avoid token-cap blow-ups from screenshots

Inline base64 PNGs commonly exceed 200 KB and can overflow an MCP
tool-result token budget. Always pass `max_bytes` for foreign agents:

```jsonc
{ "tool": "browser_screenshot", "input": { "max_bytes": 200000 } }
```

Behaviour:
1. If the PNG fits inline, return it as before.
2. Else re-encode as JPEG q=70 → q=50 → q=30; return the first that fits.
3. Else spill to `~/.agents/ycode/browser/screenshots/screenshot-<ts>.png`
   and return the absolute path in `path` (the inline `image` field is
   left empty). Set `save_path` to force file output regardless of size.

### Click without a perfect selector

The retrospective surfaced React buttons (DO dashboard's "Copy" /
"show") that don't have ARIA labels. Pass `match_text` and the Ralph
reliability layer tries:

1. `selector` as-given / trimmed / unquoted (when a selector is set).
2. `js-click` via `document.querySelector(selector).click()`.
3. `js-text-click` — walk visible text + click first match.
4. `extract-click-by-text` — run an extract scoped by text, then
   click `element_id=1`. Works in every mode, including live.

```jsonc
{ "tool": "browser_click", "input": { "match_text": "Copy", "scope": "main" } }
```

### Sidebar-biased extracts

`browser_extract` skips `<nav>`/`<aside>`/`[role=navigation]` /
`[role=complementary]` by default. Pass `scope: "main"` (or any CSS
selector) to constrain the query root. `match_text` filters by
visible text / placeholder / aria-label. `limit`/`offset` paginate.

```jsonc
{ "tool": "browser_extract",
  "input": { "scope": "main", "match_text": "connection string", "limit": 10 } }
```

### Stale extension drift

Whenever a server-side fix lands behind the WebSocket boundary (eg.
`a8a74f3` adding live `evaluate`), older extensions silently fall
back to "method not supported". The extension now sends a `_hello`
frame on connect with `{version, methods, permissions}`; the hub
compares against `LiveExtensionMinVersion` and prepends a hint to
every result:

```
"hints": ["live: extension stale (v0.1.0 < required v0.3.0); reload at chrome://extensions"]
```

Reload the extension after any `ycode browser setup live` to clear.

### Probe what's available before relying on it

```jsonc
{ "tool": "browser_capabilities", "input": {} }
```

Returns `{mode, version, methods, permissions}`. Foreign agents
should call this once per session — particularly before using the
chrome.*-permission tools (`clipboard_*`, `cookies_get`, `storage_get`).

## Reliability layer

Every mode is wrapped by the `mcpservers/reliability` package. Six
primitives, all toggleable, all ported from openchrome's design:

| Primitive | Purpose |
|---|---|
| **Hint Engine** | Detects CAPTCHA walls, Cloudflare, rate limits, login walls, 404s, 5xx errors, empty extractions. Annotates result with `hints`. |
| **Ralph fallback** | Click only. Retries with selector variants (trimmed, unquoted, JS-evaluate path) before giving up. |
| **Circuit breaker** | Element-level (3 fails in 2min), page-level (5 distinct fails in 5min), global (10 fails in 5min → 60s cooldown). |
| **DOM compression** | Strips `<script>`/`<style>`/`<svg>`/comments, dedupes repeated lines, collapses whitespace. |
| **Pattern Learner** | Logs (action, outcome) to `~/.config/ycode/browser-patterns.jsonl` for review and future promotion to Hint rules. |
| **Outcome Classifier** | Tags each result `SUCCESS` / `SILENT_CLICK` / `WRONG_ELEMENT` / `AUTH_REDIRECT` / `BLOCKED`. |

Source attribution: `internal/runtime/mcpservers/reliability/wrap.go`.

## Skills

Three skills wrap the typical workflows:

- [`/web-research`](../.agents/ycode/skills/ycode-web-research/skill.md) — biases toward `solo`
- [`/web-debug`](../.agents/ycode/skills/ycode-web-debug/skill.md) — biases toward `probe`
- [`/web-loggedin`](../.agents/ycode/skills/ycode-web-loggedin/skill.md) — biases toward `live`

The unified [`browser_agent`](../agents/browser.yaml) is
mode-agnostic and inspects the running configuration.

## Architecture

```
       ┌──────────────────────────────────────┐
       │ Agent-facing API                     │
       │   browser_navigate, browser_click... │
       └────────────────────┬─────────────────┘
                            ▼
       ┌──────────────────────────────────────┐
       │ Reliability layer (Go)               │
       │   Hint Engine · Ralph fallback ·     │
       │   Circuit breaker · DOM compression  │
       │   Pattern Learner · Outcome Classify │
       └────────────────────┬─────────────────┘
                            ▼
       ┌──────────────────────────────────────┐
       │ Mode dispatch (mcpservers.Manager)   │
       └──┬─────────────────┬───────────────┬─┘
          ▼                 ▼               ▼
      ┌────────┐       ┌────────┐      ┌─────────┐
      │  live  │       │ probe  │      │  solo   │
      │ Go+WS  │       │ Go+CDP │      │ Go+CDP+ │
      │ + MV3  │       │ attach │      │  Chrome │
      └────────┘       └────────┘      └─────────┘
```

Source:

- `internal/runtime/mcpservers/` — framework, types, manager
- `internal/runtime/mcpservers/{live,probe,solo}/` — mode services
- `internal/runtime/mcpservers/reliability/` — primitives
- `internal/runtime/mcpservers/live/extension/` — vanilla-JS MV3 extension
- `internal/tools/browser.go` — agent-facing tool registrations
- `internal/tools/browser_experimental.go` — manager ↔ shim adapter
- `cmd/ycode/browser.go` — `ycode browser` subcommands
- `cmd/ycode/browser_runtime.go` — wires manager + reliability

## Out of scope (today)

- Cloud / SaaS browsers — violates ycode's local-first wedge.
- Vision / screenshot understanding tool — separate capability.
- Bundled Chrome-for-Testing for `solo` — host Chrome required; the
  podman Chromium fallback is wired but not yet pullable.
- Foreign-agent MCP exposure of browser tools — Phase 3 of
  [`docs/lighthouse-roadmap.md`](./lighthouse-roadmap.md).
