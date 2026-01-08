package backend

// Backend defines the interface between frontend and backend
type Backend interface {
	// Start initializes the browser (does not navigate yet)
	Start() error

	// Stop closes the browser and cleans up
	Stop()

	// NeedsLogin checks if login is required
	NeedsLogin() (bool, error)

	// Login attempts to log in with credentials
	Login(username, password string) error

	// NavigateToReels goes to /reels and syncs to first captured reel
	NavigateToReels() error

	// GetCurrent returns info about the currently visible reel in browser
	GetCurrent() (*ReelInfo, error)

	// GetReel returns reel info by index (1-based) from cache, no browser interaction
	GetReel(index int) (*ReelInfo, error)

	// GetTotal returns total number of captured reels
	GetTotal() int

	// SyncTo scrolls browser to match the given index
	// This is async-friendly - call it in background after optimistic UI update
	SyncTo(index int) error

	// ToggleLike likes/unlikes the current reel
	ToggleLike() (bool, error)

	// Download downloads a reel to the cache directory and returns the file path
	Download(index int) (string, error)

	// Events returns a channel for backend events (new reels captured, etc)
	Events() <-chan Event
}

const (
	// MaxRetries is the maximum number of scroll/retry attempts for sync operations
	MaxRetries = 30

	// InstagramPKLength is the length of Instagram primary keys (19 digits)
	InstagramPKLength = 19
)

// Reel represents a single Instagram reel with metadata
type Reel struct {
	PK         string `json:"pk"`
	Code       string `json:"code"`
	VideoURL   string `json:"video_url"`
	Username   string `json:"username"`
	Caption    string `json:"caption"`
	Liked      bool   `json:"has_liked"`
	LikeCount  int    `json:"like_count"`
	IsVerified bool   `json:"is_verified"`
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
