package backend

import (
	"context"
	"encoding/base64"
	"fmt"
	"math/rand"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

// pkRegex extracts ig_cache_key from a URL — used to recover the visible
// reel's PK from its cached preview image src.
var pkRegex = regexp.MustCompile(`ig_cache_key=([^&]+)`)

// Cursor abstracts how the user navigates a list of reels in the browser.
// FeedCursor scrolls the main reels page; a future FriendCursor will navigate
// to specific reel URLs in a secondary DM window.
type Cursor interface {
	// Current returns the (1-based index, PK) of the reel the user is looking
	// at. For feed, this probes the DOM. For friend (future), it reads the
	// cursor index directly.
	Current() (index int, pk string, err error)

	// Total returns the number of reels in this source.
	Total() int

	// PKAt returns the PK at 1-based index, or "" if out of range.
	PKAt(index int) string

	// SyncTo navigates the browser so the reel at index is visible/active.
	SyncTo(index int) error

	// IsSyncing reports whether a SyncTo is in flight.
	IsSyncing() bool
}

// FeedCursor navigates the main /reels page by scrolling. PKs are appended
// as Instagram returns clip responses (see processReelResponse); discovery is
// implicit via fetch interception, not via the cursor itself.
type FeedCursor struct {
	ctx context.Context

	mu  sync.RWMutex
	pks []string

	syncMu     sync.Mutex
	syncCtx    context.Context
	syncCancel context.CancelFunc
}

// NewFeedCursor wires the cursor to the feed window's chromedp context.
func NewFeedCursor(ctx context.Context) *FeedCursor {
	return &FeedCursor{ctx: ctx}
}

// append records a newly captured PK at the tail. The caller (processReelResponse)
// has already deduped via the reels map, so no membership check is needed here.
// Caller must hold ChromeBackend.reelsMu so the b.reels insert and this append
// are atomic — otherwise readers can observe a PK in b.reels before it shows
// up in fc.pks, producing transient "pk not in captured list" errors.
func (fc *FeedCursor) append(pk string) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.pks = append(fc.pks, pk)
}

// Total returns the number of captured reels.
func (fc *FeedCursor) Total() int {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	return len(fc.pks)
}

// PKAt returns the PK at 1-based index, or "" if out of range.
func (fc *FeedCursor) PKAt(index int) string {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	if index < 1 || index > len(fc.pks) {
		return ""
	}
	return fc.pks[index-1]
}

// indexOf returns the 1-based index of pk in fc.pks, or 0 if absent. Caller
// must not hold fc.mu.
func (fc *FeedCursor) indexOf(pk string) int {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	for i, p := range fc.pks {
		if p == pk {
			return i + 1
		}
	}
	return 0
}

// Current probes the DOM for the visible reel and resolves it to a 1-based
// index in the captured list.
func (fc *FeedCursor) Current() (int, string, error) {
	pk, err := fc.domPK()
	if err != nil {
		return 0, "", err
	}
	idx := fc.indexOf(pk)
	if idx == 0 {
		return 0, "", fmt.Errorf("reel pk=%s not in captured list", pk)
	}
	return idx, pk, nil
}

// domPK extracts the pk of the currently visible reel from the DOM.
func (fc *FeedCursor) domPK() (string, error) {
	var imgSrc string
	js := `
		(() => {
			const videos = document.querySelectorAll('video[playsinline]');
			for (const video of videos) {
				const rect = video.getBoundingClientRect();
				const viewportHeight = window.innerHeight;
				const videoCenter = rect.top + rect.height / 2;
				if (videoCenter > 0 && videoCenter < viewportHeight) {
					let parent = video.parentElement;
					for (let i = 0; i < 10; i++) {
						if (!parent) break;
						const img = parent.querySelector('img[src*="ig_cache_key"]');
						if (img) return img.src;
						parent = parent.parentElement;
					}
				}
			}
			return "";
		})()
	`

	if err := chromedp.Run(fc.ctx, chromedp.Evaluate(js, &imgSrc)); err != nil {
		return "", err
	}

	if imgSrc == "" {
		return "", fmt.Errorf("no visible reel found")
	}

	matches := pkRegex.FindStringSubmatch(imgSrc)
	if len(matches) < 2 {
		return "", fmt.Errorf("no ig_cache_key found")
	}

	decoded, err := url.QueryUnescape(matches[1])
	if err != nil {
		return "", err
	}
	b64Part := strings.Split(decoded, ".")[0]

	pkBytes, err := base64.StdEncoding.DecodeString(b64Part)
	if err != nil {
		return "", err
	}

	pk := string(pkBytes)
	if len(pk) > InstagramPKLength {
		pk = pk[:InstagramPKLength]
	}
	return pk, nil
}

// scrollDown sends a single ArrowDown to advance to the next reel.
func (fc *FeedCursor) scrollDown() error {
	return chromedp.Run(fc.ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			return input.DispatchKeyEvent(input.KeyDown).
				WithKey("ArrowDown").
				WithCode("ArrowDown").
				WithWindowsVirtualKeyCode(40).
				WithNativeVirtualKeyCode(40).
				Do(ctx)
		}),
	)
}

// scrollUp sends a single ArrowUp to go back to the previous reel.
func (fc *FeedCursor) scrollUp() error {
	return chromedp.Run(fc.ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			return input.DispatchKeyEvent(input.KeyDown).
				WithKey("ArrowUp").
				WithCode("ArrowUp").
				WithWindowsVirtualKeyCode(38).
				WithNativeVirtualKeyCode(38).
				Do(ctx)
		}),
	)
}

// SyncTo scrolls the feed window until the reel at index is the visible one.
// Cancels any in-flight SyncTo so a newer one can supersede it.
func (fc *FeedCursor) SyncTo(index int) error {
	fc.syncMu.Lock()
	if fc.syncCancel != nil {
		fc.syncCancel()
	}
	ctx, cancel := context.WithCancel(fc.ctx)
	fc.syncCtx = ctx
	fc.syncCancel = cancel
	fc.syncMu.Unlock()

	defer cancel()

	currentPK, _ := fc.domPK()

	fc.mu.RLock()
	if index < 1 || index > len(fc.pks) {
		fc.mu.RUnlock()
		return fmt.Errorf("index %d out of range", index)
	}
	targetPK := fc.pks[index-1]
	if currentPK == targetPK {
		fc.mu.RUnlock()
		return nil
	}
	currentIndex := 0
	for i, pk := range fc.pks {
		if pk == currentPK {
			currentIndex = i + 1
			break
		}
	}
	fc.mu.RUnlock()

	for i := 0; i < MaxRetries; i++ {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		pk, err := fc.domPK()
		if err == nil && pk == targetPK {
			return nil
		}

		if err == nil {
			if idx := fc.indexOf(pk); idx != 0 {
				currentIndex = idx
			}
		}

		if currentIndex < index {
			if err := fc.scrollDown(); err != nil {
				return err
			}
		} else if currentIndex > index {
			if err := fc.scrollUp(); err != nil {
				return err
			}
		} else {
			// index matches but PK doesn't; try scrolling down to recover
			if err := fc.scrollDown(); err != nil {
				return err
			}
		}

		time.Sleep(time.Duration(1500+rand.Intn(500)) * time.Millisecond)
	}

	return fmt.Errorf("failed to sync to index %d after %d scrolls", index, MaxRetries)
}

// IsSyncing returns true if a SyncTo is in flight (its derived ctx not yet done).
func (fc *FeedCursor) IsSyncing() bool {
	fc.syncMu.Lock()
	defer fc.syncMu.Unlock()
	return fc.syncCtx != nil && fc.syncCtx.Err() == nil
}
