import sys
sys.path.insert(0, "harness")

from reels_harness import ReelsTestHarness

with ReelsTestHarness() as h:
    url = h.get_url()
    print(f"initial url: {url}")

    h.press("j")

    h.assert_url_changed(url, timeout=5)
    new_url = h.get_url()
    print(f"after scroll: {new_url}")

    print("PASS")
