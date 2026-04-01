"""
Connects to Chrome's DevTools Protocol on port 6767
to observe browser state independently of the reels app.
"""

import json
import websocket
import requests


CDP_HOST = "127.0.0.1"
CDP_PORT = 6767
_ws = None
_msg_id = 0


def _get_debugger_url() -> str:
    """Discover the WebSocket debugger URL from Chrome's /json endpoint."""
    resp = requests.get(f"http://{CDP_HOST}:{CDP_PORT}/json")
    resp.raise_for_status()
    targets = resp.json()

    # Find the Instagram reels tab
    for target in targets:
        if "instagram.com" in target.get("url", ""):
            return target["webSocketDebuggerUrl"]

    # Fall back to first page target
    for target in targets:
        if target.get("type") == "page":
            return target["webSocketDebuggerUrl"]

    raise RuntimeError(f"No page targets found. Targets: {json.dumps(targets, indent=2)}")


def connect():
    """Establish a WebSocket connection to Chrome DevTools."""
    global _ws
    url = _get_debugger_url()
    _ws = websocket.create_connection(url)
    print(f"[browser_observer] connected to {url}")


def _send(method: str, params: dict = None) -> dict:
    """Send a CDP command and return the response."""
    global _msg_id
    _msg_id += 1
    msg = {"id": _msg_id, "method": method}
    if params:
        msg["params"] = params
    _ws.send(json.dumps(msg))

    # Read responses until we get ours (skip events)
    while True:
        resp = json.loads(_ws.recv())
        if resp.get("id") == _msg_id:
            return resp


def get_current_url() -> str:
    """Get the current page URL from the browser."""
    resp = _send("Runtime.evaluate", {
        "expression": "window.location.href"
    })
    return resp["result"]["result"]["value"]


def evaluate_js(expression: str) -> dict:
    """Evaluate arbitrary JS in the page and return the result."""
    resp = _send("Runtime.evaluate", {"expression": expression})
    return resp.get("result", {}).get("result", {})


def get_visible_video_src() -> str | None:
    """Try to get the src of the currently visible video element."""
    resp = _send("Runtime.evaluate", {
        "expression": """
            (() => {
                const videos = document.querySelectorAll('video');
                for (const v of videos) {
                    const rect = v.getBoundingClientRect();
                    if (rect.top >= 0 && rect.bottom <= window.innerHeight) {
                        return v.src || v.querySelector('source')?.src || null;
                    }
                }
                return null;
            })()
        """
    })
    return resp.get("result", {}).get("result", {}).get("value")


def disconnect():
    """Close the WebSocket connection."""
    global _ws
    if _ws:
        _ws.close()
        _ws = None
