#!/usr/bin/env node
// Simple chat agent using ycode with local Ollama models.
// ycode provides the full agentic capabilities: tools (bash, file ops, search),
// memory, code understanding — same as the ycode TUI/web UI.
//
// Usage:
//   node index.js "explain what this project does"
//   node index.js                # interactive mode
//
// Prerequisites:
//   - ycode server running: `ycode serve` (auto-starts Ollama + all services)
//   - npm install ws readline

const { WebSocket } = require("ws");
const { readFileSync } = require("fs");
const readline = require("readline");
const os = require("os");
const path = require("path");

const PORT_PATH = path.join(os.homedir(), ".agents", "ycode", "serve.port");
const TOKEN_PATH = path.join(os.homedir(), ".agents", "ycode", "server.token");

function discoverPort() {
  try { return parseInt(readFileSync(PORT_PATH, "utf-8").trim(), 10); }
  catch { return 58080; }
}

function readToken() {
  try { return readFileSync(TOKEN_PATH, "utf-8").trim(); }
  catch { return ""; }
}

const BASE_URL = process.env.YCODE_URL || `http://127.0.0.1:${discoverPort()}`;
const API_BASE = `${BASE_URL}/ycode`;

async function request(method, endpoint, body) {
  const headers = { "Content-Type": "application/json" };
  const token = readToken();
  if (token) headers["Authorization"] = `Bearer ${token}`;
  const resp = await fetch(`${API_BASE}${endpoint}`, {
    method, headers, body: body ? JSON.stringify(body) : undefined,
  });
  if (!resp.ok) throw new Error(`${method} ${endpoint}: ${resp.status}`);
  return resp.json();
}

async function chat(sessionID, prompt) {
  const wsURL = API_BASE.replace("http", "ws") + `/api/sessions/${sessionID}/ws`;
  const ws = new WebSocket(wsURL);

  await new Promise((resolve, reject) => {
    ws.on("open", resolve);
    ws.on("error", reject);
  });

  ws.send(JSON.stringify({ type: "message.send", data: { text: prompt } }));

  return new Promise((resolve) => {
    ws.on("message", (raw) => {
      const event = JSON.parse(raw.toString());
      switch (event.type) {
        case "text.delta":
          process.stdout.write(event.data.text);
          break;
        case "tool_use.start":
          process.stderr.write(`\n[${event.data.tool}] ${event.data.detail || ""}\n`);
          break;
        case "turn.complete":
          process.stdout.write("\n");
          ws.close();
          resolve();
          break;
        case "turn.error":
          console.error(`\nError: ${event.data.error}`);
          ws.close();
          resolve();
          break;
      }
    });
  });
}

async function main() {
  // Verify server is running.
  try {
    await request("GET", "/api/health");
  } catch {
    console.error(`Cannot reach ycode server at ${BASE_URL}.`);
    console.error("Start it with: ycode serve");
    process.exit(1);
  }

  // Get active session.
  const status = await request("GET", "/api/status");
  const sessionID = status.session_id;
  console.error(`Connected to ycode agent (model: ${status.model}, session: ${sessionID})`);
  console.error("Full agentic mode: tools, memory, code understanding\n");

  const prompt = process.argv.slice(2).join(" ");
  if (prompt) {
    // One-shot mode.
    await chat(sessionID, prompt);
  } else {
    // Interactive mode.
    const rl = readline.createInterface({ input: process.stdin, output: process.stdout });
    const ask = () => rl.question("> ", async (text) => {
      if (!text.trim()) { ask(); return; }
      if (text.trim() === "/quit") { rl.close(); return; }
      await chat(sessionID, text);
      ask();
    });
    ask();
  }
}

main().catch((err) => { console.error(err.message); process.exit(1); });
