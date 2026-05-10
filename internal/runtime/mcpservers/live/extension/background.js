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

// --- entry: open/close commands from popup ---

chrome.runtime.onMessage.addListener((msg, _sender, sendResponse) => {
  if (msg && msg.type === "ycode-live:connect") {
    connectedPort = msg.port || DEFAULT_PORT;
    if (msg.tabId) {
      activeTabId = msg.tabId;
    }
    openWS();
    sendResponse({ status: "connecting", port: connectedPort });
  } else if (msg && msg.type === "ycode-live:disconnect") {
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
  setTimeout(openWS, reconnectMs);
  reconnectMs = Math.min(reconnectMs * 2, RECONNECT_MAX_MS);
}

chrome.alarms.onAlarm.addListener((alarm) => {
  if (alarm.name === "ycode-live-keepalive" && ws && ws.readyState === WebSocket.OPEN) {
    // No-op send keeps the MV3 worker alive without server traffic.
    try { ws.send(JSON.stringify({ id: 0, method: "_ping" })); } catch (e) { /* ignore */ }
  }
});

// --- request dispatch ---

async function onMessage(ev) {
  let req;
  try {
    req = JSON.parse(ev.data);
  } catch (e) {
    return; // not our protocol
  }
  if (!req || typeof req.id !== "number") return;
  if (req.method === "_pong" || req.method === "_ping") return;

  try {
    const result = await dispatch(req.method, req.params || {});
    ws.send(JSON.stringify({ id: req.id, result }));
  } catch (err) {
    ws.send(JSON.stringify({ id: req.id, error: String(err.message || err) }));
  }
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
  const tabId = await targetTabId();
  switch (method) {
    case "navigate":
      return navigate(tabId, params.url);
    case "back":
      return chrome.tabs.goBack(tabId).then(() => extractInTab(tabId));
    case "screenshot":
      return takeScreenshot(tabId);
    case "extract":
      return extractInTab(tabId);
    case "click":
      return runInTab(tabId, "click", params);
    case "type":
      return runInTab(tabId, "type", params);
    case "scroll":
      return runInTab(tabId, "scroll", params);
    case "tabs":
      return handleTabs(params);
  }
  throw new Error(`unknown method: ${method}`);
}

async function navigate(tabId, url) {
  if (!url) throw new Error("navigate: url required");
  await chrome.tabs.update(tabId, { url });
  await waitForLoad(tabId);
  return extractInTab(tabId);
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
  return { image: idx >= 0 ? dataUrl.slice(idx + 1) : dataUrl };
}

async function extractInTab(tabId) {
  const [{ result } = {}] = await chrome.scripting.executeScript({
    target: { tabId },
    func: () => {
      const elements = [];
      const interactive = document.querySelectorAll(
        "a, button, input, select, textarea, [role='button'], [role='link']"
      );
      for (let i = 0; i < Math.min(interactive.length, 50); i++) {
        const el = interactive[i];
        const tag = el.tagName.toLowerCase();
        const text = (el.innerText || "").trim().slice(0, 80);
        const attrs = [];
        for (const a of ["type", "placeholder", "href", "name", "value", "role"]) {
          const v = el.getAttribute(a);
          if (v) attrs.push(`${a}="${v.slice(0, 60)}"`);
        }
        elements.push(`[${i + 1}] <${tag} ${attrs.join(" ")}>${text}</${tag}>`);
      }
      const body = (document.body && document.body.innerText) || "";
      return {
        title: document.title,
        url: location.href,
        content: body.length > 16000 ? body.slice(0, 16000) + "\n... (truncated)" : body,
        elements: elements.join("\n"),
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
      function resolveTarget() {
        if (params.selector) return document.querySelector(params.selector);
        if (params.element_id && params.element_id > 0) {
          const list = document.querySelectorAll(
            "a, button, input, select, textarea, [role='button'], [role='link']"
          );
          return list[params.element_id - 1] || null;
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
