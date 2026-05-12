// ycode canvas — vanilla-JS host shell.
//
// Subscribes to a session's bus over WebSocket. Three flows:
//
//   1. state.update (server → client) — agent-published render payload.
//      format="iframe": create or replace a sandboxed iframe with srcdoc=html.
//      format="a2ui":   group ops by surface; dump JSON pending the
//                       @a2ui/web_core renderer integration.
//
//   2. message.send (client → server) — human types in the prompt
//      bar, we forward to the agent runtime. Same wire format as the
//      classic /ycode/ chat (text + optional files/extra fields).
//
//   3. text.delta (server → client) — streaming prose response.
//      Shown in a thin response strip beneath the canvas. Transient;
//      widgets stay the primary surface.
//
// Default session is "canvas-default" so a foreign agent that drives
// the canvas without knowing ycode's session model still round-trips.
// When the host has an active /ycode/ session (per /api/status), we
// adopt that session ID instead — so a human at /canvas/ drives the
// same agent the /ycode/ chat is driving. ?session= overrides both.

(function () {
  'use strict';

  const TOKEN = new URLSearchParams(location.search).get('token') || '';
  const URL_SESSION = new URLSearchParams(location.search).get('session') || '';
  const FALLBACK_SESSION = 'canvas-default';

  const root = document.getElementById('canvas-root');
  const welcome = document.getElementById('welcome');
  const statusBadge = document.getElementById('status-badge');
  const sessionLabel = document.getElementById('session-label');
  const responseStrip = document.getElementById('response-strip');
  const promptForm = document.getElementById('prompt-form');
  const promptEl = document.getElementById('prompt');

  // Track rendered widgets/surfaces so re-emits replace in place.
  const widgets = new Map();   // widget_id  → iframe element
  const surfaces = new Map();  // surface_id → surface container

  let ws = null;
  let sessionID = '';
  let backoffMs = 1000;
  let activeAssistantText = '';
  let isWorking = false;

  function setStatus(state, text) {
    statusBadge.className = 'badge ' + state;
    statusBadge.textContent = text;
  }

  function hideWelcomeOnce() {
    if (welcome && welcome.parentNode) welcome.remove();
  }

  // --- Session detection ---------------------------------------------------
  //
  // Priority: ?session=<id> → /api/status active session → canvas-default.
  // Adopting the /ycode/ active session means a human at /canvas/ drives
  // the same agent the chat does — output flows to both surfaces.
  async function resolveSession() {
    if (URL_SESSION) return URL_SESSION;
    try {
      const headers = TOKEN ? { 'Authorization': 'Bearer ' + TOKEN } : {};
      const resp = await fetch('/api/status', { headers });
      if (resp.ok) {
        const status = await resp.json();
        if (status && status.session_id) return status.session_id;
      }
    } catch (e) {
      // Fall through to fallback — /api/status failures are non-fatal,
      // the canvas can still subscribe to canvas-default for foreign-
      // agent-driven scenarios.
    }
    return FALLBACK_SESSION;
  }

  // --- WebSocket -----------------------------------------------------------

  function connect() {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const tokenQS = TOKEN ? '?token=' + encodeURIComponent(TOKEN) : '';
    const url = proto + '//' + location.host + '/api/sessions/' + encodeURIComponent(sessionID) + '/ws' + tokenQS;
    ws = new WebSocket(url);

    ws.onopen = () => { setStatus('connected', 'connected'); backoffMs = 1000; };
    ws.onclose = () => {
      setStatus('error', 'disconnected');
      isWorking = false;
      setTimeout(connect, backoffMs);
      backoffMs = Math.min(8000, backoffMs + 1000);
    };
    ws.onerror = () => setStatus('error', 'error');
    ws.onmessage = onMessage;
  }

  function onMessage(ev) {
    let msg;
    try { msg = JSON.parse(ev.data); } catch (e) { return; }

    switch (msg.type) {
      case 'state.update':
        dispatchStateUpdate(msg.data);
        return;
      case 'turn.start':
        isWorking = true;
        activeAssistantText = '';
        showResponseStrip('Working…');
        return;
      case 'text.delta':
        if (msg.data && typeof msg.data.text === 'string') {
          activeAssistantText += msg.data.text;
          showResponseStrip(activeAssistantText);
        }
        return;
      case 'turn.complete':
        isWorking = false;
        // Keep the final response strip visible briefly — let the user
        // read it — then collapse it. Widget output remains untouched.
        scheduleResponseHide();
        return;
      case 'turn.error':
        isWorking = false;
        showResponseStrip('Error: ' + (msg.data && msg.data.error ? msg.data.error : 'turn failed'));
        return;
      default:
        return;
    }
  }

  // --- state.update dispatch ----------------------------------------------

  function dispatchStateUpdate(payload) {
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
      frame.setAttribute('sandbox', 'allow-scripts');

      container.appendChild(header);
      container.appendChild(frame);
      root.appendChild(container);
      widgets.set(widgetID, frame);
    }
    frame.srcdoc = html;
  }

  function renderA2UI(p) {
    let body = p.body;
    if (typeof body === 'string') {
      try { body = JSON.parse(body); } catch (e) { /* leave as string */ }
    }
    const ops = (body && body.a2ui_operations) || [];
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

  // --- Response strip (text.delta surface) --------------------------------

  let responseHideTimer = null;

  function showResponseStrip(text) {
    if (responseHideTimer) {
      clearTimeout(responseHideTimer);
      responseHideTimer = null;
    }
    responseStrip.textContent = text;
    responseStrip.classList.remove('hidden');
  }

  function scheduleResponseHide() {
    if (responseHideTimer) clearTimeout(responseHideTimer);
    // Long enough to read a short response; widgets are the primary
    // record, so this strip can collapse afterward without losing info.
    responseHideTimer = setTimeout(() => {
      responseStrip.classList.add('hidden');
      activeAssistantText = '';
    }, 12000);
  }

  // --- Prompt input -------------------------------------------------------

  promptForm.addEventListener('submit', (e) => {
    e.preventDefault();
    sendPrompt();
  });
  promptEl.addEventListener('keydown', (e) => {
    // Submit on Enter (without Shift) or ⌘/Ctrl+Enter — newline on plain
    // Shift+Enter so multi-line prompts are still possible.
    const cmdOrCtrl = e.metaKey || e.ctrlKey;
    if (e.key === 'Enter' && (cmdOrCtrl || !e.shiftKey)) {
      e.preventDefault();
      sendPrompt();
    }
  });
  promptEl.addEventListener('input', () => {
    promptEl.style.height = 'auto';
    promptEl.style.height = Math.min(promptEl.scrollHeight, 160) + 'px';
  });

  function sendPrompt() {
    const text = promptEl.value.trim();
    if (!text) return;
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      showResponseStrip('Not connected — try again in a moment.');
      return;
    }
    if (isWorking) {
      // Mirror /ycode/ semantics: don't queue, just nudge.
      showResponseStrip('Agent is working — wait for it to finish.');
      return;
    }
    ws.send(JSON.stringify({ type: 'message.send', data: { text } }));
    promptEl.value = '';
    promptEl.style.height = 'auto';
    showResponseStrip('Sent.');
  }

  // --- Init ---------------------------------------------------------------

  resolveSession().then((id) => {
    sessionID = id;
    sessionLabel.textContent = sessionID;
    connect();
  });
})();
