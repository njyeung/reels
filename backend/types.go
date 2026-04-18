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

	// DM session — secondary browser window, lives for the whole backend session
	dmCtx        context.Context
	dmCancel     context.CancelFunc
	dmReelBodies chan string // clip responses captured in the DM window

	// Phase A results: DM inbox grouped by sender
	dmMu      sync.RWMutex
	dmFriends []DMFriend

	// Phase B: friend-mode state. modeMu also guards friendReels.
	modeMu       sync.RWMutex
	viewMode     ViewMode
	activeFriend string           // username; only set when viewMode == ViewModeFriend
	friendCursor int              // index into active friend's Entries
	friendReels  map[string]*Reel // PK -> captured Reel
	threadSink   func(string)     // only non-nil during DM inbox collection

	// comments encapsulates all comment-related state
	comments *CommentsState

	// share modal state
	shareFriends []Friend

	syncMu     sync.Mutex
	syncCtx    context.Context
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
	ToggleNavbar() bool

	// SetVolume updates volume and persists to disk
	SetVolume(vol float64) error

	// SetReelSize updates the reel bounding box dimensions and persists to disk.
	SetReelSize(width, height int) error

	// SyncTo scrolls browser to match the given index
	// This is async-friendly - call it in background after optimistic UI update
	SyncTo(index int) error

	// ToggleLike likes/unlikes the current reel
	ToggleLike() (bool, error)

	// ToggleSave bookmarks/unbookmarks the current reel
	ToggleSave() (bool, error)

	// IsSyncing returns true if the backend is still scrolling to a reel, false otherwise
	IsSyncing() bool

	// GetCommentsReelPK returns which reel we're fetching comments for
	GetCommentsReelPK() string

	// OpenSharePanel clicks the share button to open Instagram's share modal,
	// scrapes the friend list from the DOM, and emits EventShareFriendsLoaded.
	OpenSharePanel()

	// GetShareFriends returns the friend list scraped from the share modal
	GetShareFriends() []Friend

	// ToggleShareFriend clicks the friend at the given index in the share modal
	ToggleShareFriend(index int)

	// SendShare clicks the Send button in the share modal and closes it
	SendShare()

	// OpenComments opens the current reel's comment section
	OpenComments()

	// CloseComments closes the comments panel UI (preserves cache)
	CloseComments()

	// ClearComments closes the comments panel and clears the cache
	ClearComments()

	// FetchMoreComments fetches the next page of comments using stored pagination state
	FetchMoreComments()

	// GetDMFriends returns the list of friends who sent reels via DMs, each
	// with their unseen reel entries. Populated after EventDMReelsReady.
	GetDMFriends() []DMFriend

	// GetDMReelsCount returns the total number of DM reel entries across all friends.
	GetDMReelsCount() int

	// EnterFriendMode switches the active browser to the secondary DM window,
	// navigates it to the first reel from `username`, and routes all subsequent
	// user actions (ToggleLike, OpenComments, etc.) to that window.
	EnterFriendMode(username string) error

	// ExitFriendMode flips back to the main feed window and parks the DM window on about:blank.
	ExitFriendMode()

	// NextFriendReel advances to the next reel from the active friend.
	// If there is no next reel, it auto-exits friend mode.
	NextFriendReel() error

	// PrevFriendReel moves to the previous reel from the active friend.
	// Clamped at the first reel.
	PrevFriendReel() error

	// IsFriendMode reports whether the backend is currently in friend-mode
	// (secondary DM window). Frontend uses this to branch UI behavior.
	IsFriendMode() bool

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

	// FIFO cache limits per asset type
	ReelCacheSize     = 50
	GifCacheSize      = 1000
	SharePfpCacheSize = 50
)

// MusicInfo contains song metadata when a reel has music
type MusicInfo struct {
	Title      string
	Artist     string
	IsExplicit bool
}

// Reel represents a single Instagram reel with metadata
type Reel struct {
	PK                 string
	Code               string
	VideoURL           string
	ProfilePicUrl      string
	Username           string
	Caption            string
	Liked              bool
	Saved              bool
	LikeCount          int
	IsVerified         bool
	CommentCount       int
	CommentsDisabled   bool
	Music              *MusicInfo
	CanViewerReshare   bool
	Comments           []Comment           // cached comments (nil = not fetched yet)
	CommentsPagination *CommentsPagination // cached pagination state for resuming
}

// ReelInfo includes the reel data plus its position in the feed
type ReelInfo struct {
	Index int `json:"index"`
	Total int `json:"total"`
	Reel
}

// CommentsPagination holds the resumable pagination state for a reel's comments.
// Stored on the Reel struct so pagination can be restored after navigating away and back.
type CommentsPagination struct {
	Cursor            string
	HasNextPage       bool
	RequestTemplate   string
	PaginationEnabled bool
}

type Comment struct {
	PK                string // this is a pointer to the reel PK
	CreatedAt         int64
	ChildCommentCount int
	ProfilePicUrl     string
	Username          string
	IsVerified        bool
	HasLikedComment   bool
	Text              string
	CommentLikeCount  int
	GifUrl            string
	GifPath           string // local path to downloaded GIF file
}

// DMReelEntry is a pointer to a reel shared in a DM thread — the TargetURL is
// navigated in the secondary window to resolve the full Reel on demand.
type DMReelEntry struct {
	TargetID   string // reel PK
	TargetURL  string // navigate here to fetch the Reel
	ReelAuthor string // xmaHeaderTitle, i.e. the reel's original poster
}

// DMFriend groups all reel entries shared by a single friend via DMs.
type DMFriend struct {
	Username string
	Entries  []DMReelEntry
}

// ViewMode switches which browser window user actions operate on.
type ViewMode int

const (
	ViewModeFeed   ViewMode = iota // main reels feed window
	ViewModeFriend                 // secondary DM window, browsing one friend's reels
)

// Friend represents a user shown in the share modal's friend list
type Friend struct {
	Name    string // display name from the DOM
	ImgSrc  string // profile pic URL from the DOM
	ImgPath string // local path to downloaded profile pic
}

// EventType represents different backend events
type EventType int

const (
	EventCommentsCaptured EventType = iota
	EventShareFriendsLoaded
	EventSyncComplete
	// EventDMReelsReady fires after the DM inbox has been scraped and grouped.
	// Count holds the number of friends who shared reels.
	EventDMReelsReady
	// EventFriendReelLoaded fires after a friend-mode navigation resolves a reel.
	EventFriendReelLoaded
	// EventFriendModeExited fires when the backend leaves friend mode (either
	// auto-exit past the last reel or an explicit ExitFriendMode call).
	EventFriendModeExited
	EventError
)

// Event is sent from backend to frontend
type Event struct {
	Type  EventType
	Count int // for EventReelsCaptured
}
