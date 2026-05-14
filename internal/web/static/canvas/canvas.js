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

  // Token resolution: ?token=… overrides everything; otherwise fall
  // back to the inline window.YCODE_TOKEN that the server injects into
  // index.html (see internal/web/embed.go:HandlerWithToken). Empty
  // when the server is in no-auth mode.
  const TOKEN = new URLSearchParams(location.search).get('token') || window.YCODE_TOKEN || '';
  const URL_SESSION = new URLSearchParams(location.search).get('session') || '';
  const FALLBACK_SESSION = 'canvas-default';

  // API requests must target whatever prefix this page is mounted under —
  // direct (`/canvas/` → `/`) or behind the observability proxy
  // (`/ycode/canvas/` → `/ycode/`). The substring before `/canvas/` in
  // location.pathname is that prefix; fall back to `/` if not found.
  const API_BASE = (function () {
    const i = location.pathname.lastIndexOf('/canvas/');
    return i >= 0 ? location.pathname.slice(0, i + 1) : '/';
  })();

  const root = document.getElementById('canvas-root');
  const welcome = document.getElementById('welcome');
  const statusBadge = document.getElementById('status-badge');
  const sessionLabel = document.getElementById('session-label');
  const responseStrip = document.getElementById('response-strip');
  const promptForm = document.getElementById('prompt-form');
  const promptEl = document.getElementById('prompt');

  // Track rendered widgets so re-emits replace in place. A2UI surfaces
  // are owned by the renderer below — it maintains its own state map
  // and DOM, we just feed it ops as they arrive.
  const widgets = new Map();   // widget_id  → iframe element
  const a2uiRenderer = window.A2UI && window.A2UI.attach(root, {
    log: (...args) => console.warn(...args),
    emit: (mut) => sendStateMutate(mut),
  });

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
      const resp = await fetch(API_BASE + 'api/status', { headers });
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
    const url = proto + '//' + location.host + API_BASE + 'api/sessions/' + encodeURIComponent(sessionID) + '/ws' + tokenQS;
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
    if (!a2uiRenderer) {
      // Renderer didn't load — log and stop. Failed-load is rare (the
      // file is embedded), but we don't want to silently drop A2UI ops.
      console.error('canvas: A2UI renderer not available; A2UI ops dropped', ops);
      return;
    }
    a2uiRenderer.applyOps(ops, p.origin);
  }

  // sendStateMutate is the renderer's outbound callback — fires when a
  // Button is clicked or any other surface gesture wants to round-trip
  // back to the agent. Wraps the mutation in the bus's state.mutate
  // shape; the agent observes the event on the same session.
  function sendStateMutate(mut) {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    ws.send(JSON.stringify({
      type: 'state.mutate',
      data: {
        format: 'a2ui',
        surface: mut.surface,
        action: mut.action,
        context: mut.context,
      },
    }));
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
