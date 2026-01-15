package backend

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// compiled once for extracting ig_cache_key from URLs
var pkRegex = regexp.MustCompile(`ig_cache_key=([^&]+)`)

// NewChromeBackend creates a new Chrome-based backend
func NewChromeBackend(userDataDir, cacheDir, configDir string) *ChromeBackend {
	backend := ChromeBackend{
		orderedReels: make([]Reel, 0),
		seenPKs:      make(map[string]bool),
		events:       make(chan Event, 100),
		userDataDir:  userDataDir,
		cacheDir:     cacheDir,
		configDir:    configDir,
	}

	backend.initStorage()

	return &backend
}

// Start initializes Chrome and navigates to Instagram homepage
func (b *ChromeBackend) Start(headless bool) error {
	// Create user data directory for persistent sessions
	err := os.MkdirAll(b.userDataDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create user data dir: %w", err)
	}

	// Chrome options
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserDataDir(b.userDataDir),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
	)
	if headless {
		opts = append(opts, chromedp.Flag("headless", "new"))
	} else {
		opts = append(opts, chromedp.Flag("headless", false))
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	b.allocCancel = allocCancel

	ctx, cancel := chromedp.NewContext(allocCtx, chromedp.WithLogf(log.Printf))
	b.ctx = ctx
	b.cancel = cancel

	// network events for capturing graphql responses
	if err := chromedp.Run(ctx, network.Enable()); err != nil {
		return fmt.Errorf("failed to enable network: %w", err)
	}

	// this lets us read body before it's cleared
	err = chromedp.Run(ctx,
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{
			{URLPattern: "*graphql*", RequestStage: fetch.RequestStageResponse},
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to enable fetch: %w", err)
	}

	chromedp.ListenTarget(ctx, b.handleFetchEvent)

	// Nav to Instagram
	err = chromedp.Run(ctx,
		chromedp.Navigate("https://www.instagram.com/"),
		chromedp.Sleep(2*time.Second), // sleep to let page load
	)
	if err != nil {
		return fmt.Errorf("failed to navigate: %w", err)
	}

	return nil
}

// NeedsLogin checks if login is required by looking for login form elements
func (b *ChromeBackend) NeedsLogin() (bool, error) {
	var needsLogin bool
	err := chromedp.Run(b.ctx,
		chromedp.Evaluate(`
			document.querySelector('input[name="username"]') !== null ||
			document.querySelector('input[name="email"]') !== null ||
			document.querySelector('input[aria-label="Phone number, username, or email"]') !== null
		`, &needsLogin),
	)
	return needsLogin, err
}

// NavigateToReels goes to /reels and syncs to first captured reel
func (b *ChromeBackend) NavigateToReels() error {
	// nav to reels
	if err := chromedp.Run(b.ctx,
		chromedp.Navigate("https://www.instagram.com/reels/"),
		chromedp.Sleep(2*time.Second), // wait for reels to load (faster since it's cached)
	); err != nil {
		return fmt.Errorf("failed to navigate to reels: %w", err)
	}

	// initial sync
	for i := 0; i < MaxRetries; i++ {
		info, err := b.GetCurrent()
		if err == nil && info != nil {
			b.events <- Event{Type: EventSyncComplete, Message: "Initial sync complete"}
			return nil
		}
		if err := b.scrollDown(); err != nil {
			return err
		}
		time.Sleep(time.Duration(500+rand.Intn(500)) * time.Millisecond)
	}
	return fmt.Errorf("could not complete initial sync")
}

// Stop closes the browser
func (b *ChromeBackend) Stop() {
	if b.cancel != nil {
		b.cancel()
	}
	if b.allocCancel != nil {
		b.allocCancel()
	}
	close(b.events)
}

// Events returns the event channel
func (b *ChromeBackend) Events() <-chan Event {
	return b.events
}

// handleFetchEvent processes paused fetch events to capture response bodies
func (b *ChromeBackend) handleFetchEvent(ev interface{}) {
	switch e := ev.(type) {
	case *fetch.EventRequestPaused:
		go b.processResponse(e)
	}
}

// getCurrentPK extracts the pk of the currently visible reel from the DOM
func (b *ChromeBackend) getCurrentPK() (string, error) {
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

	if err := chromedp.Run(b.ctx, chromedp.Evaluate(js, &imgSrc)); err != nil {
		return "", err
	}

	if imgSrc == "" {
		return "", fmt.Errorf("no visible reel found")
	}

	// Extract ig_cache_key from URL
	matches := pkRegex.FindStringSubmatch(imgSrc)
	if len(matches) < 2 {
		return "", fmt.Errorf("no ig_cache_key found")
	}

	// URL decode and get base64 part
	decoded, err := url.QueryUnescape(matches[1])
	if err != nil {
		return "", err
	}
	b64Part := strings.Split(decoded, ".")[0]

	// Base64 decode to get pk
	pkBytes, err := base64.StdEncoding.DecodeString(b64Part)
	if err != nil {
		return "", err
	}

	// Instagram pks are 19 digits
	pk := string(pkBytes)
	if len(pk) > InstagramPKLength {
		pk = pk[:InstagramPKLength]
	}

	return pk, nil
}

// GetCurrent returns info about the currently visible reel
func (b *ChromeBackend) GetCurrent() (*ReelInfo, error) {
	pk, err := b.getCurrentPK()
	if err != nil {
		return nil, err
	}

	b.reelsMu.RLock()
	defer b.reelsMu.RUnlock()

	for i, reel := range b.orderedReels {
		if reel.PK == pk {
			return &ReelInfo{
				Index: i + 1,
				Total: len(b.orderedReels),
				Reel:  reel,
			}, nil
		}
	}

	return nil, fmt.Errorf("reel pk=%s not in captured list", pk)
}

// GetReel returns reel info by *1-BASED INDEX* from cache, no browser interaction
func (b *ChromeBackend) GetReel(index int) (*ReelInfo, error) {
	b.reelsMu.RLock()
	defer b.reelsMu.RUnlock()

	if index < 1 || index > len(b.orderedReels) {
		return nil, fmt.Errorf("index %d out of range (1-%d)", index, len(b.orderedReels))
	}

	reel := b.orderedReels[index-1]
	return &ReelInfo{
		Index: index,
		Total: len(b.orderedReels),
		Reel:  reel,
	}, nil
}

// GetTotal returns total number of captured reels
func (b *ChromeBackend) GetTotal() int {
	b.reelsMu.RLock()
	defer b.reelsMu.RUnlock()
	return len(b.orderedReels)
}

// SyncTo scrolls browser to match the given index
func (b *ChromeBackend) SyncTo(index int) error {
	b.syncMu.Lock()
	// if we see that there is an ongoing SyncTo call
	// we need to cancel it and start our new SyncTo.
	if b.syncCancel != nil {
		b.syncCancel()
	}
	ctx, cancel := context.WithCancel(b.ctx)
	b.syncCancel = cancel
	b.syncMu.Unlock()

	defer cancel()

	// get current position from DOM first
	currentPK, _ := b.getCurrentPK()

	// then lock and validate index, get target, and find current index
	b.reelsMu.RLock()

	if index < 1 || index > len(b.orderedReels) { // out of bounds
		b.reelsMu.RUnlock()
		return fmt.Errorf("index %d out of range", index)
	}
	targetPK := b.orderedReels[index-1].PK // we are already on this reel, no op
	if currentPK == targetPK {
		b.reelsMu.RUnlock()
		return nil
	}
	currentIndex := 0
	for idx, reel := range b.orderedReels { // we need to scroll, find current index
		if reel.PK == currentPK {
			currentIndex = idx + 1
			break
		}
	}

	b.reelsMu.RUnlock()

	// Scroll towards target
	for i := 0; i < MaxRetries; i++ {
		// check if we've been superseded by a new SyncTo
		select {
		case <-ctx.Done():
			return nil // exit
		default: // fall through
		}

		if currentIndex < index {
			if err := b.scrollDown(); err != nil {
				return err
			}
		} else if currentIndex > index {
			if err := b.scrollUp(); err != nil {
				return err
			}
		}

		time.Sleep(time.Duration(500+rand.Intn(300)) * time.Millisecond) // wait for react

		// Check if we landed on target
		pk, err := b.getCurrentPK()
		if err == nil && pk == targetPK {
			return nil
		}

		// Update current index estimate
		b.reelsMu.RLock()
		for i, r := range b.orderedReels {
			if r.PK == pk {
				currentIndex = i + 1
				break
			}
		}
		b.reelsMu.RUnlock()
	}

	return fmt.Errorf("failed to sync to index %d after %d scrolls", index, MaxRetries)
}

// scrollDown scrolls down to the next reel using CDP keyboard input
func (b *ChromeBackend) scrollDown() error {
	return chromedp.Run(b.ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			// Use CDP Input.dispatchKeyEvent directly - same as Selenium's ActionChains
			return input.DispatchKeyEvent(input.KeyDown).
				WithKey("ArrowDown").
				WithCode("ArrowDown").
				WithWindowsVirtualKeyCode(40).
				WithNativeVirtualKeyCode(40).
				Do(ctx)
		}),
	)
}

