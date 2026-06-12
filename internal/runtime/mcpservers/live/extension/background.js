// ycode-live extension — service worker
//
// Maintains a WebSocket to the ycode Go-side hub (default
// 127.0.0.1:58082/ws). Each incoming request is dispatched to a
// chrome.* API or a content-script injection; the result is shipped
// back over the same socket. A chrome.alarms ping every 25 seconds
// keeps the MV3 service worker from being killed mid-session.

const DEFAULT_PORT = 58082;
const RECONNECT_BASE_MS = 1000;
const RECONNECT_MAX_MS = 15000;
const KEEPALIVE_PERIOD_MIN = 25 / 60; // 25 seconds, expressed in minutes

let ws = null;
let reconnectMs = RECONNECT_BASE_MS;
let connectedPort = 0;
let activeTabId = null;
// User intent. Only the popup's connect/disconnect commands flip this.
// openWS/scheduleReconnect bail when it's false so a disconnect (or a
// socket that closes after the user asked to stop) fully tears down the
// reconnect loop instead of spinning on ERR_CONNECTION_REFUSED forever.
let wantConnected = false;
let reconnectTimer = null;

// --- entry: open/close commands from popup ---

chrome.runtime.onMessage.addListener((msg, _sender, sendResponse) => {
  if (msg && msg.type === "ycode-live:connect") {
    connectedPort = msg.port || DEFAULT_PORT;
    if (msg.tabId) {
      activeTabId = msg.tabId;
    }
    wantConnected = true;
    reconnectMs = RECONNECT_BASE_MS;
    openWS();
    sendResponse({ status: "connecting", port: connectedPort });
  } else if (msg && msg.type === "ycode-live:disconnect") {
    // Flip intent off *before* closing so the socket's onclose handler
    // doesn't schedule a fresh reconnect.
    wantConnected = false;
    if (reconnectTimer !== null) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
    if (ws) {
      try { ws.close(); } catch (e) { /* ignore */ }
      ws = null;
    }
    chrome.alarms.clear("ycode-live-keepalive");
    sendResponse({ status: "disconnected" });
  } else if (msg && msg.type === "ycode-live:status") {
    sendResponse({
      connected: ws !== null && ws.readyState === WebSocket.OPEN,
      port: connectedPort,
      tabId: activeTabId,
    });
  }
  return true;
});

// --- websocket lifecycle ---

function openWS() {
  if (!wantConnected) {
    return;
  }
  if (ws && ws.readyState !== WebSocket.CLOSED) {
    return;
  }
  const port = connectedPort || DEFAULT_PORT;
  const url = `ws://127.0.0.1:${port}/ws`;
  try {
    ws = new WebSocket(url);
  } catch (err) {
    console.warn("ycode-live: WebSocket constructor failed", err);
    scheduleReconnect();
    return;
  }
  ws.onopen = () => {
    reconnectMs = RECONNECT_BASE_MS;
    chrome.alarms.create("ycode-live-keepalive", { periodInMinutes: KEEPALIVE_PERIOD_MIN });
    // _hello envelope — id == 0, method == "_hello", result carries
    // {version, methods, permissions}. The hub uses this to detect
    // version drift without a separate round-trip.
    try {
      const manifest = chrome.runtime.getManifest();
      ws.send(JSON.stringify({
        id: 0,
        method: "_hello",
        result: {
          version: manifest.version,
          methods: SUPPORTED_METHODS,
          permissions: manifest.permissions || [],
        },
      }));
    } catch (e) {
      console.warn("ycode-live: _hello send failed", e);
    }
  };
  ws.onmessage = onMessage;
  ws.onerror = (e) => console.warn("ycode-live: socket error", e);
  ws.onclose = () => {
    ws = null;
    chrome.alarms.clear("ycode-live-keepalive");
    scheduleReconnect();
  };
}

function scheduleReconnect() {
  // Respect user intent — a disconnect (or a close that races the
  // disconnect command) must not revive the loop.
  if (!wantConnected) {
    return;
  }
  if (reconnectTimer !== null) {
    return; // a reconnect is already pending
  }
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null;
    openWS();
  }, reconnectMs);
  reconnectMs = Math.min(reconnectMs * 2, RECONNECT_MAX_MS);
}

chrome.alarms.onAlarm.addListener((alarm) => {
  if (alarm.name === "ycode-live-keepalive" && ws && ws.readyState === WebSocket.OPEN) {
    // No-op send keeps the MV3 worker alive without server traffic.
    try { ws.send(JSON.stringify({ id: 0, method: "_ping" })); } catch (e) { /* ignore */ }
  }
});

// --- request dispatch ---

