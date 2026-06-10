---
name: web-research
description: Anonymous web research using the solo browser mode (fresh isolated Chrome)
user_invocable: true
---

# /web-research — Anonymous web research

Use this skill when the task is **read-only web research** with no
need for the user's logged-in session: fetch a page, extract content,
follow links, fill non-credential forms. Recommended mode: **`solo`**
— a fresh isolated Chrome launched by ycode.

## Pre-flight

1. Set the mode if it isn't already:
   `ycode config set browser.mode solo`
2. Confirm a Chrome binary is available:
   `ycode browser doctor` should report `solo available=true`.

## Tools

- `browser_navigate {url}` — load a URL
- `browser_extract` — get text + interactive elements list (preferred over screenshot)
- `browser_click {selector | element_id}` — click a link/button
- `browser_type {selector, text}` — fill an input
- `browser_screenshot` — base64 PNG (use sparingly)
- `browser_back` / `browser_tabs` — history + tab management

## Guardrails

- Do not navigate to `file://`, `javascript:`, or `data:` URLs.
- Do not enter credentials. If a page requires login, stop and
  recommend switching to `/web-loggedin` (live mode).
- Do not submit forms that have side effects without explicit user
  confirmation in the same turn.
- Watch for `hints` like `captcha_detected`, `cloudflare_challenge`,
  `rate_limited` — if you see them, stop or back off; do not retry
  blindly.

## Termination

Once you have the user's answer, stop. Solo sessions are cheap but
not free.
