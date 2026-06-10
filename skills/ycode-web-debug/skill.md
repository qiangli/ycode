---
name: web-debug
description: Inspect a page's performance, network, or runtime behavior via the probe mode (CDP attach)
user_invocable: true
---

# /web-debug — DevTools-grade debugging

Use this skill when the task needs **real DevTools data** —
performance traces, network waterfalls, source-mapped console errors,
JS evaluation. Recommended mode: **`probe`** — chromedp attached to a
debug-enabled Chrome.

## Pre-flight

1. Launch Chrome with the debug port:
   `ycode browser launch`
   (refuses if the port is already in use; pick a different port via
   `--port` or `browser.probeURL`.)
2. Set the mode:
   `ycode config set browser.mode probe`
3. Verify:
   `ycode browser doctor` should report `probe available=true`.

## When NOT to use

- Pure read-only research → use `/web-research` (solo, isolated).
- Task needs the user's *real* logged-in session in their daily-
  driver Chrome → use `/web-loggedin` (live).

## Tools

Standard `browser_*` actions work; plus DevTools-flavored actions
on the same surface:

- `browser_extract` — DOM-compressed page text
- `browser_screenshot` — base64 PNG
- The reliability layer flags pages with Cloudflare / CAPTCHA / 404
  via `hints` — read them before retrying.

For raw JS evaluation, the underlying chromedp supports it via the
`evaluate` action (Script field) — request it explicitly.

## Guardrails

- `evaluate` runs arbitrary JS in the page context. Surface the
  script to the user before running it.
- Probe attaches to whatever Chrome instance is on the debug port —
  treat its open tabs as the user's data.

## Termination

Performance traces produce a lot of data. Stop and summarize once
you have the answer.
