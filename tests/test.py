import sys
import time
import os

sys.path.insert(0, os.path.dirname(__file__))

from harness.kitty_driver import build_binary, launch_app, send_key, close_app
from harness.browser_observer import connect, get_current_url, disconnect

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

def main():
    # build and launch app
    binary = build_binary(REPO_ROOT)
    proc = launch_app(binary, headed=True)

    # connect to chrome
    connected = False
    for attempt in range(60):
        try:
            connect()
            connected = True
            break
        except Exception as e:
            time.sleep(2)

    if not connected:
        close_app(proc)
        sys.exit(1)
    
    url = get_current_url()

    disconnect()

    print("SUCCESS - harness can talk to both Kitty and Chrome")

    try:
        proc.wait()
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
