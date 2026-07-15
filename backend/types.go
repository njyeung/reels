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

	// modeMu guards swaps of ctx and active when entering/exiting chat mode.
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
	// active is whichever cursor user-action methods route through: the feed
	// cursor, or a ChatCursor swapped in alongside ctx in chat mode.
	feed   *FeedCursor
	active Cursor

	// dmCtx is the secondary chromedp window used for chat-mode navigation
	// and DM-inbox collection. Created once by startDMSession after the feed
	// is up; lives until Stop. Nil if the session never started.
	dmCtx    context.Context
	dmCancel context.CancelFunc

	// dm owns the synchronized DM data: chats with their shared-reel
	// entries, plus the captured request template used for:
	// - reels prefetch
	// - reactions
	// See dmstate.go.
	dm *dmState

	// comments encapsulates all comment-related state
	comments *CommentsState

	// share modal state
	shareFriends []User

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
	GetShareFriends() []User

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

	// GetDMChats returns the chats with shared reels in DMs, grouped by
	// thread (1:1 or group). Populated by the background DM-inbox collection.
	GetDMChats() []DMChat

	// GetDMReelsCount returns the total number of unseen friend-shared reels
	// across all chats. Used by the startup HUD notification.
	GetDMReelsCount() int

	// EnterChatMode swaps the active cursor to a ChatCursor over the
	// chat's reel entries and routes user actions through the DM
	// window. The cursor is positioned on the first reel to show
	// so GetCurrent works immediately. Errors if the
	// chat isn't known.
	EnterChatMode(threadKey string) error

	// ExitChatMode restores the feed cursor and feed window. Idempotent
	// when not in chat mode. Emits EventChatModeExited on transition.
	ExitChatMode()

	// IsChatMode reports whether the active cursor is a ChatCursor.
	IsChatMode() bool

	// ChatSender returns the sender of the chat entry at 1-based index
	// (username + local pfp path). ok is false when not in chat mode or the
	// index is out of range.
	ChatSender(index int) (User, bool)

	// ChatReactions returns the reactions on the chat entry at 1-based index,
	// each a User carrying the reactor's name/pfp and their emoji (including
	// the viewer's own). ok is false when not in chat mode or out of range.
	ChatReactions(index int) ([]User, bool)

	// ReactToCurrent toggles emoji as the viewer's DM reel reaction: repeating
	// the current reaction removes it, any other emoji replaces it
	ReactToCurrent(emoji string) error
}

const (
	// MaxRetries is the maximum number of scroll/retry attempts for sync operations
	MaxRetries = 30

	// InstagramPKLength is the length of Instagram primary keys (19 digits)
	InstagramPKLength = 19

	// FIFO cache limits per asset type
	ReelCacheSize     = 50
	GifCacheSize      = 1000 // surely your screen isn't big enough to store 1000 gifs
	SharePfpCacheSize = 50
	DMPfpCacheSize    = 1000 // surely you don't have 1000 friends
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
	FloatingTypeSent     = "SENT_BY" // synthetic: not an IG floating_context_item_type
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

// User represents an Instagram user: a share-modal list entry, the sender of a
// DM reel share, or someone who reacted to one
type User struct {
	Name     string // display name (share modal) or username (DM sender/reactor)
	ImgSrc   string // profile pic URL
	ImgPath  string // local path to downloaded profile pic
	Reaction string // emoji when this User is a reaction; "" otherwise
}

// EventType represents different backend events
type EventType int

const (
	EventCommentsCaptured EventType = iota
	EventShareFriendsLoaded
	EventSyncComplete
	EventError
	EventDMReelsReady
	EventChatModeExited
)

// Event is sent from backend to frontend
type Event struct {
	Type  EventType
	Count int
}