const SUPPORTED_METHODS = [
  "navigate", "back", "screenshot", "extract",
  "click", "type", "scroll", "tabs", "evaluate",
  "wait_for_selector", "keyboard_press",
  "clipboard_read", "clipboard_write",
  "cookies_get", "storage_get", "capabilities",
  // DevTools-flavored — gated on a long-lived chrome.debugger attach
  // managed by debuggerAttach below.
  "network_list", "console_get", "perf_start", "perf_stop", "lighthouse",
];

// --- chrome.debugger attach manager + devtools event buffers --------
//
// chrome.debugger.attach is exclusive per-target, so multiple consumers
// (trusted keystrokes; long-lived Network/Runtime/Tracing listeners)
// share one attach via reference counting. Each acquire(tabId, domain)
// must pair with release; the attach is dropped when refCount hits 0.
// onDetach (page close, user-dismissed banner, etc.) wipes the entry
// so a subsequent acquire reattaches cleanly.
//
// Ring buffers (network responses, console messages) live module-level
// and start capturing on first acquire — matches probe semantics
// ("capture begins when the agent asks").

const NET_RING_MAX = 200;
const CONSOLE_RING_MAX = 200;

let netRing = [];
let consoleRing = [];
let traceState = { active: false, startedAt: 0, eventCount: 0 };

function pushBounded(ring, entry, max) {
  ring.push(entry);
  if (ring.length > max) ring.splice(0, ring.length - max);
}

const debuggerAttach = {
  // tabId -> { refCount, domains: Set<string> }
  byTab: new Map(),

  async acquire(tabId, domain) {
    let state = this.byTab.get(tabId);
    if (!state) {
      state = { refCount: 0, domains: new Set() };
      this.byTab.set(tabId, state);
      await chrome.debugger.attach({ tabId }, "1.3");
    }
    state.refCount++;
    if (domain && !state.domains.has(domain)) {
      await chrome.debugger.sendCommand({ tabId }, `${domain}.enable`);
      state.domains.add(domain);
    }
  },

  async release(tabId) {
    const state = this.byTab.get(tabId);
    if (!state) return;
    state.refCount--;
    if (state.refCount <= 0) {
      this.byTab.delete(tabId);
      try { await chrome.debugger.detach({ tabId }); } catch (_) { /* already gone */ }
    }
  },
};

chrome.debugger.onDetach.addListener((source, reason) => {
  if (source && source.tabId) {
    debuggerAttach.byTab.delete(source.tabId);
  }
  // Trace state belongs to whichever tab was being recorded; just
  // mark it inactive on any detach so a fresh perf_start works.
  if (traceState.active) traceState.active = false;
});

chrome.debugger.onEvent.addListener((_source, method, params) => {
  try {
    if (method === "Network.responseReceived" && params && params.response) {
      pushBounded(netRing, {
        url: params.response.url,
        status: params.response.status,
        mime_type: params.response.mimeType,
        resource_type: params.type,
        when: new Date().toISOString(),
      }, NET_RING_MAX);
    } else if (method === "Runtime.consoleAPICalled" && params) {
      const text = (params.args || []).map((a) => {
        if (a.value !== undefined) return String(a.value);
        if (a.description) return a.description;
        return "";
      }).join(" ");
      pushBounded(consoleRing, {
        level: params.type,
        text: text,
        when: new Date().toISOString(),
      }, CONSOLE_RING_MAX);
    } else if (method === "Runtime.exceptionThrown" && params && params.exceptionDetails) {
      const det = params.exceptionDetails;
      const text = (det.exception && det.exception.description) || det.text || "";
      pushBounded(consoleRing, {
        level: "exception",
        text: text,
        when: new Date().toISOString(),
      }, CONSOLE_RING_MAX);
    } else if (method === "Tracing.dataCollected" && traceState.active && params && params.value) {
      traceState.eventCount += params.value.length;
    }
  } catch (e) {
    console.warn("ycode-live: debugger event handler failed", e);
  }
});

// --- tool-call debug log ---------------------------------------------
//
// Bounded ring buffer of recent tool calls, surfaced in the popup UI
// below the connection status and optionally mirrored to console.debug
// for power users. In-memory only; service-worker eviction wipes it.

const TOOL_LOG_MAX = 50;
const PREVIEW_MAX = 240;
let toolLog = [];
let debugMirror = false;
const logPorts = new Set();

chrome.storage.local.get(["ycodeDebugMirror"], ({ ycodeDebugMirror }) => {
  debugMirror = ycodeDebugMirror === true;
});

function previewValue(v) {
  if (v === null || v === undefined) return "";
  let s;
  try {
    s = typeof v === "string" ? v : JSON.stringify(v);
  } catch (_) {
    s = String(v);
  }
  if (s.length > PREVIEW_MAX) return s.slice(0, PREVIEW_MAX) + "…";
  return s;
}

