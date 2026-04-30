package backend

import (
	"context"
	"fmt"
	"sync"

	"github.com/chromedp/chromedp"
)

// FriendCursor navigates a fixed list of DM-shared reels in the secondary
// window. Unlike FeedCursor, position is authoritative (we drove the
// navigation, so we know where we are).
type FriendCursor struct {
	ctx     context.Context
	entries []DMReelEntry

	mu     sync.RWMutex
	cursor int // -1 before first SyncTo; else 0-based index into entries

	syncMu     sync.Mutex
	syncCtx    context.Context
	syncCancel context.CancelFunc
}

// NewFriendCursor binds the cursor to the DM window's chromedp context and a
// snapshot of the friend's entries. The entry list is treated as immutable —
// if it changes, build a new cursor.
func NewFriendCursor(ctx context.Context, entries []DMReelEntry) *FriendCursor {
	return &FriendCursor{ctx: ctx, entries: entries, cursor: -1}
}

// Total returns the number of entries this cursor can navigate.
func (fc *FriendCursor) Total() int {
	return len(fc.entries)
}

// PKAt returns the PK at 1-based index, or "" if out of range.
func (fc *FriendCursor) PKAt(index int) string {
	if index < 1 || index > len(fc.entries) {
		return ""
	}
	return fc.entries[index-1].TargetPK
}

// Current returns the (1-based index, PK) of the entry we last navigated to.
// Errors if SyncTo hasn't been called yet.
func (fc *FriendCursor) Current() (int, string, error) {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	if fc.cursor < 0 || fc.cursor >= len(fc.entries) {
		return 0, "", fmt.Errorf("friend cursor not yet positioned")
	}
	return fc.cursor + 1, fc.entries[fc.cursor].TargetPK, nil
}

// SyncTo navigates the DM window to entries[index-1].TargetURL.Returns once
// Navigate completes. The clip-response body arrives
// asynchronously through the listener and is not awaited here.
func (fc *FriendCursor) SyncTo(index int) error {
	if index < 1 || index > len(fc.entries) {
		return fmt.Errorf("index %d out of range", index)
	}

	fc.syncMu.Lock()
	if fc.syncCancel != nil {
		fc.syncCancel()
	}
	ctx, cancel := context.WithCancel(fc.ctx)
	fc.syncCtx = ctx
	fc.syncCancel = cancel
	fc.syncMu.Unlock()
	defer cancel()

	fc.mu.Lock()
	fc.cursor = index - 1
	target := fc.entries[index-1].TargetURL
	fc.mu.Unlock()

	return chromedp.Run(ctx, chromedp.Navigate(target))
}

// IsSyncing returns true if a SyncTo Navigate is in flight.
func (fc *FriendCursor) IsSyncing() bool {
	fc.syncMu.Lock()
	defer fc.syncMu.Unlock()
	return fc.syncCtx != nil && fc.syncCtx.Err() == nil
}
