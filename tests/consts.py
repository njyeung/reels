import subprocess

PROJECT_ROOT = subprocess.run(
    ["git", "rev-parse", "--show-toplevel"],
    capture_output=True, text=True, check=True
).stdout.strip()

# For ChromeDP
CDP_HOST = "127.0.0.1"
CDP_PORT = 6767

# For kitty driver
KITTY_SOCKET_PATH = "/tmp/reels-test.sock"