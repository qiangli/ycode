---
name: canvas
description: Render generative widgets onto the ycode canvas — telemetry dashboards, dependency graphs, incident overlays, A2UI surfaces — via the agent_render_widget and agent_render_a2ui MCP tools.
---

# /canvas — Generative UI on the ycode canvas

The ycode canvas at `/canvas/` (default session `canvas-default`) is the generative-UI service: agent-rendered interactive widgets stream into a sandboxed iframe host shell over the bus. This skill is the recipe book for when and how to use the two canvas tools.

## When to use which tool

**`agent_render_widget`** — raw HTML in a sandboxed iframe. Use when the visual answer needs expressiveness a declarative component catalog can't reach.

Reach for it when answering:
- "Show me p99 latency for the last 6h" → Chart.js line chart
- "How does X connect to the rest of the codebase?" → D3 force-directed graph
- "Why is test Y failing?" → animated step-through of the failing call path
- "Visualize this trace span" → timeline + flame view
- Any custom Three.js / D3 / Chart.js / Canvas visualization

**`agent_render_a2ui`** — declarative A2UI v0.9 ops (createSurface / updateComponents / updateDataModel) over a structured component catalog. Use for surfaces with bidirectional state (data lives separately, can be patched independently) and standard UI primitives (cards, lists, forms, KPI rows).

Reach for it when answering:
- "Show me the service health canvas" → SLO bars + dep map + incident list as a structured surface
- "Organize my memos by current context" → memo cluster view (cards in columns)
- "Show me open todos" → kanban board

Both tools accept an optional `session_id`. Omit it for the default `canvas-default` session — the one the `/canvas/` page subscribes to with no `?session=` query param. Foreign agents driving the canvas via MCP should also default to it unless the user gives a specific session.

## Recipe: ad-hoc telemetry dashboard

User: "show me p99 auth latency for the last 6h"

1. Decide PromQL: `histogram_quantile(0.99, sum(rate(http_request_duration_seconds_bucket{service="auth"}[5m])) by (le))`
2. Call `promql_query_range` (existing telemetry MCP tool):
   ```json
   { "query": "<promql>", "start": "-6h", "end": "now", "step": "5m" }
   ```
3. The result has shape `{"resultType": "matrix", "result": [{"metric": {...}, "values": [[ts, "v"], ...]}]}`.
4. Build a standalone HTML doc with Chart.js. Inline the data — the iframe is sandboxed and offline-safe except for declared CDN imports. Skeleton:
   ```html
   <!DOCTYPE html><html><head>
     <script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
     <style>body{margin:0;padding:16px;font:14px -apple-system,sans-serif;}canvas{max-height:300px;}</style>
   </head><body>
     <h3>p99 auth latency · last 6h</h3>
     <canvas id="c"></canvas>
     <script>
       const data = /* inline the (timestamp,value) pairs as JSON */;
       new Chart(document.getElementById('c'), {
         type: 'line',
         data: { labels: data.map(d=>new Date(d[0]*1000).toLocaleTimeString()),
                 datasets: [{ label:'p99', data: data.map(d=>+d[1]), borderColor:'#4f46e5' }] },
         options: { animation: false, responsive: true }
       });
     </script>
   </body></html>
   ```
5. Call `agent_render_widget`:
   ```json
   { "widget_id": "p99-auth-6h", "html": "<full doc>" }
   ```
6. Tell the user "Rendered on /canvas/" and stop. Don't paste the data in chat — the widget is the answer.

## Recipe: alert-driven incident overlay (auto-publishes, you can enrich)

When `EventAlertFired` fires on the bus, ycode's `AlertHook` automatically publishes a static incident overlay onto `canvas-default`. You can replace it with a richer one by calling `agent_render_widget` with `widget_id="incident-<alertName>"` (matching what the hook emits — same ID replaces in place).

Recipe for an enriched incident overlay:
1. Read the alert name, severity, summary, labels from the alert payload (or `list_alerts`).
2. Query correlated context:
   - `search_victorialogs` for log lines matching `service=<label> AND time:[<startsAt>:]`
   - `query_traces` for `error_spans` matching the service
   - `git log --since=<startsAt-1h> -10` for recent commits
3. Compose an enriched HTML overlay with the alert header + a "Recent context" panel listing the correlated rows.
4. Render with the same `widget_id` so it replaces the static one.

## Recipe: declarative surface with bidirectional state (A2UI)

User: "open the service health canvas"

1. `createSurface` with a stable ID:
   ```json
   { "version": "v0.9", "createSurface": { "surfaceId": "health", "catalogId": "https://a2ui.org/specification/v0_9/basic_catalog.json" } }
   ```
2. `updateComponents` defines the layout (root Column containing a Row of KPI cards + a List of services + a List of recent incidents).
3. `updateDataModel` patches in live data from your telemetry queries:
   ```json
   { "version": "v0.9", "updateDataModel": { "surfaceId": "health", "path": "/", "value": { "slos": [...], "services": [...], "incidents": [...] } } }
   ```
4. Call `agent_render_a2ui` with the ops array.
5. On user click events (when the bidirectional state.mutate path is wired in v2), patch the data model with the new selection — don't rebuild the whole surface.

## Notes & limits

- **Strong-model only.** Weak models produce broken HTML / malformed A2UI ops. If running on a small Ollama model, prefer prose responses; if you must render, keep widgets minimal and test the HTML mentally before emitting.
- **No external `<img>` URLs you didn't verify exist.** Sandboxed iframe forgives missing scripts but a broken image makes the widget look amateurish. Prefer inline SVG, data URIs, or Google favicons (`https://www.google.com/s2/favicons?domain=...&sz=128`).
- **Iframes are sandboxed** with `allow-scripts` only — no allow-same-origin. The widget can't reach back into the canvas page, only postMessage. Don't rely on cross-frame data access.
- **Re-emit to update.** Same `widget_id` or `surfaceId` replaces in place. Use this for progressive rendering: emit a skeleton first, then update once the query completes.
- **Bidirectional state.mutate isn't wired yet (v2).** Buttons in A2UI surfaces can declare action events, but ycode doesn't forward them back to the agent yet — design for one-way rendering until that lands.
