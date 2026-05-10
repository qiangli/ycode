// ycode-live extension — popup
//
// Lets the user pick the port the Go-side hub is listening on
// (defaults to 58082, persists via chrome.storage.local), and
// connect/disconnect the extension to it. Connecting also captures
// the currently-active tab so requests target it.

const portEl = document.getElementById("port");
const connectBtn = document.getElementById("connect");
const disconnectBtn = document.getElementById("disconnect");
const statusEl = document.getElementById("status");

chrome.storage.local.get(["port"], ({ port }) => {
  if (typeof port === "number" && port > 0) portEl.value = port;
});

function setStatus(cls, text) {
  statusEl.className = cls;
  statusEl.textContent = text;
}

async function refreshStatus() {
  const resp = await chrome.runtime.sendMessage({ type: "ycode-live:status" });
  if (resp && resp.connected) {
    setStatus("connected", `connected on port ${resp.port}`);
  } else {
    setStatus("disconnected", "disconnected");
  }
}

connectBtn.addEventListener("click", async () => {
  const port = parseInt(portEl.value, 10) || 58082;
  await chrome.storage.local.set({ port });
  const tabs = await chrome.tabs.query({ active: true, currentWindow: true });
  const tabId = tabs.length > 0 ? tabs[0].id : null;
  setStatus("connecting", `connecting to 127.0.0.1:${port}...`);
  await chrome.runtime.sendMessage({ type: "ycode-live:connect", port, tabId });
  setTimeout(refreshStatus, 600);
});

disconnectBtn.addEventListener("click", async () => {
  await chrome.runtime.sendMessage({ type: "ycode-live:disconnect" });
  refreshStatus();
});

refreshStatus();
