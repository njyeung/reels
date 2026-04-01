"""
Drives the reels TUI by spawning it in a Kitty terminal window
and sending keystrokes via Kitty's remote control protocol.
"""

import subprocess
import time
import json


SOCKET_PATH = "/tmp/reels-test.sock"


def build_binary(repo_root: str) -> str:
    """Build the reels binary, return path to it."""
    binary = f"{repo_root}/reels"
    result = subprocess.run(
        ["go", "build", "-o", binary, "."],
        cwd=repo_root,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        raise RuntimeError(f"go build failed:\n{result.stderr}")
    return binary


def launch_app(binary: str) -> subprocess.Popen:
    """
    Spawn a new Kitty window running the reels binary.
    Returns the Popen handle for the Kitty OS window.
    """
    args = [binary, "--headed"]

    proc = subprocess.Popen(
        [
            "kitty",
            "--listen-on", f"unix:{SOCKET_PATH}",
            "--override", "allow_remote_control=yes",
            "--title", "reels-test",
            *args,
        ],
    )
    time.sleep(2)
    return proc


def send_key(key: str):
    """
    Send a keystroke to the reels Kitty window.
    Uses kitty's remote control protocol over the unix socket.
    """
    subprocess.run(
        [
            "kitten", "@",
            "--to", f"unix:{SOCKET_PATH}",
            "send-text",
            "--match", "title:reels-test",
            key,
        ],
        capture_output=True,
        text=True,
    )


def send_key_with_delay(key: str, base_delay: float = 1.0):
    """Send a key, then wait with a human-like delay."""
    import random
    send_key(key)
    delay = base_delay + random.uniform(0, 0.5)
    time.sleep(delay)


def close_app(proc: subprocess.Popen):
    """Send 'q' to quit, then clean up."""
    try:
        send_key("q")
        proc.wait(timeout=5)
    except subprocess.TimeoutExpired:
        proc.terminate()
        proc.wait(timeout=3)
