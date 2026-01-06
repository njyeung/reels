package backend

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// pkRegex is compiled once for extracting ig_cache_key from URLs
var pkRegex = regexp.MustCompile(`ig_cache_key=([^&]+)`)

// ChromeBackend implements Backend using chromedp
type ChromeBackend struct {
	ctx         context.Context
	cancel      context.CancelFunc
	allocCancel context.CancelFunc

	mu           sync.RWMutex
	orderedReels []Reel
	seenPKs      map[string]bool

	events chan Event

	userDataDir string
	cacheDir    string
}

// NewChromeBackend creates a new Chrome-based backend
func NewChromeBackend(userDataDir, cacheDir string) *ChromeBackend {
	return &ChromeBackend{
		orderedReels: make([]Reel, 0),
		seenPKs:      make(map[string]bool),
		events:       make(chan Event, 100),
		userDataDir:  userDataDir,
		cacheDir:     cacheDir,
	}
}

// Start initializes Chrome and navigates to Instagram homepage
func (b *ChromeBackend) Start() error {

	// Create user data directory for persistent sessions
	err := os.MkdirAll(b.userDataDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create user data dir: %w", err)
	}

	// Chrome options
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserDataDir(b.userDataDir),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("headless", false),
	)

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

// Login attempts to log in with user credentials
func (b *ChromeBackend) Login(username, password string) error {

	// Currently instagram is A/B testing 2 login pages
	// If they decide to add more, this will likely break
	// We also assume that insta doesn't try to send an email for 2FA

	var isVariantA bool
	chromedp.Run(b.ctx,
		chromedp.Evaluate(`document.querySelector('input[name="email"]') !== null`,
			&isVariantA),
	)

	if isVariantA { // try variant A first: name='email' and name='pass'
		err := chromedp.Run(b.ctx,
			chromedp.Clear(`input[name="email"]`),
			chromedp.SendKeys(`input[name="email"]`, username),
			chromedp.Clear(`input[name="pass"]`),
			chromedp.SendKeys(`input[name="pass"]`, password),
			chromedp.Sleep(500*time.Millisecond),
			chromedp.Click(`//div[@role='button'][.//*/text()[contains(., 'Log in')]]`),
		)
		if err != nil {
			return fmt.Errorf("login variant A failed: %w", err)
		}
	} else { // try variant B: aria-labels
		err := chromedp.Run(b.ctx,
			chromedp.Clear(`input[aria-label="Phone number, username, or email"]`),
			chromedp.SendKeys(`input[aria-label="Phone number, username, or email"]`, username),
			chromedp.Clear(`input[aria-label="Password"]`),
			chromedp.SendKeys(`input[aria-label="Password"]`, password),
			chromedp.Sleep(500*time.Millisecond),
			chromedp.Click(`button[type="submit"]`),
		)
		if err != nil {
			return fmt.Errorf("login variant B failed: %w", err)
		}
	}

	time.Sleep(5 * time.Second) // Wait for login to complete

	// if we're logged in, there shouldn't be an input element in the html
	var isLoggedin bool
	chromedp.Run(b.ctx,
		chromedp.Evaluate(`document.querySelector('input') == null`,
			&isLoggedin),
	)
	if !isLoggedin {
		return fmt.Errorf("login failed: incorrect username or password")
	}

	return nil
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

	b.mu.RLock()
	defer b.mu.RUnlock()

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
	b.mu.RLock()
	defer b.mu.RUnlock()

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
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.orderedReels)
}

// SyncTo scrolls browser to match the given index
func (b *ChromeBackend) SyncTo(index int) error {
	// get current position from DOM first
	currentPK, _ := b.getCurrentPK()

	// then lock and validate index, get target, and find current index
	b.mu.RLock()

	if index < 1 || index > len(b.orderedReels) { // out of bounds
		b.mu.RUnlock()
		return fmt.Errorf("index %d out of range", index)
	}
	targetPK := b.orderedReels[index-1].PK // we are already on this reel, no op
	if currentPK == targetPK {
		b.mu.RUnlock()
		return nil
	}
	currentIndex := 0
	for idx, reel := range b.orderedReels { // we need to scroll, find current index
		if reel.PK == currentPK {
			currentIndex = idx + 1
			break
		}
	}

	b.mu.RUnlock()

	// Scroll towards target
	for i := 0; i < MaxRetries; i++ {
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
		b.mu.RLock()
		for i, r := range b.orderedReels {
			if r.PK == pk {
				currentIndex = i + 1
				break
			}
		}
		b.mu.RUnlock()
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

// ToggleLike simulates a double-tap to like/unlike
func (b *ChromeBackend) ToggleLike() (bool, error) {
	// Double click on the video
	js := `
		(() => {
			const videos = document.querySelectorAll('video[playsinline]');
			for (const video of videos) {
				const rect = video.getBoundingClientRect();
				const viewportHeight = window.innerHeight;
				const videoCenter = rect.top + rect.height / 2;
				if (videoCenter > 0 && videoCenter < viewportHeight) {
					const event = new MouseEvent('dblclick', {
						bubbles: true,
						cancelable: true,
						view: window
					});
					video.dispatchEvent(event);
					return true;
				}
			}
			return false;
		})()
	`
	var success bool
	if err := chromedp.Run(b.ctx, chromedp.Evaluate(js, &success)); err != nil {
		return false, err
	}
	return success, nil
}

// Download downloads a reel video to the cache directory
func (b *ChromeBackend) Download(index int) (string, error) {
	b.mu.RLock()
	if index < 1 || index > len(b.orderedReels) {
		b.mu.RUnlock()
		return "", fmt.Errorf("index out of range")
	}
	reel := b.orderedReels[index-1]
	b.mu.RUnlock()

	if reel.VideoURL == "" {
		return "", fmt.Errorf("no video URL")
	}

	if err := os.MkdirAll(b.cacheDir, 0755); err != nil {
		return "", err
	}

	filename := filepath.Join(b.cacheDir, fmt.Sprintf("%03d_%s.mp4", index, reel.Code))

	// Check if already downloaded
	if _, err := os.Stat(filename); err == nil {
		return filename, nil
	}

	// Download using chromedp fetch
	var data []byte
	err := chromedp.Run(b.ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			js := fmt.Sprintf(`
				fetch("%s")
					.then(r => r.arrayBuffer())
					.then(buf => Array.from(new Uint8Array(buf)))
			`, reel.VideoURL)
			var arr []int
			if err := chromedp.Evaluate(js, &arr).Do(ctx); err != nil {
				return err
			}
			data = make([]byte, len(arr))
			for i, v := range arr {
				data[i] = byte(v)
			}
			return nil
		}),
	)
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return "", err
	}

	return filename, nil
}
