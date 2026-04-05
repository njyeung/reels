import subprocess
import time
import random
from harness.kitty_driver import KittyDriver
from harness.browser_observer import BrowserObserver


class ReelsTestHarness:
    def __init__(self):
        self.kitty = KittyDriver()
        self.browser = BrowserObserver()

    def __enter__(self):
        self.kitty.__enter__()
        self.browser.__enter__()
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        self.browser.__exit__(exc_type, exc_val, exc_tb)
        self.kitty.__exit__(exc_type, exc_val, exc_tb)

    # --- Actions ---

    def press(self, key: str):
        self.kitty.send_key(key)

    def get_screen(self) -> str:
        return self.kitty.get_text()

    def get_url(self) -> str:
        return self.browser.get_current_url()

    def get_clipboard(self) -> str:
        res = subprocess.run(
            ["wl-paste", "--no-newline"],
            capture_output=True,
            text=True,
        )
        return res.stdout

    # --- Terminal assertions ---

    def assert_on_screen(self, text: str, timeout: int):
        deadline = time.time() + timeout
        while time.time() < deadline:
            if text in self.kitty.get_text():
                return
            time.sleep(1)
        raise AssertionError(
            f"'{text}' did not appear on screen within {timeout}s"
        )

    def assert_not_on_screen(self, text: str, timeout: int):
        deadline = time.time() + timeout
        while time.time() < deadline:
            if text not in self.kitty.get_text():
                return
            time.sleep(1)
        raise AssertionError(
            f"'{text}' was still on screen after {timeout}s"
        )

    # --- Browser assertions ---

    def assert_url_contains(self, substring: str, timeout: int):
        deadline = time.time() + timeout
        while time.time() < deadline:
            if substring in self.browser.get_current_url():
                return
            time.sleep(1)
        raise AssertionError(
            f"URL did not contain '{substring}' within {timeout}s"
        )

    def assert_url_changed(self, old_url: str, timeout: int):
        deadline = time.time() + timeout
        while time.time() < deadline:
            if self.browser.get_current_url() != old_url:
                return
            time.sleep(1)
        raise AssertionError(
            f"URL was still '{old_url}' after {timeout}s"
        )

    # --- TODO: future browser JS assertions ---

    def assert_liked(self, timeout: int):
        # TODO: evaluate JS to check if the like button is in "liked" state
        raise NotImplementedError

    def assert_saved(self, timeout: int):
        # TODO: evaluate JS to check if the save/bookmark button is active
        raise NotImplementedError

    def assert_comments_open(self, timeout: int):
        # TODO: evaluate JS to check if the comments panel is open in the browser DOM
        raise NotImplementedError

    