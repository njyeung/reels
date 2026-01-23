package backend

import (
	"context"
	"sync"
)

// ChromeBackend implements Backend using chromedp
type ChromeBackend struct {
	ctx         context.Context
	cancel      context.CancelFunc
	allocCancel context.CancelFunc

	reelsMu      sync.RWMutex
	orderedReels []Reel
	seenPKs      map[string]bool

	syncMu     sync.Mutex
	syncCancel context.CancelFunc

	events chan Event

	userDataDir string
	cacheDir    string
	configDir   string
}

// Backend defines the interface between frontend and backend
type Backend interface {

	// Start initializes the browser (does not navigate yet)
	// If headless is false, the browser opens visibly for manual login
	Start(headless bool) error

	// Stop closes the browser and cleans up
	Stop()

	// NeedsLogin checks if login is required
	NeedsLogin() (bool, error)

	// NavigateToReels goes to /reels and syncs to first captured reel
	NavigateToReels() error

	// GetCurrent returns info about the currently visible reel in browser
	GetCurrent() (*ReelInfo, error)

	// GetReel returns reel info by index (1-based) from cache, no browser interaction
	GetReel(index int) (*ReelInfo, error)

	// GetTotal returns total number of captured reels
	GetTotal() int

	// ToggleNavbar toggles navbar visibility and persists the state.
	// Returns true if navbar should be shown, false if hidden.
	ToggleNavbar() (bool, error)

	// SyncTo scrolls browser to match the given index
	// This is async-friendly - call it in background after optimistic UI update
	SyncTo(index int) error

	// ToggleLike likes/unlikes the current reel
	ToggleLike() (bool, error)

	// Download downloads a reel video and profile picture to the cache directory
	Download(index int) (videoPath string, pfpPath string, err error)

	// Events returns a channel for backend events (new reels captured, etc)
	Events() <-chan Event
}

const (
	// MaxRetries is the maximum number of scroll/retry attempts for sync operations
	MaxRetries = 30

	// InstagramPKLength is the length of Instagram primary keys (19 digits)
	InstagramPKLength = 19

	// Max num of reels that can be in the cache
	CacheSize = 10
)

// MusicInfo contains song metadata when a reel has music
type MusicInfo struct {
	Title      string
	Artist     string
	IsExplicit bool
}

// Reel represents a single Instagram reel with metadata
type Reel struct {
	PK            string
	Code          string
	VideoURL      string
	ProfilePicUrl string
	Username      string
	Caption       string
	Liked         bool
	LikeCount     int
	IsVerified    bool
	Music         *MusicInfo
}

// ReelInfo includes the reel data plus its position in the feed
type ReelInfo struct {
	Index int `json:"index"`
	Total int `json:"total"`
	Reel
}

// EventType represents different backend events
type EventType int

const (
	EventReelsCaptured EventType = iota
	EventSyncComplete
	EventError
)

// Event is sent from backend to frontend
type Event struct {
	Type    EventType
	Message string
	Count   int // for EventReelsCaptured
}