function broadcastLog(msg) {
  for (const p of logPorts) {
    try { p.postMessage(msg); } catch (_) { /* port closed */ }
  }
}

function recordToolCall(call) {
  const entry = {
    id: call.id,
    method: call.method,
    paramsPreview: previewValue(call.params),
    resultPreview: call.error ? "" : previewValue(call.result),
    error: call.error || "",
    ok: !call.error,
    durationMs: call.durationMs,
    startedAt: call.startedAt,
  };
  toolLog.push(entry);
  if (toolLog.length > TOOL_LOG_MAX) {
    toolLog.splice(0, toolLog.length - TOOL_LOG_MAX);
  }
  if (debugMirror) {
    const tag = entry.ok ? "ok" : "err";
    console.debug(
      `ycode-live[${tag}] ${entry.method} ${entry.durationMs}ms`,
      { params: call.params, result: call.result, error: entry.error },
    );
  }
  broadcastLog({ kind: "entry", entry });
}

chrome.runtime.onConnect.addListener((port) => {
  if (port.name !== "ycode-live:log-stream") return;
  logPorts.add(port);
  try {
    port.postMessage({ kind: "snapshot", entries: toolLog, mirror: debugMirror });
  } catch (_) { /* popup already closed */ }
  port.onMessage.addListener((msg) => {
    if (!msg) return;
    if (msg.type === "clear") {
      toolLog = [];
      broadcastLog({ kind: "snapshot", entries: toolLog, mirror: debugMirror });
    } else if (msg.type === "mirror") {
      debugMirror = !!msg.value;
      chrome.storage.local.set({ ycodeDebugMirror: debugMirror });
      broadcastLog({ kind: "mirror", mirror: debugMirror });
    }
  });
  port.onDisconnect.addListener(() => logPorts.delete(port));
});

async function onMessage(ev) {
  let req;
  try {
    req = JSON.parse(ev.data);
  } catch (e) {
    return; // not our protocol
  }
  if (!req || typeof req.id !== "number") return;
  if (req.method === "_pong" || req.method === "_ping") return;

  const startedAt = Date.now();
  let result;
  let error = "";
  try {
    result = await dispatch(req.method, req.params || {});
    ws.send(JSON.stringify({ id: req.id, result }));
  } catch (err) {
    error = String(err.message || err);
    ws.send(JSON.stringify({ id: req.id, error }));
  }
  // Record EVERY dispatched method — including capabilities. Do not
  // add per-method skips here. A pathological client that hammers
  // capabilities (or any other low-cost probe) is exactly what this
  // log is meant to surface; suppressing "noisy" methods hides the
  // misbehavior that the panel exists to catch.
  recordToolCall({
    id: req.id,
    method: req.method,
    params: req.params || {},
    result,
    error,
    durationMs: Date.now() - startedAt,
    startedAt,
  });
}

async function targetTabId() {
  if (activeTabId !== null) {
    try {
      await chrome.tabs.get(activeTabId);
      return activeTabId;
    } catch (e) {
      activeTabId = null;
    }
  }
  const tabs = await chrome.tabs.query({ active: true, currentWindow: true });
  if (tabs.length === 0) throw new Error("no active tab");
  activeTabId = tabs[0].id;
  return activeTabId;
}

async function dispatch(method, params) {
  // Capabilities does not need a tab (it's a pure metadata read).
  if (method === "capabilities") return capabilities();
  if (method === "cookies_get") return cookiesGet(params);

  const tabId = await targetTabId();
  switch (method) {
    case "navigate":
      return navigate(tabId, params.url);
    case "back":
      return chrome.tabs.goBack(tabId).then(() => extractInTab(tabId, {}));
    case "screenshot":
      return takeScreenshot(tabId);
    case "extract":
      return extractInTab(tabId, params);
    case "click":
      return runInTab(tabId, "click", params);
    case "type":
      return runInTab(tabId, "type", params);
    case "scroll":
      return runInTab(tabId, "scroll", params);
    case "tabs":
      return handleTabs(params);
    case "evaluate":
      return evaluateInTab(tabId, params.script);
    case "wait_for_selector":
      return waitForSelector(tabId, params);
    case "keyboard_press":
      return keyboardPress(tabId, params);
    case "clipboard_read":
      return clipboardRead(tabId);
    case "clipboard_write":
      return clipboardWrite(tabId, params);
    case "storage_get":
      return storageGet(tabId, params);
    case "network_list":
      return networkList(tabId);
    case "console_get":
      return consoleGet(tabId);
    case "perf_start":
      return perfStart(tabId);
    case "perf_stop":
      return perfStop(tabId);
    case "lighthouse":
      return lighthouse(tabId);
  }
  throw new Error(`unknown method: ${method}`);
}

