#!/usr/bin/env python3
"""Minimal ycode client in Python.

Connects to a running ycode server, sends a prompt, and streams the response.

Usage:
    python main.py "explain what this project does"

Prerequisites:
    - ycode server running: `ycode serve` or auto-started by `ycode`
    - pip install websockets httpx  (only dependencies)
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


BASE_URL = os.environ.get("YCODE_URL", f"http://127.0.0.1:{discover_port()}")


def read_token() -> str:
    try:
        return TOKEN_PATH.read_text().strip()
    except FileNotFoundError:
        return ""


def headers() -> dict:
    h = {"Content-Type": "application/json"}
    token = read_token()
    if token:
        h["Authorization"] = f"Bearer {token}"
    return h


async def main():
    prompt = " ".join(sys.argv[1:])
    if not prompt:
        print("Usage: python main.py <prompt>", file=sys.stderr)
        sys.exit(1)

    # 1. Health check
    async with httpx.AsyncClient(base_url=BASE_URL, headers=headers()) as client:
        try:
            resp = await client.get("/ycode/api/health", timeout=2.0)
            resp.raise_for_status()
        except (httpx.ConnectError, httpx.HTTPStatusError):
            print(f"Cannot reach ycode server at {BASE_URL}. Is it running?", file=sys.stderr)
            print("Start it with: ycode serve", file=sys.stderr)
            sys.exit(1)

        # 2. Get or create session
        try:
            resp = await client.get("/ycode/api/status")
            session_id = resp.json().get("session_id")
        except Exception:
            resp = await client.post("/ycode/api/sessions", json={})
            session_id = resp.json().get("id")

        if not session_id:
            print("Failed to get session ID from server", file=sys.stderr)
            sys.exit(1)

    # 3. Connect WebSocket
    ws_url = BASE_URL.replace("http", "ws") + f"/ycode/api/sessions/{session_id}/ws"
    async with websockets.connect(ws_url) as ws:
        # 4. Send message
        await ws.send(json.dumps({"type": "message.send", "data": {"text": prompt}}))

        # 5. Stream response
        async for raw in ws:
            event = json.loads(raw)
            event_type = event.get("type")

            if event_type == "text.delta":
                print(event["data"]["text"], end="", flush=True)
            elif event_type == "tool_use.start":
                tool = event["data"].get("tool", "")
                detail = event["data"].get("detail", "")
                print(f"\n[tool: {tool}] {detail}", file=sys.stderr)
            elif event_type == "turn.complete":
                print()
                break
            elif event_type == "turn.error":
                print(f"\nError: {event['data']['error']}", file=sys.stderr)
                sys.exit(1)


if __name__ == "__main__":
    asyncio.run(main())
