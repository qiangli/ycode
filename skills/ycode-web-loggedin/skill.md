---
name: web-loggedin
description: Drive the user's real Chrome (cookies, SSO, fingerprint) via the live mode (ycode-owned extension)
user_invocable: true
---

# /web-loggedin — Drive the user's real Chrome

Use this skill when the task **requires the user's logged-in
session**: Gmail, internal SSO apps, banking, sites that aggressively
detect fresh-Chromium fingerprints. Recommended mode: **`live`** —
a ycode-owned MV3 extension over a local WebSocket.

## Pre-flight (one-time)

1. Extract the extension:
   `ycode browser setup live`
2. Open `chrome://extensions`, toggle **Developer mode**, click
   **Load unpacked**, point at the printed path.
3. Pin the extension.
4. Set the mode:
   `ycode config set browser.mode live`

## Pre-flight (each session)

The ycode-live extension drives whichever tab you connected it to.
Click the extension icon on the target tab and click **Connect**.
The popup goes green when the WebSocket is up.

## When NOT to use

- Anonymous research → use `/web-research` (solo, cheaper, isolated).
- DevTools / perf debugging → use `/web-debug` (probe).

## Tools

Standard `browser_*` actions. They all target the connected tab.

## Privacy + safety

You are operating inside the user's **real** browser session.

- **Treat every page as confidential.** Do not paste page content
  into external services, summaries shared off-machine, or
  long-lived agent memory unless the user has explicitly approved.
- **Do not follow links you don't need.** Each click happens in the
  user's authenticated context.
- **Never submit forms with side effects** (send email, transfer
  money, publish, delete) without an explicit confirmation from the
  user **in the same turn**.
- **Stop on auth prompts.** If a page asks for credentials or 2FA, do
  not type anything — return control to the user.

## Termination

Disconnect as soon as the task is done. Do not keep an automation
session attached to a logged-in tab any longer than necessary.