// scrollUp scrolls up to the previous reel using CDP keyboard input
func (b *ChromeBackend) scrollUp() error {
	return chromedp.Run(b.ctx,
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

// ToggleLike clicks the like button for the current reel
func (b *ChromeBackend) ToggleLike() (bool, error) {
	// Get the current reel's PK first
	pk, err := b.getCurrentPK()
	if err != nil {
		return false, err
	}

	// Clear any previous marker, then find the like button for the visible video
	js := `
		(() => {
			// Clear old markers first
			document.querySelectorAll('[data-reels-like-btn]').forEach(el => {
				el.removeAttribute('data-reels-like-btn');
			});

			const videos = document.querySelectorAll('video[playsinline]');
			for (const video of videos) {
				const rect = video.getBoundingClientRect();
				const viewportHeight = window.innerHeight;
				const videoCenter = rect.top + rect.height / 2;
				if (videoCenter > 0 && videoCenter < viewportHeight) {
					let parent = video.parentElement;
					for (let i = 0; i < 15; i++) {
						if (!parent) break;
						const svg = parent.querySelector('svg[aria-label="Like"], svg[aria-label="Unlike"]');
						if (svg) {
							const btn = svg.closest('[role="button"]');
							if (btn) {
								btn.setAttribute('data-reels-like-btn', 'true');
								return true;
							}
						}
						parent = parent.parentElement;
					}
				}
			}
			return false;
		})()
	`
	var found bool
	if err := chromedp.Run(b.ctx, chromedp.Evaluate(js, &found)); err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}

	// Now use chromedp's native click on the marked element
	if err := chromedp.Run(b.ctx,
		chromedp.Click(`[data-reels-like-btn="true"]`, chromedp.ByQuery),
	); err != nil {
		return false, err
	}

	// Toggle like in the stored reel
	b.reelsMu.Lock()
	for i := range b.orderedReels {
		if b.orderedReels[i].PK == pk {
			b.orderedReels[i].Liked = !b.orderedReels[i].Liked
			break
		}
	}
	b.reelsMu.Unlock()

	return true, nil
}
