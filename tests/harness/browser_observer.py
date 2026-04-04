"""
Connects to Chrome's DevTools Protocol on port 6767
to observe browser state independently of the reels app.
"""

import json
import websocket
import requests
from consts import CDP_HOST, CDP_PORT

class BrowserObserver:
    def __init__(self):
        self._ws = None
        self._msg_id = 0
    
    def _send(self, method: str, params: dict = None):
        self._msg_id += 1
        msg = {
            "id": self._msg_id, 
            "method": method
        }
        if params:
            msg["params"] = params
        self._ws.send(json.dumps(msg))

        while True:
            resp = json.loads(self._ws.recv())
            if resp.get("id") == self._msg_id:
                return resp

    def get_current_url(self):
        resp = self._send("Runtime.evaluate", {
        "expression": "window.location.href"
        })
        return resp["result"]["result"]["value"]

    def evaluate_js(self, expression):
        resp = self._send("Runtime.evaluate", {"expression": expression})
        return resp.get("result", {}).get("result", {})
    
    # initial connection and cleanup
    def __enter__(self):
        url = ""
        resp = requests.get(f"http://{CDP_HOST}:{CDP_PORT}/json")
        resp.raise_for_status()
        targets = resp.json()
        for target in targets:
            if target.get("type") == "page" and "instagram.com" in target["url"]:
                url = target["webSocketDebuggerUrl"]
        
        self._ws = websocket.create_connection(url)
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        if self._ws:
            self._ws.close()
            self._ws = None