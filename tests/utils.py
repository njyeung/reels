import random
import time
import re

def sleep(timeout: int):
    delay = random.uniform(0, 1)
    time.sleep(timeout + delay)

def extract_reel_code(url: str) -> str:
    m = re.search(r"/reels?/([A-Za-z0-9_-]+)", url)
    return m.group(1) if m else ""