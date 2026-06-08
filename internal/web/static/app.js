// ycode merged shell — chat thread (left) + canvas pane (right).
//
// Owns one WebSocket to /api/sessions/{id}/ws and routes events:
//   - text.delta, thinking.delta, tool.*, turn.*, usage.update,
//     permission.request, command.* → chat thread (DOM in #messages)
//   - state.update                                  → canvas pane
//     (delegated to window.YcodeCanvas.createRenderer)
//
// Header controls:
//   - #model-select   GET /api/models on init, PUT /api/config/model
//                     on change.
//   - #theme-select   stores ycode.theme in localStorage; "system"
//                     resolves to light/dark via prefers-color-scheme.
//   - #canvas-toggle  reveal/hide the canvas pane after it has been
//                     populated at least once.

(function () {
  'use strict';

  // --- Configuration ---
  // Detect base path: if served at /ycode/, API calls go to /ycode/api/...
  // If served at root, API calls go to /api/...
  const basePath = (function () {
    const path = window.location.pathname;
    const dir = path.replace(/\/[^/]*$/, '');
    if (dir && dir !== '/') return dir;
    return '';
  })();
  const API_BASE = window.location.origin + basePath;
  // Token resolution: ?token=… overrides everything; otherwise fall
  // back to the inline window.YCODE_TOKEN that the server injects into
  // index.html (see internal/web/embed.go:HandlerWithToken).
  const TOKEN = new URLSearchParams(window.location.search).get('token') || window.YCODE_TOKEN || '';

  // --- DOM ---
  const messagesEl = document.getElementById('messages');
  const welcomeEl = document.getElementById('welcome');
  const inputEl = document.getElementById('input');
  const formEl = document.getElementById('input-form');
  const sendBtn = document.getElementById('send-btn');
  const statusBadge = document.getElementById('status-badge');
  const modelSelect = document.getElementById('model-select');
  const workspaceSelect = document.getElementById('workspace-select');
  const themeSelect = document.getElementById('theme-select');
  const tokenCount = document.getElementById('token-count');
  const toolProgressEl = document.getElementById('tool-progress');
  const canvasPane = document.getElementById('canvas-pane');
  const canvasPaneRoot = document.getElementById('canvas-pane-root');
  const canvasToggle = document.getElementById('canvas-toggle');
  const canvasClose = document.getElementById('canvas-close');

  // --- State ---
  let ws = null;
  let sessionID = null;
  let currentAssistantEl = null;
  let currentThinkingEl = null;
  let isWorking = false;
  let totalInputTokens = 0;
  let totalOutputTokens = 0;
  let toolStates = {};
  let toolCallEls = {};   // tool_use_id -> inline log entry element
  let canvasHasContent = false;
  let canvasRenderer = null;

  // Prompts longer than this get ellipsed with a "Show full" link that
  // opens a modal. Keeps the chat thread scannable when users paste
  // large prompts (logs, stack traces, etc.).
  const USER_PROMPT_ELLIPSIS = 280;

  // --- Theme --------------------------------------------------------------
  // Stored preference: "system" (default) | "light" | "dark".
  // JS resolves "system" against prefers-color-scheme and sets
  // <html data-theme="light|dark"> so CSS only has to handle two
  // effective themes.
  const THEME_KEY = 'ycode.theme';
  const mq = window.matchMedia ? window.matchMedia('(prefers-color-scheme: dark)') : null;

  function getStoredTheme() {
    try { return localStorage.getItem(THEME_KEY) || 'system'; } catch (e) { return 'system'; }
  }

  function applyTheme(pref) {
    let effective = pref;
    if (pref === 'system' || !pref) {
      effective = mq && mq.matches ? 'dark' : 'light';
    }
    document.documentElement.setAttribute('data-theme', effective);
  }

  function initTheme() {
    const pref = getStoredTheme();
    themeSelect.value = pref;
    applyTheme(pref);
    themeSelect.addEventListener('change', function () {
      const v = themeSelect.value;
      try { localStorage.setItem(THEME_KEY, v); } catch (e) {}
      applyTheme(v);
    });
    if (mq && mq.addEventListener) {
      mq.addEventListener('change', function () {
        if (getStoredTheme() === 'system') applyTheme('system');
      });
    }
  }

  // --- REST helpers ---
  function apiHeaders() {
    const h = { 'Content-Type': 'application/json' };
    if (TOKEN) h['Authorization'] = 'Bearer ' + TOKEN;
    return h;
  }

  async function apiGet(path) {
    const resp = await fetch(API_BASE + path, { headers: apiHeaders() });
    return resp.json();
  }

  async function apiPost(path, body) {
    const resp = await fetch(API_BASE + path, {
      method: 'POST',
      headers: apiHeaders(),
      body: JSON.stringify(body),
    });
    return resp.json();
  }

  async function apiPut(path, body) {
    const resp = await fetch(API_BASE + path, {
      method: 'PUT',
      headers: apiHeaders(),
      body: JSON.stringify(body),
    });
    return resp.json();
  }

  // --- Init ---
  // The init flow takes the workspace policy into account:
  //
  //   policy=cwd (or unset): server seeded a primary session against
  //     its startup CWD. /api/status returns it; we adopt as today.
  //     The workspace dropdown shows just "cwd" and is read-only.
  //
  //   policy=per-session (default): status.session_id is intentionally
  //     blank under this policy because the seeded session is rooted
  //     at the server's startup dir, not a per-session sandbox. We
  //     fetch /api/workspaces, reattach to the last-used one (via
  //     localStorage) when it still exists, otherwise pick the
  //     newest existing workspace, otherwise allocate a fresh one.
  //
  //   policy=loom: TBD — wired in a follow-up task. The dropdown
  //     surfaces the policy so the user understands the workspace
  //     model their session is operating under.
  const WORKSPACE_KEY = 'ycode.workspaceID';
  let workspacePolicy = '';

  async function init() {
    initTheme();
    initCanvas();

    try {
      const status = await apiGet('/api/status');
      workspacePolicy = status.workspace_policy || '';

      if (workspacePolicy === 'per-session') {
        await initWorkspaceSelect_PerSession();
      } else {
        // cwd / loom / unset — preserve today's auto-adopt-seeded shape.
        sessionID = status.session_id;
        if (!sessionID) {
          const sess = await apiPost('/api/sessions', {});
          sessionID = sess.id;
        }
        await initWorkspaceSelect_Static(status.work_dir || '');
      }

      // Populate model dropdown after we know the active model.
      await initModelSelect(status.model || '');

      connectWebSocket();
    } catch (err) {
      setStatus('error', 'Failed to connect: ' + err.message);
    }
  }

  // initWorkspaceSelect_PerSession lists the user's workspaces, picks
  // one (last-used > newest existing > freshly allocated), and creates
  // a session attached to it.
  async function initWorkspaceSelect_PerSession() {
    let list = [];
    try {
      const resp = await apiGet('/api/workspaces');
      list = (resp && resp.workspaces) || [];
    } catch (e) {
      console.warn('/api/workspaces failed', e);
    }

    const stored = (function () {
      try { return localStorage.getItem(WORKSPACE_KEY) || ''; } catch (e) { return ''; }
    })();

    let pickID = '';
    if (stored && list.some(function (w) { return w.id === stored; })) {
      pickID = stored;
    } else if (list.length > 0) {
      pickID = list[0].id; // newest first per server-side sort
    }

    // Create a session attached to pickID (or freshly allocated when
    // pickID is empty).
    const body = pickID ? { workspace_id: pickID } : {};
    const sess = await apiPost('/api/sessions', body);
    sessionID = sess.id;
    const activeWorkspace = sess.work_dir || '';
    // The server may have allocated a fresh workspace — refresh the
    // list so the dropdown sees it.
    if (!pickID) {
      try {
        const refreshed = await apiGet('/api/workspaces');
        list = (refreshed && refreshed.workspaces) || [];
      } catch (e) { /* keep stale list */ }
    }
    // Persist the chosen workspace for the next page load.
    const chosenID = inferWorkspaceID(activeWorkspace, list, pickID);
    try { localStorage.setItem(WORKSPACE_KEY, chosenID); } catch (e) {}

    renderWorkspaceSelect(list, chosenID);
  }

  // initWorkspaceSelect_Static is the cwd/loom path — the workspace is
  // pinned by the server, no per-session list. We still render the
  // dropdown for visibility but it has just one entry.
  async function initWorkspaceSelect_Static(workDir) {
    workspaceSelect.innerHTML = '';
    const opt = document.createElement('option');
    opt.value = '';
    opt.textContent = workspacePolicy ? (workspacePolicy + ': ' + (workDir || '?')) : (workDir || '(no workspace)');
    workspaceSelect.appendChild(opt);
    workspaceSelect.disabled = true;
    workspaceSelect.title = 'Workspace policy: ' + (workspacePolicy || 'cwd') + ' — pinned by server';
  }

  // inferWorkspaceID derives the workspace id from the server's session
  // info. The session response carries work_dir; we match it against
  // the listed workspaces to find the id. Fallback to pickID (when we
  // reattached) or the list's first id.
  function inferWorkspaceID(workDir, list, pickID) {
    if (pickID) return pickID;
    if (workDir) {
      for (const w of list) {
        if (w.path === workDir) return w.id;
      }
    }
    return list.length > 0 ? list[0].id : '';
  }

  function renderWorkspaceSelect(list, activeID) {
    workspaceSelect.innerHTML = '';
    workspaceSelect.disabled = false;
    for (const w of list) {
      const opt = document.createElement('option');
      opt.value = w.id;
      opt.textContent = shortWorkspaceLabel(w);
      opt.title = w.path;
      if (w.id === activeID) opt.selected = true;
      workspaceSelect.appendChild(opt);
    }
    // Sentinel rows for "new" and "manage" — handled in the change
    // listener via the special-prefix values "+new" / "+manage".
    const sep = document.createElement('option');
    sep.disabled = true;
    sep.textContent = '──────────';
    workspaceSelect.appendChild(sep);
    const newOpt = document.createElement('option');
    newOpt.value = '+new';
    newOpt.textContent = '+ New workspace';
    workspaceSelect.appendChild(newOpt);
    const manageOpt = document.createElement('option');
    manageOpt.value = '+manage';
    manageOpt.textContent = 'Manage workspaces…';
    workspaceSelect.appendChild(manageOpt);

    workspaceSelect.dataset.previous = activeID;
    workspaceSelect.onchange = onWorkspaceSelectChange;
  }

  function shortWorkspaceLabel(w) {
    // The id is timestamp-prefix-randhex; show just the date + time
    // portion + last 4 chars of the random so the dropdown stays
    // narrow but the entries remain distinguishable.
    const id = w.id || '';
    const m = id.match(/^(\d{4})(\d{2})(\d{2})-(\d{2})(\d{2})(\d{2})-([0-9a-f]+)$/);
    if (m) {
      return m[2] + '/' + m[3] + ' ' + m[4] + ':' + m[5] + '  ' + m[7].slice(-4);
    }
    return id || '(workspace)';
  }

  async function onWorkspaceSelectChange() {
    const next = workspaceSelect.value;
    if (next === '+new') {
      await switchToNewWorkspace();
      return;
    }
    if (next === '+manage') {
      // Revert the visible selection back to the previous, since
      // "manage" isn't an actual workspace.
      const prev = workspaceSelect.dataset.previous || '';
      if (prev) workspaceSelect.value = prev;
      showManageModal();
      return;
    }
    // Real workspace pick: switch by reloading the page with the new
    // selection stored. Simpler than tearing down the live WebSocket
    // and reinitializing state in-place; the page is cheap to rebuild.
    try { localStorage.setItem(WORKSPACE_KEY, next); } catch (e) {}
    window.location.reload();
  }

  async function switchToNewWorkspace() {
    try {
      const ws = await apiPost('/api/workspaces', {});
      if (ws && ws.id) {
        try { localStorage.setItem(WORKSPACE_KEY, ws.id); } catch (e) {}
        window.location.reload();
      }
    } catch (e) {
      console.error('create workspace failed', e);
      const prev = workspaceSelect.dataset.previous || '';
      if (prev) workspaceSelect.value = prev;
    }
  }

  // --- Manage workspaces modal -------------------------------------------
  let manageModal = null;

  async function showManageModal() {
    if (!manageModal) {
      manageModal = document.createElement('div');
      manageModal.className = 'prompt-modal';
      manageModal.innerHTML =
        '<div class="prompt-modal-backdrop"></div>' +
        '<div class="prompt-modal-card">' +
        '<div class="prompt-modal-header">' +
        '<span>Workspaces</span>' +
        '<button class="prompt-modal-close" type="button" aria-label="Close">×</button>' +
        '</div>' +
        '<div class="prompt-modal-body manage-list"></div>' +
        '</div>';
      const close = function () { manageModal.classList.add('hidden'); };
      manageModal.querySelector('.prompt-modal-backdrop').addEventListener('click', close);
      manageModal.querySelector('.prompt-modal-close').addEventListener('click', close);
      document.body.appendChild(manageModal);
    }
    const listEl = manageModal.querySelector('.manage-list');
    listEl.textContent = 'Loading…';
    manageModal.classList.remove('hidden');
    let list = [];
    try {
      const resp = await apiGet('/api/workspaces');
      list = (resp && resp.workspaces) || [];
    } catch (e) {
      listEl.textContent = 'Failed: ' + e.message;
      return;
    }
    listEl.innerHTML = '';
    if (list.length === 0) {
      listEl.textContent = '(no workspaces yet)';
      return;
    }
    const activeID = (function () {
      try { return localStorage.getItem(WORKSPACE_KEY) || ''; } catch (e) { return ''; }
    })();
    for (const w of list) {
      const row = document.createElement('div');
      row.className = 'manage-row';
      const isActive = w.id === activeID;
      row.innerHTML =
        '<div class="manage-info">' +
        '<div class="manage-id">' + escapeHtml(shortWorkspaceLabel(w)) +
        (isActive ? ' <span class="manage-active">active</span>' : '') +
        '</div>' +
        '<div class="manage-path">' + escapeHtml(w.path) + '</div>' +
        '</div>' +
        '<button class="manage-delete" type="button" data-id="' + escapeHtml(w.id) + '">Delete</button>';
      row.querySelector('.manage-delete').addEventListener('click', async function () {
        if (!confirm('Delete workspace and all its files?\n' + w.path)) return;
        try {
          await apiDelete('/api/workspaces/' + encodeURIComponent(w.id));
          // If we deleted the active workspace, clear localStorage so
          // next reload picks fresh.
          if (w.id === activeID) {
            try { localStorage.removeItem(WORKSPACE_KEY); } catch (e) {}
          }
          // Refresh the modal in place.
          showManageModal();
        } catch (e) {
          alert('delete failed: ' + e.message);
        }
      });
      listEl.appendChild(row);
    }
  }

  async function apiDelete(path) {
    const resp = await fetch(API_BASE + path, {
      method: 'DELETE',
      headers: apiHeaders(),
    });
    if (!resp.ok) throw new Error('HTTP ' + resp.status);
    return resp.json();
  }

  // --- Model dropdown -----------------------------------------------------
  async function initModelSelect(activeModel) {
    try {
      const models = await apiGet('/api/models');
      const list = Array.isArray(models) ? models : [];

      // Replace placeholder option with the full list. Use ID as value,
      // alias-augmented display label so the user can recognize them.
      modelSelect.innerHTML = '';
      if (list.length === 0) {
        const opt = document.createElement('option');
        opt.value = activeModel || '';
        opt.textContent = activeModel || '(no models)';
        modelSelect.appendChild(opt);
      } else {
        for (const m of list) {
          const opt = document.createElement('option');
          opt.value = m.id;
          opt.textContent = m.alias ? (m.alias + ' — ' + m.id) : m.id;
          if (m.current || m.id === activeModel) opt.selected = true;
          modelSelect.appendChild(opt);
        }
      }

      modelSelect.addEventListener('change', async function () {
        const next = modelSelect.value;
        const prev = modelSelect.dataset.previous || '';
        modelSelect.disabled = true;
        try {
          await apiPut('/api/config/model', { model: next });
          modelSelect.dataset.previous = next;
        } catch (e) {
          // Revert on failure.
          if (prev) modelSelect.value = prev;
          console.error('failed to switch model', e);
        } finally {
          modelSelect.disabled = false;
        }
      });
      modelSelect.dataset.previous = modelSelect.value;
    } catch (e) {
      // Non-fatal: leave the placeholder option in place.
      console.warn('model list unavailable', e);
    }
  }

  // --- Canvas pane --------------------------------------------------------
  function initCanvas() {
    if (!window.YcodeCanvas || typeof window.YcodeCanvas.createRenderer !== 'function') {
      // Canvas module didn't load (older build, embed missing). The
      // shell still works; state.update events are dropped with a warn.
      console.warn('YcodeCanvas not loaded; state.update events will be dropped');
      return;
    }
    canvasRenderer = window.YcodeCanvas.createRenderer(canvasPaneRoot, {
      send: function (msg) {
        if (!ws || ws.readyState !== WebSocket.OPEN) return;
        ws.send(JSON.stringify(msg));
      },
    });
    canvasRenderer.setOnFirstRender(function () {
      canvasHasContent = true;
      expandCanvas();
      canvasToggle.classList.remove('hidden');
    });

    canvasToggle.addEventListener('click', function () {
      if (canvasPane.classList.contains('collapsed')) expandCanvas();
      else collapseCanvas();
    });
    canvasClose.addEventListener('click', collapseCanvas);
  }

  function expandCanvas() {
    canvasPane.classList.remove('collapsed');
    canvasToggle.classList.add('active');
  }

  function collapseCanvas() {
    canvasPane.classList.add('collapsed');
    canvasToggle.classList.remove('active');
    if (!canvasHasContent) canvasToggle.classList.add('hidden');
  }

  // --- WebSocket ---
  function connectWebSocket() {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = proto + '//' + location.host + basePath + '/api/sessions/' + sessionID + '/ws?token=' + TOKEN;

    ws = new WebSocket(wsUrl);

    ws.onopen = function () { setStatus('connected', 'connected'); };
    ws.onclose = function () {
      setStatus('error', 'disconnected');
      setTimeout(connectWebSocket, 2000);
    };
    ws.onerror = function () { setStatus('error', 'connection error'); };

    ws.onmessage = function (evt) {
      try {
        const event = JSON.parse(evt.data);
        handleEvent(event);
      } catch (e) {
        console.error('Failed to parse event:', e);
      }
    };
  }

  // --- Event handling ---
  function handleEvent(event) {
    const data = event.data ? (typeof event.data === 'string' ? JSON.parse(event.data) : event.data) : {};

    switch (event.type) {
      case 'turn.start': onTurnStart(data); break;
      case 'text.delta': onTextDelta(data); break;
      case 'thinking.delta': onThinkingDelta(data); break;
      case 'tool_use.start': onToolStart(data); break;
      case 'tool.progress': onToolProgress(data); break;
      case 'tool.result': onToolResult(data); break;
      case 'turn.complete': onTurnComplete(data); break;
      case 'turn.error': onTurnError(data); break;
      case 'usage.update': onUsageUpdate(data); break;
      case 'permission.request': onPermissionRequest(data); break;
      case 'command.progress': onCommandProgress(data); break;
      case 'command.delta': onCommandDelta(data); break;
      case 'command.complete': onCommandComplete(data); break;
      case 'command.error': onCommandError(data); break;
      case 'state.update': onStateUpdate(data); break;
      case 'llm.request': onLLMRequest(data); break;
      case 'llm.response': onLLMResponse(data); break;
    }
  }

  function onStateUpdate(data) {
    if (canvasRenderer) canvasRenderer.handleStateUpdate(data);
  }

  function onTurnStart(data) {
    isWorking = true;
    sendBtn.disabled = true;
    currentAssistantEl = null;
    currentThinkingEl = null;
    toolStates = {};
    toolCallEls = {};
    toolProgressEl.innerHTML = '';
    toolProgressEl.classList.add('hidden');
  }

  function onTextDelta(data) {
    if (!currentAssistantEl) {
      currentAssistantEl = addMessage('assistant', '');
      if (welcomeEl) welcomeEl.remove();
    }
    const content = currentAssistantEl.querySelector('.message-content');
    const cursor = content.querySelector('.streaming-cursor');
    if (cursor) cursor.remove();
    content.appendChild(document.createTextNode(data.text || ''));
    const newCursor = document.createElement('span');
    newCursor.className = 'streaming-cursor';
    content.appendChild(newCursor);
    scrollToBottom();
  }

  function onThinkingDelta(data) {
    if (!currentThinkingEl) {
      currentThinkingEl = document.createElement('div');
      currentThinkingEl.className = 'thinking';
      messagesEl.appendChild(currentThinkingEl);
    }
    currentThinkingEl.textContent += (data.text || '');
    scrollToBottom();
  }

  function onToolStart(data) {
    toolProgressEl.classList.remove('hidden');
    const id = data.id || data.tool;
    toolStates[id] = { tool: data.tool, status: 'running' };
    renderToolProgress();
    // Also append an inline log entry in the message thread so users see
    // tool name + args alongside text deltas. Result text is filled in
    // later by onToolResult.
    addToolCallEntry(id, data.tool, data.input);
  }

  function onToolProgress(data) {
    const id = data.id || data.tool;
    if (toolStates[id]) toolStates[id].status = data.status || 'running';
    renderToolProgress();
    updateToolCallStatus(id, data.status || 'running');
  }

  function onToolResult(data) {
    const id = data.tool_use_id || data.id;
    if (toolStates[id]) toolStates[id].status = data.is_error ? 'failed' : 'completed';
    renderToolProgress();
    updateToolCallStatus(id, data.is_error ? 'failed' : 'completed');
    setToolCallResult(id, data.content || '', !!data.is_error);
  }

  function onTurnComplete(data) {
    isWorking = false;
    sendBtn.disabled = false;
    if (currentAssistantEl) {
      const cursor = currentAssistantEl.querySelector('.streaming-cursor');
      if (cursor) cursor.remove();
      const content = currentAssistantEl.querySelector('.message-content');
      if (content) renderMarkdown(content);
    }
    currentAssistantEl = null;
    currentThinkingEl = null;
    setTimeout(function () {
      toolProgressEl.classList.add('hidden');
      toolProgressEl.innerHTML = '';
      toolStates = {};
    }, 1000);
    inputEl.focus();
  }

  function onTurnError(data) {
    isWorking = false;
    sendBtn.disabled = false;
    const errorEl = document.createElement('div');
    errorEl.className = 'error-msg';
    errorEl.textContent = 'Error: ' + (data.error || 'Unknown error');
    messagesEl.appendChild(errorEl);
    scrollToBottom();
    currentAssistantEl = null;
    inputEl.focus();
  }

  function onUsageUpdate(data) {
    totalInputTokens += (data.input_tokens || 0);
    totalOutputTokens += (data.output_tokens || 0);
    tokenCount.textContent = totalInputTokens + ' / ' + totalOutputTokens + ' tokens';
  }

  // --- Slash-command stream handlers (e.g. /init) ---
  let currentCommandEl = null;

  function ensureCommandEl() {
    if (!currentCommandEl) {
      currentCommandEl = addMessage('assistant', '');
      if (welcomeEl) welcomeEl.remove();
      isWorking = true;
      sendBtn.disabled = true;
    }
    return currentCommandEl;
  }

  function onCommandProgress(data) {
    const msg = data.message || '';
    if (!msg) return;
    const el = ensureCommandEl();
    const content = el.querySelector('.message-content');
    const cursor = content.querySelector('.streaming-cursor');
    if (cursor) cursor.remove();
    content.appendChild(document.createTextNode(msg + '\n'));
    const newCursor = document.createElement('span');
    newCursor.className = 'streaming-cursor';
    content.appendChild(newCursor);
    scrollToBottom();
  }

  function onCommandDelta(data) {
    const text = data.text || '';
    if (!text) return;
    const el = ensureCommandEl();
    const content = el.querySelector('.message-content');
    const cursor = content.querySelector('.streaming-cursor');
    if (cursor) cursor.remove();
    content.appendChild(document.createTextNode(text));
    const newCursor = document.createElement('span');
    newCursor.className = 'streaming-cursor';
    content.appendChild(newCursor);
    scrollToBottom();
  }

  function onCommandComplete(data) {
    isWorking = false;
    sendBtn.disabled = false;
    if (currentCommandEl) {
      const cursor = currentCommandEl.querySelector('.streaming-cursor');
      if (cursor) cursor.remove();
      const content = currentCommandEl.querySelector('.message-content');
      if (content) {
        const result = data.result || '';
        if (result) content.appendChild(document.createTextNode('\n' + result));
        renderMarkdown(content);
      }
    }
    currentCommandEl = null;
    inputEl.focus();
  }

  function onCommandError(data) {
    isWorking = false;
    sendBtn.disabled = false;
    if (currentCommandEl) {
      const cursor = currentCommandEl.querySelector('.streaming-cursor');
      if (cursor) cursor.remove();
    }
    const errorEl = document.createElement('div');
    errorEl.className = 'error-msg';
    errorEl.textContent = 'Command error: ' + (data.error || 'Unknown error');
    messagesEl.appendChild(errorEl);
    scrollToBottom();
    currentCommandEl = null;
    inputEl.focus();
  }

  function onPermissionRequest(data) {
    var reqId = data.request_id || '';
    var tool = data.tool || 'unknown';
    var detail = data.detail || '';

    var permEl = document.createElement('div');
    permEl.className = 'permission-prompt';
    permEl.innerHTML =
      '<div class="perm-text">Tool <strong>' + escapeHtml(tool) + '</strong> requires permission' +
      (detail ? ': <code>' + escapeHtml(detail) + '</code>' : '') +
      '</div>' +
      '<div class="perm-buttons">' +
      '<button class="perm-allow" data-id="' + reqId + '">Allow</button>' +
      '<button class="perm-deny" data-id="' + reqId + '">Deny</button>' +
      '</div>';

    permEl.querySelector('.perm-allow').addEventListener('click', function () {
      respondPermission(reqId, true);
      permEl.remove();
    });
    permEl.querySelector('.perm-deny').addEventListener('click', function () {
      respondPermission(reqId, false);
      permEl.remove();
    });

    messagesEl.appendChild(permEl);
    scrollToBottom();
  }

  function respondPermission(requestId, allowed) {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    ws.send(JSON.stringify({
      type: 'permission.respond',
      data: { request_id: requestId, allowed: allowed },
    }));
  }

  function escapeHtml(str) {
    var div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
  }

  // --- Inline log entries: tool calls + raw LLM payloads ----------------
  // Each entry is a sibling of .message in #messages. Visual indicators:
  //   .log-entry.tool-call    — gear icon, orange left border (tool I/O)
  //   .log-entry.llm-request  — outbound arrow, blue left border (raw req)
  //   .log-entry.llm-response — inbound arrow, green left border (raw resp)
  // Each is collapsible; tool result + raw JSON are hidden behind a
  // <details> disclosure so the thread stays scannable.

  function decodeToolInput(input) {
    if (input == null) return '';
    if (typeof input === 'string') return input;
    if (Array.isArray(input)) {
      // tool_use.start emits the raw input bytes as a byte array — turn
      // it back into the original JSON string.
      try { return new TextDecoder().decode(new Uint8Array(input)); }
      catch (e) { return JSON.stringify(input); }
    }
    try { return JSON.stringify(input, null, 2); } catch (e) { return String(input); }
  }

  function prettyJSON(obj) {
    try { return JSON.stringify(obj, null, 2); } catch (e) { return String(obj); }
  }

  function addToolCallEntry(id, toolName, rawInput) {
    if (welcomeEl) welcomeEl.remove();
    const argsText = decodeToolInput(rawInput);

    const el = document.createElement('div');
    el.className = 'log-entry tool-call running';
    el.dataset.toolId = id;

    const header = document.createElement('div');
    header.className = 'log-header';
    header.innerHTML =
      '<span class="log-icon">⚙</span>' +
      '<span class="log-label">Tool</span>' +
      '<span class="log-name"></span>' +
      '<span class="log-status" data-status="running">running</span>';
    header.querySelector('.log-name').textContent = toolName || '(unknown)';
    el.appendChild(header);

    if (argsText) {
      const argsDetails = document.createElement('details');
      argsDetails.className = 'log-section';
      argsDetails.open = true;
      const argsSummary = document.createElement('summary');
      argsSummary.textContent = 'args';
      argsDetails.appendChild(argsSummary);
      const argsPre = document.createElement('pre');
      argsPre.className = 'log-body';
      argsPre.textContent = argsText;
      argsDetails.appendChild(argsPre);
      el.appendChild(argsDetails);
    }

    const resultDetails = document.createElement('details');
    resultDetails.className = 'log-section log-result';
    resultDetails.style.display = 'none'; // revealed when result arrives
    const resultSummary = document.createElement('summary');
    resultSummary.textContent = 'result';
    resultDetails.appendChild(resultSummary);
    const resultPre = document.createElement('pre');
    resultPre.className = 'log-body';
    resultDetails.appendChild(resultPre);
    el.appendChild(resultDetails);

    messagesEl.appendChild(el);
    toolCallEls[id] = el;
    scrollToBottom();
  }

  function updateToolCallStatus(id, status) {
    const el = toolCallEls[id];
    if (!el) return;
    el.classList.remove('running', 'completed', 'failed', 'queued');
    el.classList.add(status);
    const badge = el.querySelector('.log-status');
    if (badge) {
      badge.textContent = status;
      badge.dataset.status = status;
    }
  }

  function setToolCallResult(id, content, isError) {
    const el = toolCallEls[id];
    if (!el) return;
    const details = el.querySelector('.log-result');
    if (!details) return;
    const pre = details.querySelector('.log-body');
    if (pre) pre.textContent = content || '(no output)';
    details.style.display = '';
    // Collapse by default unless the tool failed (errors are usually the
    // thing the user wants to read first).
    details.open = !!isError;
    if (isError) el.classList.add('failed');
  }

  // Each turn produces one llm.request and one llm.response. Render them
  // as collapsible entries; both default to closed since most users only
  // open them when they need to debug a wire-level question.
  function addLLMEntry(kind, data) {
    if (welcomeEl) welcomeEl.remove();
    const el = document.createElement('div');
    el.className = 'log-entry llm-' + kind;

    const icon = kind === 'request' ? '↗' : '↙';
    const label = kind === 'request' ? 'LLM Request' : 'LLM Response';
    const subtitle = data && data.model ? data.model : '';

    const header = document.createElement('div');
    header.className = 'log-header';
    header.innerHTML =
      '<span class="log-icon">' + icon + '</span>' +
      '<span class="log-label">' + label + '</span>' +
      '<span class="log-name"></span>';
    header.querySelector('.log-name').textContent = subtitle;
    el.appendChild(header);

    const details = document.createElement('details');
    details.className = 'log-section';
    const summary = document.createElement('summary');
    summary.textContent = 'payload';
    details.appendChild(summary);
    const pre = document.createElement('pre');
    pre.className = 'log-body';
    pre.textContent = prettyJSON(data);
    details.appendChild(pre);
    el.appendChild(details);

    messagesEl.appendChild(el);
    scrollToBottom();
  }

  function onLLMRequest(data) { addLLMEntry('request', data); }
  function onLLMResponse(data) { addLLMEntry('response', data); }

  // --- Prompt modal: full text of a long user prompt ----------------------
  let promptModal = null;

  function showPromptModal(text) {
    if (!promptModal) {
      promptModal = document.createElement('div');
      promptModal.className = 'prompt-modal';
      promptModal.innerHTML =
        '<div class="prompt-modal-backdrop"></div>' +
        '<div class="prompt-modal-card">' +
        '<div class="prompt-modal-header">' +
        '<span>Full prompt</span>' +
        '<button class="prompt-modal-close" type="button" aria-label="Close">×</button>' +
        '</div>' +
        '<pre class="prompt-modal-body"></pre>' +
        '</div>';
      const close = function () { promptModal.classList.add('hidden'); };
      promptModal.querySelector('.prompt-modal-backdrop').addEventListener('click', close);
      promptModal.querySelector('.prompt-modal-close').addEventListener('click', close);
      document.body.appendChild(promptModal);
    }
    promptModal.querySelector('.prompt-modal-body').textContent = text;
    promptModal.classList.remove('hidden');
  }

  // Fill the input with a previous prompt for editing + resending.
  function resendPrompt(text) {
    inputEl.value = text;
    inputEl.style.height = 'auto';
    inputEl.style.height = Math.min(inputEl.scrollHeight, 200) + 'px';
    inputEl.focus();
    // Place cursor at end so editing flows naturally.
    inputEl.setSelectionRange(text.length, text.length);
  }

  // --- Tool progress rendering ---
  function renderToolProgress() {
    toolProgressEl.innerHTML = '';
    for (const [id, state] of Object.entries(toolStates)) {
      const el = document.createElement('div');
      el.className = 'tool-item ' + state.status;
      const icons = { running: '⧗', completed: '✓', failed: '✗', queued: '◻' };
      el.innerHTML = '<span class="icon">' + (icons[state.status] || '◻') + '</span> ' + state.tool;
      toolProgressEl.appendChild(el);
    }
  }

  // --- Message rendering ---
  function addMessage(role, text) {
    if (welcomeEl) welcomeEl.remove();

    const msgEl = document.createElement('div');
    msgEl.className = 'message ' + role;

    const roleEl = document.createElement('div');
    roleEl.className = 'message-role ' + role;
    const icon = role === 'user' ? '→' : '←';
    roleEl.innerHTML =
      '<span class="role-icon">' + icon + '</span>' +
      '<span class="role-label">' + (role === 'user' ? 'You' : 'Assistant') + '</span>';

    if (role === 'user' && text) {
      const resendBtn = document.createElement('button');
      resendBtn.type = 'button';
      resendBtn.className = 'msg-action resend-btn';
      resendBtn.textContent = 'Resend / edit';
      resendBtn.title = 'Copy this prompt into the input box for editing + resending';
      resendBtn.addEventListener('click', function () { resendPrompt(text); });
      roleEl.appendChild(resendBtn);
    }

    msgEl.appendChild(roleEl);

    const contentEl = document.createElement('div');
    contentEl.className = 'message-content';
    if (text) {
      if (role === 'user' && text.length > USER_PROMPT_ELLIPSIS) {
        // Truncate long pasted prompts; full text behind a popup link.
        const trimmed = text.slice(0, USER_PROMPT_ELLIPSIS).trimEnd() + '…';
        contentEl.textContent = trimmed;
        const showFull = document.createElement('button');
        showFull.type = 'button';
        showFull.className = 'show-full-link';
        showFull.textContent = 'Show full (' + text.length + ' chars)';
        showFull.addEventListener('click', function () { showPromptModal(text); });
        contentEl.appendChild(document.createElement('br'));
        contentEl.appendChild(showFull);
      } else {
        contentEl.textContent = text;
      }
    }
    msgEl.appendChild(contentEl);

    messagesEl.appendChild(msgEl);
    scrollToBottom();
    return msgEl;
  }

  // Simple markdown: render code blocks and inline code in final output.
  function renderMarkdown(el) {
    let text = el.textContent;
    text = text.replace(/```(\w*)\n([\s\S]*?)```/g, function (_, lang, code) {
      return '<pre class="code-block"><code>' + escapeHtml(code.trimEnd()) + '</code></pre>';
    });
    text = text.replace(/`([^`]+)`/g, '<code class="inline-code">$1</code>');
    text = text.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
    el.innerHTML = text;
  }

  function scrollToBottom() {
    messagesEl.scrollTop = messagesEl.scrollHeight;
  }

  function setStatus(cls, text) {
    statusBadge.className = 'badge ' + cls;
    statusBadge.textContent = text;
  }

  // --- Input handling ---
  formEl.addEventListener('submit', function (e) {
    e.preventDefault();
    sendMessage();
  });

  inputEl.addEventListener('keydown', function (e) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      sendMessage();
    }
  });

  inputEl.addEventListener('input', function () {
    this.style.height = 'auto';
    this.style.height = Math.min(this.scrollHeight, 200) + 'px';
  });

  function sendMessage() {
    const text = inputEl.value.trim();
    if (!text || isWorking || !ws || ws.readyState !== WebSocket.OPEN) return;

    addMessage('user', text);
    inputEl.value = '';
    inputEl.style.height = 'auto';

    ws.send(JSON.stringify({
      type: 'message.send',
      data: { text: text },
    }));
  }

  // --- Start ---
  init();
})();