function capabilities() {
  const m = chrome.runtime.getManifest();
  return {
    data: JSON.stringify({
      mode: "live",
      version: m.version,
      methods: SUPPORTED_METHODS,
      permissions: m.permissions || [],
    }),
  };
}

async function cookiesGet(params) {
  // Use the chrome.cookies API rather than document.cookie so HttpOnly
  // cookies are visible. Filters: name + domain (both optional).
  const tabId = await targetTabId();
  const tab = await chrome.tabs.get(tabId);
  let domain = params.domain || "";
  try {
    if (!domain && tab.url) domain = new URL(tab.url).hostname;
  } catch (_) { /* about:blank etc */ }
  const all = await chrome.cookies.getAll({});
  const want = (params.name || "").trim();
  const out = [];
  for (const c of all) {
    if (want && c.name !== want) continue;
    if (domain) {
      // chrome cookies store .example.com — match by suffix.
      const cd = (c.domain || "").replace(/^\./, "");
      if (cd !== domain && !domain.endsWith("." + cd) && !cd.endsWith("." + domain)) continue;
    }
    out.push({
      name: c.name, value: c.value, domain: c.domain, path: c.path,
      secure: c.secure, httpOnly: c.httpOnly,
      session: c.session, sameSite: c.sameSite,
      expirationDate: c.expirationDate,
    });
  }
  return { data: JSON.stringify(out) };
}

async function navigate(tabId, url) {
  if (!url) throw new Error("navigate: url required");
  await chrome.tabs.update(tabId, { url });
  await waitForLoad(tabId);
  return extractInTab(tabId, {});
}

function waitForLoad(tabId) {
  return new Promise((resolve) => {
    const listener = (updatedId, info) => {
      if (updatedId === tabId && info.status === "complete") {
        chrome.tabs.onUpdated.removeListener(listener);
        resolve();
      }
    };
    chrome.tabs.onUpdated.addListener(listener);
    setTimeout(() => {
      chrome.tabs.onUpdated.removeListener(listener);
      resolve();
    }, 20000);
  });
}

async function takeScreenshot(tabId) {
  const tab = await chrome.tabs.get(tabId);
  const dataUrl = await chrome.tabs.captureVisibleTab(tab.windowId, { format: "png" });
  // strip the data:image/png;base64, prefix
  const idx = dataUrl.indexOf(",");
  // Caller (Go side) is responsible for MaxBytes / SavePath
  // post-processing; the extension always emits a raw base64 PNG so
  // the JPEG re-encode path lives in one place.
  return { image: idx >= 0 ? dataUrl.slice(idx + 1) : dataUrl };
}

async function extractInTab(tabId, params) {
  const [{ result } = {}] = await chrome.scripting.executeScript({
    target: { tabId },
    args: [params || {}],
    func: (params) => {
      const SCOPE_SEL = (params.scope || "").trim();
      const MATCH = (params.match_text || params.goal || "").trim();
      const LIMIT = params.limit && params.limit > 0 ? params.limit : 50;
      const OFFSET = params.offset && params.offset > 0 ? params.offset : 0;
      const root = SCOPE_SEL ? document.querySelector(SCOPE_SEL) : document;
      if (!root) {
        return { title: document.title, url: location.href, content: "", elements: "", error: "extract: scope " + SCOPE_SEL + " not found" };
      }
      const navFilter = !SCOPE_SEL;
      const all = root.querySelectorAll(
        "a, button, input, select, textarea, [role='button'], [role='link']"
      );
      const matches = [];
      const want = MATCH.toLowerCase();
      for (let i = 0; i < all.length; i++) {
        const el = all[i];
        if (navFilter && el.closest && el.closest("nav, aside, [role='navigation'], [role='complementary']")) continue;
        const visible = ((el.innerText) || el.value || el.getAttribute("aria-label") || "").trim();
        if (want) {
          const ph = el.getAttribute("placeholder") || "";
          const ar = el.getAttribute("aria-label") || "";
          if (visible.toLowerCase().indexOf(want) < 0 &&
              ph.toLowerCase().indexOf(want) < 0 &&
              ar.toLowerCase().indexOf(want) < 0) continue;
        }
        matches.push(el);
      }
      const total = matches.length;
      const slice = matches.slice(OFFSET, OFFSET + LIMIT);
      const lines = [];
      for (let j = 0; j < slice.length; j++) {
        const el = slice[j];
        const tag = el.tagName.toLowerCase();
        const text = ((el.innerText) || el.value || el.getAttribute("aria-label") || "").trim().slice(0, 80);
        const attrs = [];
        for (const a of ["type", "placeholder", "href", "name", "value", "role", "aria-label"]) {
          const v = el.getAttribute(a);
          if (v) attrs.push(`${a}="${String(v).slice(0, 60)}"`);
        }
        lines.push(`[${OFFSET + j + 1}] <${tag} ${attrs.join(" ")}>${text}</${tag}>`);
      }
      const body = (root === document ? (document.body && document.body.innerText) : root.innerText) || "";
      return {
        title: document.title,
        url: location.href,
        content: body.length > 16000 ? body.slice(0, 16000) + "\n... (truncated)" : body,
        elements: lines.join("\n"),
        total: total,
        truncated: total > (OFFSET + LIMIT),
      };
    },
  });
  return result || { title: "", url: "", content: "", elements: "" };
}

