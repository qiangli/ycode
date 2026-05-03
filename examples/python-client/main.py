#!/usr/bin/env python3
"""Simple chat agent using ycode with local Ollama models.

ycode provides the full agentic capabilities: tools (bash, file ops, search),
memory, code understanding — same as the ycode TUI/web UI.

Usage:
    python main.py "explain what this project does"
    python main.py                # interactive mode

Prerequisites:
    - ycode server running: `ycode serve` (auto-starts Ollama + all services)
    - pip install websockets httpx
"""

import asyncio
import json
import os
import sys
from pathlib import Path

import httpx
import websockets

PORT_PATH = Path.home() / ".agents" / "ycode" / "serve.port"
TOKEN_PATH = Path.home() / ".agents" / "ycode" / "server.token"


def discover_port() -> int:
    try:
        return int(PORT_PATH.read_text().strip())
    except (FileNotFoundError, ValueError):
        return 58080


def read_token() -> str:
    try:
        return TOKEN_PATH.read_text().strip()
    except FileNotFoundError:
        return ""


BASE_URL = os.environ.get("YCODE_URL", f"http://127.0.0.1:{discover_port()}")
API_BASE = f"{BASE_URL}/ycode"


def headers() -> dict:
    h = {"Content-Type": "application/json"}
    token = read_token()
    if token:
        h["Authorization"] = f"Bearer {token}"
    return h


async def chat(session_id: str, prompt: str):
    """Send a message and stream the agent's response."""
    ws_url = API_BASE.replace("http", "ws") + f"/api/sessions/{session_id}/ws"

    async with websockets.connect(ws_url) as ws:
        await ws.send(json.dumps({"type": "message.send", "data": {"text": prompt}}))

        async for raw in ws:
            event = json.loads(raw)
            event_type = event.get("type")

            if event_type == "text.delta":
                print(event["data"]["text"], end="", flush=True)
            elif event_type == "tool_use.start":
                tool = event["data"].get("tool", "")
                detail = event["data"].get("detail", "")
                print(f"\n[{tool}] {detail}", file=sys.stderr)
            elif event_type == "turn.complete":
                print()
                break
            elif event_type == "turn.error":
                print(f"\nError: {event['data']['error']}", file=sys.stderr)
                break


async def main():
    # Verify server is running.
    async with httpx.AsyncClient(base_url=API_BASE, headers=headers()) as client:
        try:
            resp = await client.get("/api/health", timeout=2.0)
            resp.raise_for_status()
        except (httpx.ConnectError, httpx.HTTPStatusError):
            print(f"Cannot reach ycode server at {BASE_URL}.", file=sys.stderr)
            print("Start it with: ycode serve", file=sys.stderr)
            sys.exit(1)

        # Get active session.
        resp = await client.get("/api/status")
        status = resp.json()
        session_id = status.get("session_id", "")

    print(f"Connected to ycode agent (model: {status.get('model')}, session: {session_id})",
          file=sys.stderr)
    print("Full agentic mode: tools, memory, code understanding\n", file=sys.stderr)

    prompt = " ".join(sys.argv[1:])
    if prompt:
        # One-shot mode.
        await chat(session_id, prompt)
    else:
        # Interactive mode.
        try:
            while True:
                text = input("> ")
                if not text.strip():
                    continue
                if text.strip() == "/quit":
                    break
                await chat(session_id, text)
        except (EOFError, KeyboardInterrupt):
            print()


if __name__ == "__main__":
    asyncio.run(main())
