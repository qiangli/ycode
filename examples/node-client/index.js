#!/usr/bin/env node
// Minimal ycode client in Node.js.
// Connects to a running ycode server, sends a prompt, and streams the response.
//
// Usage:
//   node index.js "explain what this project does"
//
// Prerequisites:
//   - ycode server running: `ycode serve` or auto-started by `ycode`
//   - npm install ws  (only dependency)

const { WebSocket } = require("ws");
const { readFileSync, existsSync } = require("fs");
const { join } = require("os").homedir
  ? { join: require("path").join }
  : require("path");
const os = require("os");
const path = require("path");

const BASE_URL = process.env.YCODE_URL || "http://127.0.0.1:58080";
const TOKEN_PATH = path.join(os.homedir(), ".agents", "ycode", "server.token");

function readToken() {
  try {
    return readFileSync(TOKEN_PATH, "utf-8").trim();
  } catch {
    return "";
  }
}

async function request(method, endpoint, body) {
  const url = `${BASE_URL}${endpoint}`;
  const headers = { "Content-Type": "application/json" };
  const token = readToken();
  if (token) headers["Authorization"] = `Bearer ${token}`;

  const resp = await fetch(url, {
    method,
    headers,
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!resp.ok) throw new Error(`${method} ${endpoint}: ${resp.status}`);
  return resp.json();
}

async function main() {
  const prompt = process.argv.slice(2).join(" ");
  if (!prompt) {
    console.error("Usage: node index.js <prompt>");
    process.exit(1);
  }

  // 1. Health check
  try {
    await request("GET", "/api/health");
  } catch (err) {
    console.error(`Cannot reach ycode server at ${BASE_URL}. Is it running?`);
    console.error("Start it with: ycode serve");
    process.exit(1);
  }

  // 2. Get or create session
  let sessionID;
  try {
    const status = await request("GET", "/api/status");
    sessionID = status.session_id;
  } catch {
    const session = await request("POST", "/api/sessions", {});
    sessionID = session.id;
  }

  // 3. Connect WebSocket
  const wsURL = BASE_URL.replace("http", "ws") + `/api/sessions/${sessionID}/ws`;
  const ws = new WebSocket(wsURL);

  await new Promise((resolve, reject) => {
    ws.on("open", resolve);
    ws.on("error", reject);
  });

  // 4. Send message
  ws.send(JSON.stringify({ type: "message.send", data: { text: prompt } }));

  // 5. Stream response
  ws.on("message", (raw) => {
    const event = JSON.parse(raw.toString());
    switch (event.type) {
      case "text.delta":
        process.stdout.write(event.data.text);
        break;
      case "tool_use.start":
        process.stderr.write(`\n[tool: ${event.data.tool}] ${event.data.detail || ""}\n`);
        break;
      case "tool.result":
        // Tool completed, response continues
        break;
      case "turn.complete":
        process.stdout.write("\n");
        ws.close();
        break;
      case "turn.error":
        console.error(`\nError: ${event.data.error}`);
        ws.close();
        process.exit(1);
        break;
    }
  });

  ws.on("close", () => process.exit(0));
}

main().catch((err) => {
  console.error(err.message);
  process.exit(1);
});