async function runInTab(tabId, kind, params) {
  const [{ result } = {}] = await chrome.scripting.executeScript({
    target: { tabId },
    args: [kind, params],
    func: (kind, params) => {
      const matchText = params.match_text || "";
      function elemsInScope() {
        const scope = params.scope ? document.querySelector(params.scope) : document;
        return (scope || document).querySelectorAll(
          "a, button, input, select, textarea, [role='button'], [role='link']"
        );
      }
      function resolveTarget() {
        if (params.selector) {
          const sc = params.scope ? document.querySelector(params.scope) : document;
          return (sc || document).querySelector(params.selector);
        }
        if (params.element_id && params.element_id > 0) {
          const list = elemsInScope();
          // For typed lookup, mirror the extract enumeration: skip
          // nav landmarks unless caller passed an explicit scope.
          const navFilter = !params.scope;
          const flat = [];
          for (const el of list) {
            if (navFilter && el.closest && el.closest("nav, aside, [role='navigation'], [role='complementary']")) continue;
            flat.push(el);
          }
          return flat[params.element_id - 1] || null;
        }
        if (matchText) {
          const want = matchText.toLowerCase();
          for (const el of elemsInScope()) {
            const v = ((el.innerText) || el.value || el.getAttribute("aria-label") || "").trim().toLowerCase();
            if (v.indexOf(want) >= 0) return el;
          }
        }
        return null;
      }
      switch (kind) {
        case "click": {
          const el = resolveTarget();
          if (!el) return { error: "click: element not found" };
          el.click();
          return { content: "clicked" };
        }
        case "type": {
          const el = resolveTarget();
          if (!el) return { error: "type: element not found" };
          el.focus();
          if ("value" in el) {
            el.value = params.text || "";
          } else {
            el.textContent = params.text || "";
          }
          el.dispatchEvent(new Event("input", { bubbles: true }));
          el.dispatchEvent(new Event("change", { bubbles: true }));
          return { content: "typed" };
        }
        case "scroll": {
          let amount = params.amount || 500;
          if (params.direction === "up") amount = -amount;
          window.scrollBy(0, amount);
          return { data: String(window.scrollY) };
        }
      }
      return { error: `runInTab: unknown kind ${kind}` };
    },
  });
  if (result && result.error) {
    throw new Error(result.error);
  }
  return result || {};
}

async function evaluateInTab(tabId, script) {
  if (!script) throw new Error("evaluate: script required");
  // Run in the MAIN world so the script sees the page's globals
  // (matches chromedp.Evaluate semantics on probe/solo).
  const [{ result } = {}] = await chrome.scripting.executeScript({
    target: { tabId },
    args: [script],
    world: "MAIN",
    func: (src) => {
      const stringify = (v) => {
        if (v === undefined) return "";
        if (typeof v === "string") return v;
        try { return JSON.stringify(v); } catch (_) { return String(v); }
      };
      let value;
      try {
        value = (new Function("return (" + src + ")"))();
      } catch (e1) {
        try {
          value = (new Function(src))();
        } catch (e2) {
          return { error: String(e1 && e1.message || e1) };
        }
      }
      if (value && typeof value.then === "function") {
        return value.then(
          (r) => ({ value: stringify(r) }),
          (e) => ({ error: String(e && e.message || e) })
        );
      }
      return { value: stringify(value) };
    },
  });
  if (result && result.error) throw new Error(result.error);
  return { data: result ? result.value : "" };
}

