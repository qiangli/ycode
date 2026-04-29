package browseruse

// dockerfile is the Dockerfile for the browser-use container image.
// It installs browser-use, Playwright, and Chromium inside the container.
const dockerfile = `FROM python:3.12-slim

# Install system dependencies for Chromium.
RUN apt-get update && apt-get install -y --no-install-recommends \
    libnss3 libatk1.0-0 libatk-bridge2.0-0 libcups2 libdrm2 \
    libxkbcommon0 libxcomposite1 libxdamage1 libxfixes3 libxrandr2 \
    libgbm1 libpango-1.0-0 libcairo2 libasound2 libx11-xcb1 \
    fonts-liberation wget ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Install browser-use and playwright.
RUN pip install --no-cache-dir browser-use playwright \
    && playwright install chromium

COPY entrypoint.py /app/entrypoint.py

WORKDIR /app
`

// entrypointPy is the Python entrypoint that bridges JSON actions to browser-use.
// It reads a JSON action from stdin, dispatches it, and writes JSON output to stdout.
const entrypointPy = `#!/usr/bin/env python3
"""Thin JSON bridge between ycode container exec and browser-use.

Reads a JSON action from stdin, dispatches to the appropriate browser-use
function, and writes a JSON result to stdout.
"""
import json
import sys
import asyncio
import os

# Global browser state (persists across exec calls via module cache).
_browser = None
_context = None
_page = None


async def get_page():
    """Get or create the browser page."""
    global _browser, _context, _page
    if _page is not None:
        return _page

    from playwright.async_api import async_playwright
    pw = await async_playwright().start()
    _browser = await pw.chromium.launch(headless=True, args=[
        "--no-sandbox",
        "--disable-dev-shm-usage",
        "--disable-gpu",
    ])
    _context = await _browser.new_context(
        viewport={"width": 1280, "height": 720},
        user_agent="ycode/1.0 (browser-use)",
    )
    _page = await _context.new_page()
    return _page


def check_domain(url):
    """Check if URL is in allowed domains list."""
    allowed = os.environ.get("ALLOWED_DOMAINS")
    if not allowed:
        return True
    import urllib.parse
    domains = json.loads(allowed)
    parsed = urllib.parse.urlparse(url)
    return any(parsed.hostname == d or parsed.hostname.endswith("." + d) for d in domains)


async def handle_navigate(action):
    url = action.get("url", "")
    if not check_domain(url):
        return {"success": False, "error": f"Domain not allowed: {url}"}
    for scheme in ("file://", "javascript:", "data:"):
        if url.lower().startswith(scheme):
            return {"success": False, "error": f"Blocked URL scheme: {scheme}"}

    page = await get_page()
    await page.goto(url, wait_until="domcontentloaded", timeout=20000)
    title = await page.title()
    content = await page.inner_text("body")
    # Truncate content for LLM consumption.
    if len(content) > 16000:
        content = content[:16000] + "\n... (truncated)"

    # Extract interactive elements.
    elements = await extract_elements(page)
    return {
        "success": True,
        "title": title,
        "url": page.url,
        "content": content,
        "elements": elements,
    }


async def handle_click(action):
    page = await get_page()
    selector = action.get("selector", "")
    element_id = action.get("element_id", 0)

    if element_id > 0:
        selector = f"[data-ycode-id='{element_id}']"
        # Try nth-match approach for indexed elements.
        elements = await page.query_selector_all("a, button, input, select, textarea, [role='button'], [role='link']")
        if element_id <= len(elements):
            await elements[element_id - 1].click(timeout=5000)
        else:
            return {"success": False, "error": f"Element {element_id} not found (max: {len(elements)})"}
    elif selector:
        await page.click(selector, timeout=5000)
    else:
        return {"success": False, "error": "No selector or element_id provided"}

    await page.wait_for_load_state("domcontentloaded", timeout=10000)
    title = await page.title()
    content = await page.inner_text("body")
    if len(content) > 16000:
        content = content[:16000] + "\n... (truncated)"
    elements = await extract_elements(page)
    return {"success": True, "title": title, "url": page.url, "content": content, "elements": elements}


async def handle_type(action):
    page = await get_page()
    selector = action.get("selector", "")
    text = action.get("text", "")
    element_id = action.get("element_id", 0)

    if element_id > 0:
        inputs = await page.query_selector_all("input, textarea, [contenteditable='true']")
        if element_id <= len(inputs):
            await inputs[element_id - 1].fill(text)
        else:
            return {"success": False, "error": f"Input element {element_id} not found"}
    elif selector:
        await page.fill(selector, text)
    else:
        return {"success": False, "error": "No selector or element_id provided"}

    return {"success": True, "content": f"Typed '{text}'"}


async def handle_scroll(action):
    page = await get_page()
    direction = action.get("direction", "down")
    amount = action.get("amount", 500)
    if direction == "up":
        amount = -abs(amount)
    await page.evaluate(f"window.scrollBy(0, {amount})")
    content = await page.inner_text("body")
    if len(content) > 16000:
        content = content[:16000] + "\n... (truncated)"
    return {"success": True, "content": content}


async def handle_screenshot(action):
    page = await get_page()
    import base64
    screenshot = await page.screenshot(full_page=False)
    return {"success": True, "image": base64.b64encode(screenshot).decode()}


async def handle_extract(action):
    page = await get_page()
    goal = action.get("goal", "")
    # Extract all text content and let the LLM process it.
    content = await page.inner_text("body")
    if len(content) > 32000:
        content = content[:32000] + "\n... (truncated)"
    title = await page.title()
    return {"success": True, "title": title, "url": page.url, "content": content, "data": goal}


async def handle_back(action):
    page = await get_page()
    await page.go_back(wait_until="domcontentloaded", timeout=10000)
    title = await page.title()
    content = await page.inner_text("body")
    if len(content) > 16000:
        content = content[:16000] + "\n... (truncated)"
    elements = await extract_elements(page)
    return {"success": True, "title": title, "url": page.url, "content": content, "elements": elements}


async def handle_tabs(action):
    global _page, _context
    tab_action = action.get("tab_action", "list")

    if tab_action == "list":
        pages = _context.pages if _context else []
        tabs = [{"id": i + 1, "url": p.url, "title": await p.title()} for i, p in enumerate(pages)]
        return {"success": True, "data": json.dumps(tabs)}
    elif tab_action == "switch":
        tab_id = action.get("tab_id", 1) - 1
        pages = _context.pages if _context else []
        if 0 <= tab_id < len(pages):
            _page = pages[tab_id]
            await _page.bring_to_front()
            return {"success": True, "title": await _page.title(), "url": _page.url}
        return {"success": False, "error": f"Tab {tab_id + 1} not found"}
    elif tab_action == "new":
        _page = await _context.new_page()
        return {"success": True, "content": "New tab opened"}
    elif tab_action == "close":
        if len(_context.pages) > 1:
            await _page.close()
            _page = _context.pages[-1]
            return {"success": True, "content": "Tab closed"}
        return {"success": False, "error": "Cannot close last tab"}
    return {"success": False, "error": f"Unknown tab action: {tab_action}"}


async def extract_elements(page):
    """Extract interactive elements with indices for LLM reference."""
    elements = await page.query_selector_all(
        "a, button, input, select, textarea, [role='button'], [role='link'], [role='tab']"
    )
    lines = []
    for i, el in enumerate(elements[:50], 1):  # Limit to 50 elements.
        tag = await el.evaluate("e => e.tagName.toLowerCase()")
        text = (await el.inner_text()).strip()[:80] if await el.inner_text() else ""
        attrs = {}
        for attr in ["type", "placeholder", "href", "name", "value", "role"]:
            val = await el.get_attribute(attr)
            if val:
                attrs[attr] = val[:60]

        attr_str = " ".join(f'{k}="{v}"' for k, v in attrs.items())
        if text:
            lines.append(f"[{i}] <{tag} {attr_str}>{text}</{tag}>")
        else:
            lines.append(f"[{i}] <{tag} {attr_str}/>")
    return "\n".join(lines)


HANDLERS = {
    "navigate": handle_navigate,
    "click": handle_click,
    "type": handle_type,
    "scroll": handle_scroll,
    "screenshot": handle_screenshot,
    "extract": handle_extract,
    "back": handle_back,
    "tabs": handle_tabs,
}


async def main():
    raw = sys.stdin.read().strip()
    if not raw:
        print(json.dumps({"success": False, "error": "No input provided"}))
        return

    try:
        action = json.loads(raw)
    except json.JSONDecodeError as e:
        print(json.dumps({"success": False, "error": f"Invalid JSON: {e}"}))
        return

    action_type = action.get("action", "")
    handler = HANDLERS.get(action_type)
    if not handler:
        print(json.dumps({"success": False, "error": f"Unknown action: {action_type}"}))
        return

    try:
        result = await handler(action)
        print(json.dumps(result))
    except Exception as e:
        print(json.dumps({"success": False, "error": str(e)}))


if __name__ == "__main__":
    asyncio.run(main())
`
