"""
Drives the reels TUI by spawning it in a Kitty terminal window
and sending keystrokes via Kitty's remote control protocol.
"""

import subprocess
import time
import json
import os
import random

from consts import PROJECT_ROOT, KITTY_SOCKET_PATH

class KittyDriver:
    def __init__(self):
        self.proc = None

    def get_text(self) -> str:
        res = subprocess.run(
            [
                "kitten", "@",
                "--to", f"unix:{KITTY_SOCKET_PATH}",
                "get-text",
            ],
            capture_output=True,
            text=True,
        )
        return res.stdout

    def send_key(self, key: str):
        delay = random.uniform(0.2, 1)
        time.sleep(delay)
        subprocess.run(
            [
                "kitten", "@",
                "--to", f"unix:{KITTY_SOCKET_PATH}",
                "send-text",
                key,
            ],
            capture_output=True,
            text=True,
        )
    
    def __enter__(self):
        # build the binary
        binary = os.path.join(PROJECT_ROOT, "reels")
        res = subprocess.run(
            ["go", "build", "-o", binary, "."],
            cwd=PROJECT_ROOT,
            capture_output=True,
            text=True,
        )
        if res.returncode != 0:
            raise RuntimeError(f"go build failed:\n{res.stderr}")
        
        # start program on kitty
        args = [binary, "--headed"]

        self.proc = subprocess.Popen(
            [
                "kitty",
                "--listen-on", f"unix:{KITTY_SOCKET_PATH}",
                "--override", "allow_remote_control=yes",
                "--title", "reels-test",
                *args,
            ],
        )
        time.sleep(2)

        # Wait for app to finish sync cycle.
        # The loading screen (view_loading.go) shows the REELS ASCII logo.
        # Once the first reel loads, it shows @username.
        # We poll the terminal text until we find an @.
        synced = False
        for _ in range(60):
            text = self.get_text()
            if "@" in text:
                synced = True
                break
            time.sleep(2)
        if not synced:
            raise TimeoutError("App did not finish loading within 60 seconds")

        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        try:
            self.send_key("q")
            self.proc.wait(timeout=3)
        except subprocess.TimeoutExpired:
            self.proc.terminate()
            self.proc.wait(timeout=3)