async function waitForSelector(tabId, params) {
  const sel = params.selector;
  if (!sel) throw new Error("wait_for_selector: selector required");
  const timeoutMs = params.timeout_ms && params.timeout_ms > 0 ? params.timeout_ms : 5000;
  const state = params.state || "visible";
  // Poll inside the page in 100 ms ticks. Cheaper than IPC because
  // chrome.scripting.executeScript per tick would burn quota.
  const [{ result } = {}] = await chrome.scripting.executeScript({
    target: { tabId },
    args: [sel, timeoutMs, state],
    func: async (sel, timeoutMs, state) => {
      const deadline = Date.now() + timeoutMs;
      function visible(el) {
        if (!el) return false;
        const cs = getComputedStyle(el);
        if (cs.visibility === "hidden" || cs.display === "none") return false;
        const r = el.getBoundingClientRect();
        return r.width > 0 && r.height > 0;
      }
      while (Date.now() < deadline) {
        const el = document.querySelector(sel);
        if (state === "detached") {
          if (!el) return { ok: true };
        } else if (state === "attached") {
          if (el) return { ok: true };
        } else {
          if (visible(el)) return { ok: true };
        }
        await new Promise((r) => setTimeout(r, 100));
      }
      return { ok: false };
    },
  });
  if (!result || !result.ok) {
    throw new Error(`wait_for_selector: timeout after ${timeoutMs}ms (state=${state})`);
  }
  return { data: `state=${state}` };
}

async function keyboardPress(tabId, params) {
  if (!params.key) throw new Error("keyboard_press: key required");
  // Prefer chrome.debugger + Input.dispatchKeyEvent for trusted
  // keystrokes (manifest 0.4.0). Falls back to the synthetic
  // KeyboardEvent path if the debugger attach fails — for example
  // if DevTools is open on the same tab.
  if (params.selector) {
    try {
      await chrome.scripting.executeScript({
        target: { tabId },
        args: [params.selector],
        func: (sel) => {
          const el = document.querySelector(sel);
          if (el && el.focus) el.focus();
        },
      });
    } catch (_) { /* ignore focus failures; keystrokes still go to body */ }
  }
  try {
    await dispatchTrustedKey(tabId, params.key, params.modifiers || []);
    return { data: "pressed=" + params.key + " (trusted)" };
  } catch (e) {
    // Fall back to synthetic events.
    console.warn("ycode-live: trusted key dispatch failed, falling back", e);
  }
  const [{ result } = {}] = await chrome.scripting.executeScript({
    target: { tabId },
    args: [params.key, params.modifiers || []],
    func: (key, modifiers) => {
      const mods = new Set(modifiers.map((m) => String(m).toLowerCase()));
      const opts = {
        key: key,
        code: key.length === 1 ? "Key" + key.toUpperCase() : key,
        bubbles: true,
        cancelable: true,
        shiftKey: mods.has("shift"),
        ctrlKey: mods.has("control") || mods.has("ctrl"),
        altKey: mods.has("alt"),
        metaKey: mods.has("meta") || mods.has("cmd") || mods.has("command"),
      };
      const target = document.activeElement || document.body;
      target.dispatchEvent(new KeyboardEvent("keydown", opts));
      target.dispatchEvent(new KeyboardEvent("keypress", opts));
      if (key === "Enter" && target.form) {
        try { target.form.requestSubmit ? target.form.requestSubmit() : target.form.submit(); } catch (_) { /* ignore */ }
      }
      target.dispatchEvent(new KeyboardEvent("keyup", opts));
      return { ok: true };
    },
  });
  if (result && result.error) throw new Error(result.error);
  return { data: "pressed=" + params.key + " (synthetic)" };
}

// dispatchTrustedKey attaches chrome.debugger to the target tab,
// dispatches a real Input.dispatchKeyEvent pair (keyDown + keyUp),
// then detaches. Modifiers map to the CDP bitfield. Throws on any
// failure — the caller falls back to a synthetic KeyboardEvent.
const KEY_TO_CODE = {
  Enter: { code: "Enter", windowsVirtualKeyCode: 13 },
  Tab: { code: "Tab", windowsVirtualKeyCode: 9 },
  Escape: { code: "Escape", windowsVirtualKeyCode: 27 },
  Backspace: { code: "Backspace", windowsVirtualKeyCode: 8 },
  Delete: { code: "Delete", windowsVirtualKeyCode: 46 },
  ArrowUp: { code: "ArrowUp", windowsVirtualKeyCode: 38 },
  ArrowDown: { code: "ArrowDown", windowsVirtualKeyCode: 40 },
  ArrowLeft: { code: "ArrowLeft", windowsVirtualKeyCode: 37 },
  ArrowRight: { code: "ArrowRight", windowsVirtualKeyCode: 39 },
  Home: { code: "Home", windowsVirtualKeyCode: 36 },
  End: { code: "End", windowsVirtualKeyCode: 35 },
  PageUp: { code: "PageUp", windowsVirtualKeyCode: 33 },
  PageDown: { code: "PageDown", windowsVirtualKeyCode: 34 },
};

