package backend

import (
	"context"
	"fmt"
	"sync"

	"github.com/chromedp/chromedp"
)

// ChatCursor navigates a fixed list of DM-shared reels in the secondary
// window. Position is authoritative (we drove the
// navigation, so we know where we are).
type ChatCursor struct {
	ctx       context.Context
	threadKey string
	entries   []dmReelEntry

	mu     sync.RWMutex
	cursor int // 0-based index into entries

	syncMu     sync.Mutex
	syncCtx    context.Context
	syncCancel context.CancelFunc
}

// NewChatCursor binds the cursor to the DM window's chromedp context and a
// snapshot of the chat's entries. The entry list is treated as immutable.
// if it changes, build a new cursor. startIndex (1-based) positions the cursor
// up front — reels are prefetched, so Current() resolves before SyncTo's
// background navigation lands.
func NewChatCursor(ctx context.Context, threadKey string, entries []dmReelEntry, startIndex int) *ChatCursor {
	return &ChatCursor{ctx: ctx, threadKey: threadKey, entries: entries, cursor: startIndex - 1}
}

// ThreadKey returns the chat whose entries this cursor navigates.
func (cc *ChatCursor) ThreadKey() string {
	return cc.threadKey
}

// Total returns the number of entries this cursor can navigate.
func (cc *ChatCursor) Total() int {
	return len(cc.entries)
}

// PKAt returns the PK at 1-based index, or "" if out of range.
func (cc *ChatCursor) PKAt(index int) string {
	if index < 1 || index > len(cc.entries) {
		return ""
	}
	return cc.entries[index-1].PK
}

// Current returns the (1-based index, PK) of the entry we last navigated to.
// Errors if SyncTo hasn't been called yet.
func (cc *ChatCursor) Current() (int, string, error) {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	if cc.cursor < 0 || cc.cursor >= len(cc.entries) {
		return 0, "", fmt.Errorf("chat cursor not yet positioned")
	}
	return cc.cursor + 1, cc.entries[cc.cursor].PK, nil
}

// SyncTo navigates the DM window to entries[index-1].TargetURL.Returns once
// Navigate completes. The clip-response body arrives
// asynchronously through the listener and is not awaited here.
func (cc *ChatCursor) SyncTo(index int) error {
	if index < 1 || index > len(cc.entries) {
		return fmt.Errorf("index %d out of range", index)
	}

	cc.syncMu.Lock()
	if cc.syncCancel != nil {
		cc.syncCancel()
	}
	ctx, cancel := context.WithCancel(cc.ctx)
	cc.syncCtx = ctx
	cc.syncCancel = cancel
	cc.syncMu.Unlock()
	defer cancel()

	cc.mu.Lock()
	cc.cursor = index - 1
	target := cc.entries[index-1].TargetURL
	cc.mu.Unlock()

	return chromedp.Run(ctx, chromedp.Navigate(target))
}

// IsSyncing returns true if a SyncTo Navigate is in flight.
func (cc *ChatCursor) IsSyncing() bool {
	cc.syncMu.Lock()
	defer cc.syncMu.Unlock()
	return cc.syncCtx != nil && cc.syncCtx.Err() == nil
}
