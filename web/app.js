// ycode Web Client
// Connects to ycode API server via WebSocket for conversation
// and REST for session management.

(function () {
  'use strict';

  // --- Configuration ---
  // Detect base path: if served at /ycode/, API calls go to /ycode/api/...
  // If served at root, API calls go to /api/...
  const basePath = (function () {
    const path = window.location.pathname;
    // Strip trailing filename or slash to get the directory.
    const dir = path.replace(/\/[^/]*$/, '');
    // If we're under a proxy prefix like /ycode, use that as base.
    if (dir && dir !== '/') return dir;
    return '';
  })();
  const API_BASE = window.location.origin + basePath;
  const TOKEN = new URLSearchParams(window.location.search).get('token') || '';

  // --- DOM ---
  const messagesEl = document.getElementById('messages');
  const welcomeEl = document.getElementById('welcome');
  const inputEl = document.getElementById('input');
  const formEl = document.getElementById('input-form');
  const sendBtn = document.getElementById('send-btn');
  const statusBadge = document.getElementById('status-badge');
  const modelBtn = document.getElementById('model-btn');
  const modelDropdown = document.getElementById('model-dropdown');
  const modelFilter = document.getElementById('model-filter');
  const modelListEl = document.getElementById('model-list');
  const tokenCount = document.getElementById('token-count');
  const toolProgressEl = document.getElementById('tool-progress');

  // --- State ---
  let ws = null;
  let sessionID = null;
  let currentAssistantEl = null;
  let currentThinkingEl = null;
  let isWorking = false;
  let totalInputTokens = 0;
  let totalOutputTokens = 0;
  let toolStates = {};

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

  // --- Init ---
  async function init() {
    try {
      // Get server status.
      const status = await apiGet('/api/status');
      currentModel = status.model || '';
      modelBtn.textContent = currentModel || 'select model';
      sessionID = status.session_id;

      if (!sessionID) {
        // Create a session.
        const sess = await apiPost('/api/sessions', {});
        sessionID = sess.id;
      }

      connectWebSocket();
    } catch (err) {
      setStatus('error', 'Failed to connect: ' + err.message);
    }
  }

  // --- WebSocket ---
  function connectWebSocket() {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = proto + '//' + location.host + basePath + '/api/sessions/' + sessionID + '/ws?token=' + TOKEN;

    ws = new WebSocket(wsUrl);

    ws.onopen = function () {
      setStatus('connected', 'connected');
    };

    ws.onclose = function () {
      setStatus('error', 'disconnected');
      // Reconnect after 2s.
      setTimeout(connectWebSocket, 2000);
    };

    ws.onerror = function () {
      setStatus('error', 'connection error');
    };

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
      case 'turn.start':
        onTurnStart(data);
        break;
      case 'text.delta':
        onTextDelta(data);
        break;
      case 'thinking.delta':
        onThinkingDelta(data);
        break;
      case 'tool_use.start':
        onToolStart(data);
        break;
      case 'tool.progress':
        onToolProgress(data);
        break;
      case 'tool.result':
        onToolResult(data);
        break;
      case 'turn.complete':
        onTurnComplete(data);
        break;
      case 'turn.error':
        onTurnError(data);
        break;
      case 'usage.update':
        onUsageUpdate(data);
        break;
      case 'permission.request':
        onPermissionRequest(data);
        break;
    }
  }

  function onTurnStart(data) {
    isWorking = true;
    sendBtn.disabled = true;
    currentAssistantEl = null;
    currentThinkingEl = null;
    toolStates = {};
    toolProgressEl.innerHTML = '';
    toolProgressEl.classList.add('hidden');
  }

  function onTextDelta(data) {
    if (!currentAssistantEl) {
      currentAssistantEl = addMessage('assistant', '');
      // Remove welcome message.
      if (welcomeEl) welcomeEl.remove();
    }
    const content = currentAssistantEl.querySelector('.message-content');
    // Remove cursor if present.
    const cursor = content.querySelector('.streaming-cursor');
    if (cursor) cursor.remove();
    // Append text.
    content.appendChild(document.createTextNode(data.text || ''));
    // Add cursor back.
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
  }

  function onToolProgress(data) {
    const id = data.id || data.tool;
    if (toolStates[id]) {
      toolStates[id].status = data.status || 'running';
    }
    renderToolProgress();
  }

  function onToolResult(data) {
    const id = data.tool_use_id || data.id;
    if (toolStates[id]) {
      toolStates[id].status = data.is_error ? 'failed' : 'completed';
    }
    renderToolProgress();
  }

  function onTurnComplete(data) {
    isWorking = false;
    sendBtn.disabled = false;
    // Remove streaming cursor and render markdown.
    if (currentAssistantEl) {
      const cursor = currentAssistantEl.querySelector('.streaming-cursor');
      if (cursor) cursor.remove();
      const content = currentAssistantEl.querySelector('.message-content');
      if (content) renderMarkdown(content);
    }
    currentAssistantEl = null;
    currentThinkingEl = null;
    // Clear tool progress after a short delay.
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
    roleEl.textContent = role === 'user' ? 'You' : 'Assistant';
    msgEl.appendChild(roleEl);

    const contentEl = document.createElement('div');
    contentEl.className = 'message-content';
    if (text) contentEl.textContent = text;
    msgEl.appendChild(contentEl);

    messagesEl.appendChild(msgEl);
    scrollToBottom();
    return msgEl;
  }

  // Simple markdown: render code blocks and inline code in final output.
  function renderMarkdown(el) {
    let text = el.textContent;
    // Replace fenced code blocks.
    text = text.replace(/```(\w*)\n([\s\S]*?)```/g, function (_, lang, code) {
      return '<pre class="code-block"><code>' + escapeHtml(code.trimEnd()) + '</code></pre>';
    });
    // Replace inline code.
    text = text.replace(/`([^`]+)`/g, '<code class="inline-code">$1</code>');
    // Replace bold.
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

  // Auto-resize textarea.
  inputEl.addEventListener('input', function () {
    this.style.height = 'auto';
    this.style.height = Math.min(this.scrollHeight, 200) + 'px';
  });

  function sendMessage() {
    const text = inputEl.value.trim();
    if (!text || isWorking || !ws || ws.readyState !== WebSocket.OPEN) return;

    // Show user message.
    addMessage('user', text);
    inputEl.value = '';
    inputEl.style.height = 'auto';

    // Send via WebSocket.
    ws.send(JSON.stringify({
      type: 'message.send',
      data: { text: text },
    }));
  }

  // --- Model picker ---
  let allModels = [];
  let currentModel = '';

  async function fetchModels() {
    try {
      allModels = await apiGet('/api/models');
      if (!Array.isArray(allModels)) allModels = [];
    } catch (e) {
      console.error('Failed to fetch models:', e);
      allModels = [];
    }
  }

  function renderModelList(filter) {
    modelListEl.innerHTML = '';
    var lower = (filter || '').toLowerCase();
    var filtered = allModels.filter(function (m) {
      if (!lower) return true;
      return (m.id || '').toLowerCase().indexOf(lower) !== -1 ||
        (m.alias || '').toLowerCase().indexOf(lower) !== -1 ||
        (m.provider || '').toLowerCase().indexOf(lower) !== -1 ||
        (m.source || '').toLowerCase().indexOf(lower) !== -1;
    });

    if (filtered.length === 0) {
      var empty = document.createElement('li');
      empty.textContent = 'No models found';
      empty.style.color = 'var(--text-muted)';
      empty.style.cursor = 'default';
      modelListEl.appendChild(empty);
      return;
    }

    for (var i = 0; i < filtered.length; i++) {
      var m = filtered[i];
      var li = document.createElement('li');
      if (m.id === currentModel || m.alias === currentModel) {
        li.classList.add('active');
      }

      var html = '';
      if (m.alias) {
        html += '<span class="model-alias">' + escapeHtml(m.alias) + '</span> ';
      }
      html += '<span class="model-id">' + escapeHtml(m.id) + '</span>';

      var meta = m.provider || '';
      if (m.size) meta += ' ' + m.size;
      if (m.source && m.source !== 'builtin') {
        html += ' <span class="source-label">' + escapeHtml(m.source) + '</span>';
      }
      if (meta) {
        html += ' <span class="model-meta">' + escapeHtml(meta) + '</span>';
      }

      li.innerHTML = html;
      li.dataset.model = m.alias || m.id;
      li.addEventListener('click', onModelSelect);
      modelListEl.appendChild(li);
    }
  }

  function onModelSelect(e) {
    var model = e.currentTarget.dataset.model;
    if (!model) return;
    switchModel(model);
    closeModelDropdown();
  }

  async function switchModel(model) {
    try {
      await fetch(API_BASE + '/api/config/model', {
        method: 'PUT',
        headers: apiHeaders(),
        body: JSON.stringify({ model: model }),
      });
      currentModel = model;
      modelBtn.textContent = model;
    } catch (e) {
      console.error('Failed to switch model:', e);
    }
  }

  function openModelDropdown() {
    fetchModels().then(function () {
      renderModelList('');
      modelDropdown.classList.remove('hidden');
      modelFilter.value = '';
      modelFilter.focus();
    });
  }

  function closeModelDropdown() {
    modelDropdown.classList.add('hidden');
  }

  modelBtn.addEventListener('click', function (e) {
    e.stopPropagation();
    if (modelDropdown.classList.contains('hidden')) {
      openModelDropdown();
    } else {
      closeModelDropdown();
    }
  });

  modelFilter.addEventListener('input', function () {
    renderModelList(modelFilter.value);
  });

  modelFilter.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') {
      closeModelDropdown();
    }
  });

  // Close dropdown on outside click.
  document.addEventListener('click', function (e) {
    if (!modelDropdown.contains(e.target) && e.target !== modelBtn) {
      closeModelDropdown();
    }
  });

  // --- Start ---
  init();
})();