async function dispatchTrustedKey(tabId, key, modifiers) {
  const target = { tabId: tabId };
  const mods = new Set(modifiers.map((m) => String(m).toLowerCase()));
  // CDP modifier bitfield: 1=Alt, 2=Ctrl, 4=Meta, 8=Shift.
  let modBits = 0;
  if (mods.has("alt")) modBits |= 1;
  if (mods.has("control") || mods.has("ctrl")) modBits |= 2;
  if (mods.has("meta") || mods.has("cmd") || mods.has("command")) modBits |= 4;
  if (mods.has("shift")) modBits |= 8;

  // Input.* commands don't need a domain enable; pass null so the
  // manager only handles attach refcount.
  await debuggerAttach.acquire(tabId, null);
  try {
    const named = KEY_TO_CODE[key];
    const isPrintable = key.length === 1;
    const base = named
      ? { key: key, code: named.code, windowsVirtualKeyCode: named.windowsVirtualKeyCode, modifiers: modBits }
      : { key: key, code: isPrintable ? "Key" + key.toUpperCase() : key, modifiers: modBits };
    const keyDown = Object.assign({ type: isPrintable ? "keyDown" : "rawKeyDown" }, base);
    if (isPrintable) keyDown.text = key;
    await chrome.debugger.sendCommand(target, "Input.dispatchKeyEvent", keyDown);
    await chrome.debugger.sendCommand(target, "Input.dispatchKeyEvent", Object.assign({ type: "keyUp" }, base));
  } finally {
    await debuggerAttach.release(tabId);
  }
}

async function clipboardRead(tabId) {
  // navigator.clipboard requires a focused, secure-context page. We
  // run in the page's MAIN world; the extension's clipboardRead
  // permission grants the underlying access. Many sites strip the
  // permission with a page-level CSP, so callers should expect this
  // to fail on locked-down pages.
  const [{ result } = {}] = await chrome.scripting.executeScript({
    target: { tabId },
    world: "MAIN",
    func: async () => {
      try {
        const v = await navigator.clipboard.readText();
        return { value: v };
      } catch (e) {
        return { error: String(e && e.message || e) };
      }
    },
  });
  if (result && result.error) throw new Error("clipboard_read: " + result.error);
  return { data: (result && result.value) || "" };
}

async function clipboardWrite(tabId, params) {
  const text = params.text || "";
  const [{ result } = {}] = await chrome.scripting.executeScript({
    target: { tabId },
    args: [text],
    world: "MAIN",
    func: async (text) => {
      try {
        await navigator.clipboard.writeText(text);
        return { ok: true };
      } catch (e) {
        return { error: String(e && e.message || e) };
      }
    },
  });
  if (result && result.error) throw new Error("clipboard_write: " + result.error);
  return { content: "wrote " + text.length + " chars" };
}

async function storageGet(tabId, params) {
  const kind = (params.storage || "local").toLowerCase();
  if (kind !== "local" && kind !== "session") {
    throw new Error(`storage_get: unknown storage "${params.storage}" (local|session)`);
  }
  const [{ result } = {}] = await chrome.scripting.executeScript({
    target: { tabId },
    args: [kind, params.key || ""],
    world: "MAIN",
    func: (kind, key) => {
      const s = kind === "session" ? sessionStorage : localStorage;
      if (key) return { value: JSON.stringify({ key: key, value: s.getItem(key) }) };
      const out = {};
      for (let i = 0; i < s.length; i++) {
        const k = s.key(i);
        out[k] = s.getItem(k);
      }
      return { value: JSON.stringify(out) };
    },
  });
  return { data: (result && result.value) || "{}" };
}

async function handleTabs(params) {
  const action = params.action || "list";
  if (action === "list") {
    const tabs = await chrome.tabs.query({});
    const lines = tabs.map((t, i) => `[${i + 1}] ${t.title || ""}\n    ${t.url || ""}`);
    return { content: lines.join("\n") };
  }
  if (action === "switch") {
    const idx = (params.tab_id || 1) - 1;
    const tabs = await chrome.tabs.query({});
    if (idx < 0 || idx >= tabs.length) throw new Error(`tab ${idx + 1} not found`);
    activeTabId = tabs[idx].id;
    await chrome.tabs.update(tabs[idx].id, { active: true });
    return { content: `switched to tab ${idx + 1}` };
  }
  if (action === "new") {
    const t = await chrome.tabs.create({ url: params.url || "about:blank" });
    activeTabId = t.id;
    return { content: `opened tab ${t.id}` };
  }
  if (action === "close") {
    const tid = await targetTabId();
    await chrome.tabs.remove(tid);
    activeTabId = null;
    return { content: `closed tab ${tid}` };
  }
  throw new Error(`unknown tab action: ${action}`);
}

