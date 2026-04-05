import random
from harness.reels_harness import ReelsTestHarness
from utils import sleep, extract_reel_code
with ReelsTestHarness() as h:
    url = h.get_url()
    print(f"initial url: {url}")

    for i in range(500):
        h.press("j")
        sleep(7)

        new_url = h.get_url()
        assert new_url != url, f"[{i}] URL did not change after scroll: {url}"

        h.press("y")
        sleep(1)
        yanked = h.get_clipboard()

        browser_code = extract_reel_code(new_url)
        yanked_code = extract_reel_code(yanked)
        assert browser_code and browser_code == yanked_code, (
            f"[{i}] reel code mismatch: browser={new_url} yanked={yanked}"
        )

        url = new_url
        print(f"[{i}] PASS: {url}")
        sleep(random.uniform(3, 12))

    print("ALL PASS")
