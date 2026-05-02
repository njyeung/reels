package backend

import (
	"context"
	"sync"
)

// ChromeBackend implements Backend using chromedp
type ChromeBackend struct {
	// feedCtx is stable for the lifetime of the backend and tied to the main
	// feed browser window. Used by the fetch listener and FeedCursor for
	// scrolling and DOM probes.
	feedCtx     context.Context
	feedCancel  context.CancelFunc
	allocCancel context.CancelFunc

	// modeMu guards swaps of ctx and active when entering/exiting friend mode.
	// Read paths in user-action methods access ctx directly; modeMu only
	// matters at swap boundaries.
	modeMu sync.RWMutex
	// ctx is the swappable handle that user-action methods (ToggleLike,
	// OpenSharePanel, FetchMoreComments JS fetch, fetchURLs, etc.) read.
	ctx context.Context

	// reels is the single source of truth for reel data, keyed by PK.
	// Membership in this map is the dedup signal — no separate seen set.
	reelsMu sync.RWMutex
	reels   map[string]*Reel

	// feed is the always-present cursor for the main reels page.
	// active is whichever cursor user-action methods route through; today
	// always == feed. A future FriendCursor will be swapped in alongside ctx.
	feed   *FeedCursor
	active Cursor

	// dmCtx is the secondary chromedp window used for friend-mode navigation
	// and DM-inbox collection. Created once by startDMSession after the feed
	// is up; lives until Stop. Nil if the session never started.
	dmCtx    context.Context
	dmCancel context.CancelFunc

	// dmMu guards dmFriends. Merged into by processThreadResponse on every DM
	// thread body; read by GetDMFriends / GetDMReelsCount / EnterFriendMode.
	dmMu      sync.RWMutex
	dmFriends []DMFriend

	// comments encapsulates all comment-related state
	comments *CommentsState

	// share modal state
	shareFriends []Friend

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

	// ToggleRepost reposts/unreposts the current reel
	ToggleRepost() (bool, error)

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

	// SendShare clicks the Send button in the share modal and closes it.
	// Returns (sent, err): sent=true if a share was sent, sent=false with
	// err=nil when nothing was selected (modal closed without sending), and
	// a non-nil err if the button is disabled or a runtime error occurred.
	SendShare() (bool, error)

	// OpenComments opens the current reel's comment section
	OpenComments()

	// CloseComments closes the comments panel UI (preserves cache)
	CloseComments()

	// ClearComments closes the comments panel and clears the cache
	ClearComments()

	// FetchMoreComments fetches the next page of comments using stored pagination state
	FetchMoreComments()

	// Download downloads a reel video, creator profile pic, and any floating-
	// context item pfps (reposts/likes from friends) to the cache directory.
	Download(index int) (videoPath string, pfpPath string, floatingPfps []FloatingPfpFile, err error)

	// Events returns a channel for backend events (new reels captured, etc)
	Events() <-chan Event

	// GetDMFriends returns the friends who have shared reels in DMs, grouped
	// by sender. Populated by the background DM-inbox collection.
	GetDMFriends() []DMFriend

	// GetDMReelsCount returns the total number of unseen friend-shared reels
	// across all friends. Used by the startup HUD notification.
	GetDMReelsCount() int

	// EnterFriendMode swaps the active cursor to a FriendCursor over the
	// named friend's reel entries and routes user actions through the DM
	// window. Errors if the friend isn't known.
	EnterFriendMode(username string) error

	// ExitFriendMode restores the feed cursor and feed window. Idempotent
	// when not in friend mode. Emits EventFriendModeExited on transition.
	ExitFriendMode()

	// IsFriendMode reports whether the active cursor is a FriendCursor.
	IsFriendMode() bool
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

// FloatingContextItem represents a friend-activity badge on a reel,
type FloatingContextItem struct {
	Type          string // REPOSTED_BY, LIKED_BY, etc.
	Username      string
	ProfilePicUrl string
	Text          string // media_note.text or comment.text, empty when absent
}

// Floating-context item types
const (
	FloatingTypeReposted = "REPOSTED_BY"
	FloatingTypeLiked    = "LIKED_BY"
)

// FloatingPfpFile is a downloaded floating-context pfp paired with its type
type FloatingPfpFile struct {
	Path string
	Type string
}

// Reel represents a single Instagram reel with metadata
type Reel struct {
	PK                   string
	Code                 string
	VideoURL             string
	ProfilePicUrl        string
	Username             string
	Caption              string
	Liked                bool
	Saved                bool
	Reposted             bool
	LikeCount            int
	RepostCount          int
	IsVerified           bool
	CommentCount         int
	CommentsDisabled     bool
	Music                *MusicInfo
	CanViewerReshare     bool
	FloatingContextItems []FloatingContextItem
	Comments             []Comment           // cached comments (nil = not fetched yet)
	CommentsPagination   *CommentsPagination // cached pagination state for resuming
}

// ReelInfo includes the reel data plus its position in the feed
type ReelInfo struct {
	Index int `json:"index"`
	Total int `json:"total"`
	Reel
}

// DMReelEntry is a pointer to a reel shared in a DM thread. The DM window
// navigates to TargetURL to materialize the full Reel
type DMReelEntry struct {
	TargetPK       string // reel PK
	TargetURL      string // navigate here to fetch the Reel
	ReelAuthor     string // xmaHeaderTitle, i.e. the reel's original poster
	SenderUsername string // who shared the reel
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
	EventError
	EventDMReelsReady
	EventFriendReelLoaded
	EventFriendModeExited
)

// Event is sent from backend to frontend
type Event struct {
	Type  EventType
	Count int
}
