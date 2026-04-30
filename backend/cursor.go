package backend

import (
	"regexp"
)

// pkRegex extracts ig_cache_key from a URL — used to recover the visible
// reel's PK from its cached preview image src.
var pkRegex = regexp.MustCompile(`ig_cache_key=([^&]+)`)

// Cursor abstracts how the user navigates a list of reels in the browser.
// FeedCursor scrolls the main reels page; FriendCursor navigates to specific
// reel URLs in a secondary DM window.
type Cursor interface {
	// Current returns the (1-based index, PK) of the reel the user is looking
	// at. FeedCursor probes the DOM; FriendCursor reads its internal cursor.
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