// --- DevTools-flavored actions (network_list / console_get / perf_* /
//     lighthouse). Match probe response shapes so the agent sees
//     identical JSON across modes. ----------------------------------

async function networkList(tabId) {
  // Sticky acquire — Network keeps recording after the read so a
  // follow-up call sees more entries. Caller pays no extra detach cost
  // on each read.
  await debuggerAttach.acquire(tabId, "Network");
  return { data: JSON.stringify({ count: netRing.length, entries: netRing }) };
}

async function consoleGet(tabId) {
  await debuggerAttach.acquire(tabId, "Runtime");
  return { data: JSON.stringify({ count: consoleRing.length, entries: consoleRing }) };
}

async function perfStart(tabId) {
  if (traceState.active) {
    throw new Error("perf_start: trace already active — call perf_stop first");
  }
  // Tracing.start enables the domain itself; no <Domain>.enable needed.
  await debuggerAttach.acquire(tabId, null);
  try {
    await chrome.debugger.sendCommand({ tabId }, "Tracing.start", {
      transferMode: "ReportEvents",
    });
  } catch (e) {
    await debuggerAttach.release(tabId);
    throw e;
  }
  traceState.active = true;
  traceState.startedAt = Date.now();
  traceState.eventCount = 0;
  return { data: "tracing started" };
}

async function perfStop(tabId) {
  if (!traceState.active) {
    throw new Error("perf_stop: no active trace");
  }
  const startedAt = traceState.startedAt;
  try {
    await chrome.debugger.sendCommand({ tabId }, "Tracing.end");
  } catch (e) {
    // Best-effort cleanup; surface the error.
    traceState.active = false;
    await debuggerAttach.release(tabId);
    throw e;
  }
  // Drain the final dataCollected events; CDP buffers them after End().
  // 250ms matches probe.doPerfStop.
  await new Promise((r) => setTimeout(r, 250));
  const count = traceState.eventCount;
  traceState.active = false;
  await debuggerAttach.release(tabId);
  return {
    data: JSON.stringify({
      duration_ms: Date.now() - startedAt,
      event_count: count,
      note: "raw trace events are dropped after counting — perf_stop returns aggregate only",
    }),
  };
}

async function lighthouse(tabId) {
  // Mirrors probe's lighthouseScript verbatim so agents get identical
  // JSON across modes. Not full Lighthouse — Core Web Vitals + Navigation
  // Timing only. No chrome.debugger needed; runs as a MAIN-world script.
  const [{ result } = {}] = await chrome.scripting.executeScript({
    target: { tabId },
    world: "MAIN",
    func: () => {
      const out = {
        mode: "core-web-vitals",
        paint: {},
        navigation: {},
        largest_contentful_paint_ms: null,
        cumulative_layout_shift: null,
        first_input_delay_ms: null,
        resource_count: 0,
        notes: [],
      };
      try {
        for (const p of performance.getEntriesByType("paint")) {
          out.paint[p.name.replace(/-/g, "_")] = Math.round(p.startTime);
        }
        const nav = performance.getEntriesByType("navigation")[0];
        if (nav) {
          out.navigation = {
            ttfb_ms: Math.round(nav.responseStart - nav.requestStart),
            dom_content_loaded_ms: Math.round(nav.domContentLoadedEventEnd),
            load_event_ms: Math.round(nav.loadEventEnd),
            transfer_size: nav.transferSize,
            encoded_body_size: nav.encodedBodySize,
            type: nav.type,
          };
        }
        const lcps = performance.getEntriesByType("largest-contentful-paint");
        if (lcps && lcps.length) {
          out.largest_contentful_paint_ms = Math.round(lcps[lcps.length - 1].startTime);
        }
        let cls = 0;
        for (const ls of performance.getEntriesByType("layout-shift")) {
          if (!ls.hadRecentInput) cls += ls.value;
        }
        out.cumulative_layout_shift = Number(cls.toFixed(4));
        const fids = performance.getEntriesByType("first-input");
        if (fids && fids.length) {
          out.first_input_delay_ms = Math.round(fids[0].processingStart - fids[0].startTime);
        }
        out.resource_count = performance.getEntriesByType("resource").length;
        if (out.largest_contentful_paint_ms === null) {
          out.notes.push("LCP not yet observed — observer needs to have run since page load. Navigate and wait a beat.");
        }
        if (out.first_input_delay_ms === null) {
          out.notes.push("FID not observed — fires only on the first user interaction.");
        }
      } catch (e) {
        out.error = String(e);
      }
      return JSON.stringify(out);
    },
  });
  return { data: result || "{}" };
}
