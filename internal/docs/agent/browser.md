---
topic: browser
summary: drive a real or headless browser via ycode's MCP tools
when: navigate, fill forms, extract page state; AUTH_REDIRECT or BLOCKED
audience: agent
max_lines: 160
---

ycode exposes one unified browser-automation surface across three
backend modes. Choose a mode before any other call — the rest of the
tools share the same verbs and same `BrowserResult` envelope (text,
JSON `data`, plus `hints` + `outcome_class`).

## When to use this

- The user asks to "open", "click", "fill", "screenshot", "scrape",
  or "verify" anything in a webpage.
- You see `outcome_class: "AUTH_REDIRECT"` or `"BLOCKED"` and need a
  recovery path (see Recovery patterns below).
- You need a screenshot or computed style of an element you just
  edited, to verify a frontend change end-to-end.

## Modes

| Mode    | What it drives                             | Pick when |
|---------|--------------------------------------------|-----------|
| `live`  | The user's real Chrome via ycode's MV3 extension | You need the user's logged-in session (cookies, SSO). |
| `probe` | An existing Chrome started with `--remote-debugging-port` | You want CDP power on a Chrome you started yourself. |
| `solo`  | A fresh chromedp-launched Chrome           | You want full isolation; no user session, no extension. |

Configure with `browser.mode` in settings.json. Run
`ycode browser doctor` to diagnose readiness; `ycode browser setup`
once per mode (live extracts the extension).

## Tool families

- **Navigation:** `browser_navigate`, `browser_back`, `browser_tabs`
- **Interaction:** `browser_click`, `browser_type`, `browser_scroll`,
  `browser_keyboard_press`, `browser_wait_for_selector`
- **Extraction:** `browser_extract` (text + a11y), `browser_eval`
  (JS), `browser_screenshot`, `browser_console_get`,
  `browser_network_list`, `browser_lighthouse`
- **Session/state:** `browser_cookies_get`, `browser_storage_get`,
  `browser_clipboard_read`, `browser_clipboard_write`
- **Introspection:** `browser_capabilities` (probe for supported
  methods + extension permissions; call once per session to detect
  stale-extension drift)

## browser_eval quirks

The script body is evaluated as a **statement**, not a bare
expression. `({foo:1})` returns null. To return a value:

1. Single expression: `document.title`
2. IIFE that returns: `(()=>{ return {foo:1}; })()`
3. JSON.stringify at the top: `JSON.stringify({foo:1})`

Execution context: probe/solo run via CDP `Runtime.evaluate` in the
page's main world; live runs via `chrome.scripting.executeScript`
with `world: 'MAIN'`. In both, there are no `chrome.*` APIs and
`window` persists across calls within the same page lifetime.

The MCP arg name is `script`; `expression` is accepted as an alias.

## Recovery patterns

| outcome_class | What it means | Recovery |
|---|---|---|
| `AUTH_REDIRECT` | Page wants a sign-in | (a) `browser_extract` scope=form to inspect inputs; (b) `browser_type` + `browser_click` to drive the form; (c) `browser_cookies_get` / `browser_storage_get` on a logged-in tab to replay. If not in `live` mode and the user wants their real session, switch via `ycode browser doctor`. |
| `BLOCKED` (captcha) | Cloudflare / hCaptcha interstitial | Switch to `live` mode if you have a real fingerprint there; otherwise back off and surface the URL to the user. |
| `BLOCKED` (rate_limited) | 429 / "too many requests" | Back off; do not retry the same URL within seconds. |
| `BLOCKED` (server_error) | 5xx page | Do not retry; surface URL and timestamp. |
| `SILENT_CLICK` | Click succeeded but page didn't change | Re-query the DOM; the target may have moved or been replaced by an SPA route. Consider `browser_wait_for_selector` before the next interaction. |

See `ycode docs outcomes` for the full classifier reference.

## Failure modes

| Symptom | Fix |
|---|---|
| "Browser tools are not available" | `browser.mode` unset; set it in settings.json. |
| `live` mode times out | Extension stale or not loaded; `ycode browser install-extension`, then reload Chrome. |
| `probe` cannot connect | Chrome not started with `--remote-debugging-port`; `ycode browser launch`. |
| `evaluate: argument 'script' is required` | Pass `script` (or alias `expression`) in the args object. |
| `browser_eval` returns `null` for an object | You wrote a statement, not an expression — wrap in an IIFE that returns. |
| `browser_screenshot` blob too big | Pass `max_bytes` (~200000 is safe) — backend spills to a file path returned in `path` when over the cap. |

## Exact calls

- Diagnose modes: `ycode browser doctor`
- One-time setup (live extension): `ycode browser setup live`
- Start Chrome for probe: `ycode browser launch`
- Open Chrome and sign in manually (the dedicated `ycode browser login` is a stub today — prints "not yet implemented"; use `launch` and sign in yourself): `ycode browser launch`
- Navigate (any mode): MCP `browser_navigate` with `{url, max_bytes?}`
- Read a computed style:
  `browser_eval` with `{script: "(()=>{const e=document.querySelector('.foo');return JSON.stringify(getComputedStyle(e).color);})()"}`
- Restore an existing session: `browser_cookies_get` on logged-in tab → save → `browser_navigate` on target page (live mode re-uses cookies automatically; the read tools matter mostly for solo/probe replay).
- Capabilities probe: MCP `browser_capabilities` with `{}` (returns the live extension's permitted `chrome.*` APIs + supported method list).
