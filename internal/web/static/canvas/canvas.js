// ycode canvas — vanilla-JS host shell.
//
// Subscribes to a session's bus over WebSocket. State.update events
// arrive in two formats, discriminated by payload.format:
//
//   - "iframe": create/replace a sandboxed iframe with srcdoc=html.
//     Agent uses this for free-form widgets (Chart.js dashboards,
//     D3 dep graphs, trace viewers).
//
//   - "a2ui":   forward the ops to the A2UI renderer (@a2ui/web_core
//     lands in the next task). Until then, dump the ops as a <pre>
//     block so developers can see the stream while wiring backends.
//
// Default session is "canvas-default" so foreign agents that don't
// know about ycode's session model can call agent_render_a2ui /
// agent_render_widget with no session_id and the result still shows
// up here. Override via ?session=<id>.

(function () {
  'use strict';

  const TOKEN = new URLSearchParams(location.search).get('token') || '';
  const SESSION_ID = new URLSearchParams(location.search).get('session') || 'canvas-default';

  const root = document.getElementById('canvas-root');
  const welcome = document.getElementById('welcome');
  const statusBadge = document.getElementById('status-badge');
  const sessionLabel = document.getElementById('session-label');

  sessionLabel.textContent = SESSION_ID;

  // Track rendered widgets and surfaces so re-emits replace in place
  // rather than appending (key idea: stable IDs let the agent stream
  // partial updates without flicker).
  const widgets = new Map();   // widget_id  → iframe element
  const surfaces = new Map();  // surface_id → surface container

  let ws = null;
  let backoffMs = 1000;

  function setStatus(state, text) {
    statusBadge.className = 'badge ' + state;
    statusBadge.textContent = text;
  }

  function hideWelcomeOnce() {
    if (welcome && welcome.parentNode) welcome.remove();
  }

  function connect() {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const tokenQS = TOKEN ? '?token=' + encodeURIComponent(TOKEN) : '';
    const url = proto + '//' + location.host + '/api/sessions/' + encodeURIComponent(SESSION_ID) + '/ws' + tokenQS;
    ws = new WebSocket(url);

    ws.onopen = () => { setStatus('connected', 'connected'); backoffMs = 1000; };
    ws.onclose = () => {
      setStatus('error', 'disconnected');
      // Reconnect with simple linear backoff capped at 8s.
      setTimeout(connect, backoffMs);
      backoffMs = Math.min(8000, backoffMs + 1000);
    };
    ws.onerror = () => setStatus('error', 'error');
    ws.onmessage = onMessage;
  }

  function onMessage(ev) {
    let msg;
    try { msg = JSON.parse(ev.data); } catch (e) { return; }
    if (msg.type !== 'state.update') return;

    // The server forwards bus.Event.Data as a raw JSON value. The
    // widget MCP handler marshals iframePayload / a2uiPayload structs,
    // so msg.data is an object already (not a string).
    const payload = msg.data;
    if (!payload || typeof payload !== 'object') return;

    if (payload.format === 'iframe') {
      hideWelcomeOnce();
      renderIframe(payload);
    } else if (payload.format === 'a2ui') {
      hideWelcomeOnce();
      renderA2UI(payload);
    } else {
      console.warn('canvas: unknown format', payload.format, payload);
    }
  }

  // --- iframe widgets ------------------------------------------------------

  function renderIframe(p) {
    const widgetID = p.widget_id;
    const html = p.html || '';
    const origin = p.origin || '';

    let frame = widgets.get(widgetID);
    if (!frame) {
      const container = document.createElement('div');
      container.className = 'widget';
      container.dataset.widgetId = widgetID;

      const header = document.createElement('div');
      header.className = 'widget-header';
      const idEl = document.createElement('span');
      idEl.className = 'widget-id';
      idEl.textContent = widgetID;
      header.appendChild(idEl);
      if (origin) {
        const originEl = document.createElement('span');
        originEl.className = 'widget-origin';
        originEl.textContent = 'via ' + origin;
        header.appendChild(originEl);
      }

      frame = document.createElement('iframe');
      frame.className = 'widget-iframe';
      // sandbox without allow-same-origin — the iframe gets a unique
      // opaque origin so it cannot reach back into the canvas page
      // beyond postMessage (which we don't yet listen for — v2).
      frame.setAttribute('sandbox', 'allow-scripts');

      container.appendChild(header);
      container.appendChild(frame);
      root.appendChild(container);
      widgets.set(widgetID, frame);
    }
    frame.srcdoc = html;
  }

  // --- A2UI surfaces -------------------------------------------------------

  function renderA2UI(p) {
    // Body is the wrapped {"a2ui_operations": [...]} that a2ui.Render
    // marshals. It arrives as a JSON-encoded string field inside the
    // payload object because the Go side marshaled the bytes as a
    // string member. Decode and pull the op array out for display.
    let body = p.body;
    if (typeof body === 'string') {
      try { body = JSON.parse(body); } catch (e) { /* leave as string */ }
    }
    const ops = (body && body.a2ui_operations) || [];

    // Group ops by surface ID — each surface gets one stable container.
    // For now there is no real renderer wired; we dump the ops per
    // surface as JSON so developers can see what the agent is emitting.
    // The @a2ui/web_core renderer lands in a follow-up task and will
    // replace the dump call inside each surface container.
    for (const op of ops) {
      const surfaceID = surfaceIDOf(op);
      if (!surfaceID) continue;
      const container = ensureSurface(surfaceID, p.origin);
      appendA2UIDump(container, op);
    }
  }

  function surfaceIDOf(op) {
    if (op.createSurface) return op.createSurface.surfaceId;
    if (op.updateComponents) return op.updateComponents.surfaceId;
    if (op.updateDataModel) return op.updateDataModel.surfaceId;
    return '';
  }

  function ensureSurface(surfaceID, origin) {
    let container = surfaces.get(surfaceID);
    if (container) return container;

    container = document.createElement('div');
    container.className = 'a2ui-surface';
    container.dataset.surfaceId = surfaceID;

    const header = document.createElement('div');
    header.className = 'a2ui-surface-header';
    const idEl = document.createElement('span');
    idEl.className = 'a2ui-surface-id';
    idEl.textContent = 'a2ui: ' + surfaceID;
    header.appendChild(idEl);
    if (origin) {
      const originEl = document.createElement('span');
      originEl.className = 'widget-origin';
      originEl.textContent = 'via ' + origin;
      header.appendChild(originEl);
    }
    container.appendChild(header);

    root.appendChild(container);
    surfaces.set(surfaceID, container);
    return container;
  }

  function appendA2UIDump(container, op) {
    const pre = document.createElement('pre');
    pre.className = 'a2ui-dump';
    pre.textContent = JSON.stringify(op, null, 2);
    container.appendChild(pre);
  }

  connect();
})();
