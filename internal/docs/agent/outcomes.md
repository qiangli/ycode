---
topic: outcomes
summary: outcome_class taxonomy on browser results
when: you saw a non-SUCCESS outcome_class and need to know what to do
audience: agent
max_lines: 90
---

Every `BrowserResult` from a `browser_*` MCP call carries an
`outcome_class` string. It is the reliability layer's one-shot
classification of what happened, separate from `success` (which only
tracks whether the call dispatched). Branch on `outcome_class`, not on
content, when deciding next steps.

## When to use this

- Your last `browser_*` call returned a non-SUCCESS outcome and you
  need to choose a recovery.
- You're writing a wrapper / harness around browser tools and want to
  enumerate the classes you must handle.

## The four classes

`SUCCESS`
: Default. The call dispatched and no blocking hint matched. Continue.

`AUTH_REDIRECT`
: A `login_wall` hint matched the page content (sign-in / log-in
  language on a short page). The user is not authenticated for this
  surface. Recovery: see `ycode docs browser` AUTH_REDIRECT row —
  `browser_extract` the form, `browser_type` + `browser_click` to
  drive it, or `browser_cookies_get` / `browser_storage_get` on a
  logged-in tab and replay. If you can switch to `live` mode you
  inherit the user's real session.

`BLOCKED`
: One of three sub-hints fired: `captcha_detected`,
  `cloudflare_challenge`, or `rate_limited`. Also returned when the
  call errored or `success: false`. Recovery depends on the sub-hint:
  - captcha / cloudflare → switch to `live` if you have a real
    fingerprint there, otherwise surface URL to the user.
  - rate_limited → back off; do not retry the same URL for at least
    several seconds.
  - error / 5xx → do not retry immediately; surface URL + timestamp.

`SILENT_CLICK`
: A `click` action returned success but the page didn't change
  (empty content + no URL). The selector matched something that did
  nothing, or the SPA route swapped under you. Recovery: re-query
  the DOM (`browser_extract` with a fresh scope, or
  `browser_wait_for_selector` before the next click).

## Where the classifier lives

`internal/runtime/mcpservers/reliability/hints.go`. Hints are matched
first (substring scans on page content), then `classifyOutcome` maps
matching hint prefixes to a class. Adding a new class requires:

1. Emit a hint with a new prefix from a rule in `hints.go`.
2. Map that prefix to the new class in `classifyOutcome`.
3. Document it here. The parity test `TestOutcomeDocCoverage` will
   fail the build if step 3 is skipped.

## Exact calls

- Inspect the most recent outcome: any `browser_*` MCP response
  carries `outcome_class` at the top of the result envelope.
- Confirm the hint that fired: same envelope, `hints` field.
- See recovery patterns per class: `ycode docs browser`.
